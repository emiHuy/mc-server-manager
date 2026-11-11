package minecraft

import (
	"slices"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
)

const testBackoff = 20 * time.Millisecond

func TestBackoff(t *testing.T) {
	initial := 5 * time.Second
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 5 * time.Second},
		{2, 10 * time.Second},
		{3, 20 * time.Second},
		{4, 40 * time.Second},
		{5, 80 * time.Second},
		{6, 160 * time.Second},
		{7, maxBackoff},
		{8, maxBackoff},
		{1000, maxBackoff}, // must not overflow
	}
	for _, tt := range tests {
		if got := backoff(initial, tt.attempt); got != tt.want {
			t.Errorf("backoff(%v, %d) = %v, want %v", initial, tt.attempt, got, tt.want)
		}
	}
}

// helpers

func (r *Restarter) snapshot() (crashes int, gaveUp bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.crashes, r.gaveUp
}

func collectStates(t *testing.T, c <-chan Event, n int) []State {
	t.Helper()
	var got []State
	timeout := time.After(lifecycleTimeout)
	for len(got) < n {
		select {
		case e := <-c:
			if e.Type == EventStateChanged {
				got = append(got, e.State)
			}
		case <-timeout:
			t.Fatalf("timed out with %d of %d states: %v", len(got), n, got)
		}
	}
	return got
}

func expectNoStates(t *testing.T, c <-chan Event, d time.Duration) {
	t.Helper()
	timeout := time.After(d)
	for {
		select {
		case e := <-c:
			if e.Type == EventStateChanged {
				t.Fatalf("unexpected state change %v", e.State)
			}
		case <-timeout:
			return
		}
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(lifecycleTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// setup subscribes first so no events are missed, then wires the restarter.
func setupRestarter(t *testing.T, mode string, max int, initial time.Duration) (*Instance, *Restarter, <-chan Event) {
	t.Helper()
	inst := newFakeInstance(t, mode, 5*time.Second)
	sub := inst.SubscribeEvents()
	t.Cleanup(sub.Cancel)
	r := NewRestarter(inst, true, max, initial)
	t.Cleanup(r.Disable) // runs before newFakeInstance's process cleanup
	return inst, r, sub.C
}

// tests

func TestRestarterCrashThenRestart(t *testing.T) {
	inst, _, c := setupRestarter(t, modeCrashOnce, 3, testBackoff)

	if err := inst.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	want := []State{StateStarting, StateRunning, StateCrashed, StateStarting, StateRunning}
	if got := collectStates(t, c, len(want)); !slices.Equal(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
}

func TestRestarterLimitReached(t *testing.T) {
	const max = 2
	inst, r, c := setupRestarter(t, modeCrash, max, testBackoff)

	if err := inst.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// the first launch plus max restarts, each Starting, Running, Crashed
	var want []State
	for i := 0; i < max+1; i++ {
		want = append(want, StateStarting, StateRunning, StateCrashed)
	}
	if got := collectStates(t, c, len(want)); !slices.Equal(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}

	waitUntil(t, "gaveUp", func() bool { _, g := r.snapshot(); return g })
	expectNoStates(t, c, 10*testBackoff)

	if got := inst.State(); got != StateCrashed {
		t.Errorf("final state = %v, want %v", got, StateCrashed)
	}
}

func TestRestarterCrashBeforeReadyCounts(t *testing.T) {
	inst, r, c := setupRestarter(t, modeExitBeforeReady, 1, testBackoff)

	if err := inst.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	want := []State{StateStarting, StateCrashed, StateStarting, StateCrashed}
	if got := collectStates(t, c, len(want)); !slices.Equal(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}

	waitUntil(t, "gaveUp", func() bool { _, g := r.snapshot(); return g })
	expectNoStates(t, c, 10*testBackoff)
}

func TestRestarterDisableCancelsPendingRetry(t *testing.T) {
	const delay = 200 * time.Millisecond
	inst, r, c := setupRestarter(t, modeCrash, 5, delay)

	if err := inst.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	collectStates(t, c, 3) // Starting, Running, Crashed

	// wait until the retry is actually scheduled, so Disable has something to cancel
	waitUntil(t, "retry scheduled", func() bool { n, _ := r.snapshot(); return n == 1 })
	r.Disable()

	expectNoStates(t, c, 2*delay)
	if got := inst.State(); got != StateCrashed {
		t.Errorf("state = %v, want %v", got, StateCrashed)
	}
}

func TestRestarterManualStartCancelsRetry(t *testing.T) {
	const delay = 200 * time.Millisecond
	inst, r, c := setupRestarter(t, modeCrashOnce, 5, delay)

	if err := inst.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	collectStates(t, c, 3) // Starting, Running, Crashed
	waitUntil(t, "retry scheduled", func() bool { n, _ := r.snapshot(); return n == 1 })

	if err := inst.Start(); err != nil { // manual start, before the timer fires
		t.Fatalf("manual Start() error: %v", err)
	}
	want := []State{StateStarting, StateRunning}
	if got := collectStates(t, c, len(want)); !slices.Equal(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}

	// the timer fires into a Running server and must do nothing
	expectNoStates(t, c, 2*delay)
}

func TestRestarterFailedStartCountsAndGivesUp(t *testing.T) {
	const max = 2
	inst := New(config.MinecraftConfig{Command: "definitely-not-a-real-binary"})
	r := NewRestarter(inst, true, max, testBackoff)
	t.Cleanup(r.Disable)

	// simulate a crash without launching anything
	inst.mu.Lock()
	inst.state = StateCrashed
	inst.mu.Unlock()
	r.onCrash(0)

	waitUntil(t, "gaveUp", func() bool { _, g := r.snapshot(); return g })

	if n, _ := r.snapshot(); n != max {
		t.Errorf("attempts = %d, want %d", n, max)
	}
	// a failed Start leaves the instance Stopped, not Crashed
	if got := inst.State(); got != StateStopped {
		t.Errorf("final state = %v, want %v", got, StateStopped)
	}
}

func TestRestarterStableUptimeResets(t *testing.T) {
	tests := []struct {
		name         string
		ranFor       time.Duration
		wantGaveUp   bool
		wantAttempts int
	}{
		{"short run stays given up", time.Second, true, 2},
		{"stable run resets the counter", stableUptime, false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := New(config.MinecraftConfig{})
			r := NewRestarter(inst, true, 2, time.Hour) // timer never fires in the test
			t.Cleanup(r.Disable)

			r.mu.Lock()
			r.crashes, r.gaveUp = 2, true
			r.mu.Unlock()

			r.onCrash(tt.ranFor)

			crashes, gaveUp := r.snapshot()
			if gaveUp != tt.wantGaveUp || crashes != tt.wantAttempts {
				t.Errorf("crashes, gaveUp = %d, %v, want %d, %v", crashes, gaveUp, tt.wantAttempts, tt.wantGaveUp)
			}
		})
	}
}

// helpers

// newCrashedInstance returns an instance forced into Crashed whose Start()
// fails, so a callback that wrongly proceeds shows up as a state change.
func newCrashedInstance(t *testing.T) *Instance {
	t.Helper()
	inst := New(config.MinecraftConfig{Command: "definitely-not-a-real-binary"})
	inst.mu.Lock()
	inst.state = StateCrashed
	inst.mu.Unlock()
	return inst
}

// Status and nextRetryAt

func TestRestarterStatusIdle(t *testing.T) {
	inst := New(config.MinecraftConfig{})
	r := NewRestarter(inst, true, 5, time.Second)
	t.Cleanup(r.Disable)

	got := r.Status()
	if !got.Enabled || got.MaxRestarts != 5 || got.Restarts != 0 || got.GaveUp {
		t.Errorf("Status() = %+v, want enabled, max 5, no restarts, not gave up", got)
	}
	if !got.NextRetryAt.IsZero() {
		t.Errorf("NextRetryAt = %v, want zero", got.NextRetryAt)
	}
}

func TestRestarterStatusAfterCrash(t *testing.T) {
	inst := New(config.MinecraftConfig{})
	r := NewRestarter(inst, true, 3, time.Hour) // capped to maxBackoff, so the timer stays pending
	t.Cleanup(r.Disable)

	before := time.Now()
	r.onCrash(0)
	after := time.Now()

	got := r.Status()
	if got.Restarts != 1 {
		t.Errorf("Restarts = %d, want 1", got.Restarts)
	}
	want := backoff(time.Hour, 1)
	if got.NextRetryAt.Before(before.Add(want)) || got.NextRetryAt.After(after.Add(want)) {
		t.Errorf("NextRetryAt = %v, want about %v from now", got.NextRetryAt, want)
	}
}

func TestRestarterNextRetryAtClearsWhenRetryFires(t *testing.T) {
	inst, r, c := setupRestarter(t, modeCrashOnce, 3, testBackoff)

	if err := inst.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	collectStates(t, c, 5) // Starting, Running, Crashed, Starting, Running

	if got := r.Status().NextRetryAt; !got.IsZero() {
		t.Errorf("NextRetryAt = %v after the retry fired, want zero", got)
	}
}

func TestRestarterNextRetryAtClearsOnGiveUp(t *testing.T) {
	inst, r, c := setupRestarter(t, modeCrash, 1, testBackoff)

	if err := inst.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	collectStates(t, c, 6) // two launches, each Starting, Running, Crashed
	waitUntil(t, "gaveUp", func() bool { return r.Status().GaveUp })

	if got := r.Status().NextRetryAt; !got.IsZero() {
		t.Errorf("NextRetryAt = %v after giving up, want zero", got)
	}
}

func TestRestarterStatusStableRun(t *testing.T) {
	tests := []struct {
		name         string
		stable       time.Duration
		wantRestarts int
		wantGaveUp   bool
	}{
		{"running past the stable period reports clean", 50 * time.Millisecond, 0, false},
		{"running short of the stable period keeps the streak", time.Hour, 2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst, r, c := setupRestarter(t, modeNormal, 2, time.Hour)

			r.mu.Lock()
			r.stable = tt.stable
			r.crashes, r.gaveUp = 2, true
			r.mu.Unlock()

			if err := inst.Start(); err != nil {
				t.Fatalf("Start() error: %v", err)
			}
			collectStates(t, c, 2) // Starting, Running
			time.Sleep(100 * time.Millisecond)

			got := r.Status()
			if got.Restarts != tt.wantRestarts || got.GaveUp != tt.wantGaveUp {
				t.Errorf("Restarts, GaveUp = %d, %v, want %d, %v", got.Restarts, got.GaveUp, tt.wantRestarts, tt.wantGaveUp)
			}
		})
	}
}

// Runtime toggle

func TestRestarterSetEnabledFalseCancelsPendingRetry(t *testing.T) {
	const delay = 200 * time.Millisecond
	inst, r, c := setupRestarter(t, modeCrash, 5, delay)

	if err := inst.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	collectStates(t, c, 3) // Starting, Running, Crashed
	waitUntil(t, "retry scheduled", func() bool { return r.Status().Restarts == 1 })

	r.SetEnabled(false)

	if got := r.Status().NextRetryAt; !got.IsZero() {
		t.Errorf("NextRetryAt = %v after disabling, want zero", got)
	}
	expectNoStates(t, c, 2*delay)
	if got := inst.State(); got != StateCrashed {
		t.Errorf("state = %v, want %v", got, StateCrashed)
	}
}

func TestRestarterSetEnabledTrueResetsStreak(t *testing.T) {
	inst := New(config.MinecraftConfig{})
	r := NewRestarter(inst, true, 3, time.Hour)
	t.Cleanup(r.Disable)

	r.mu.Lock()
	r.crashes, r.gaveUp, r.failedStart = 2, true, true
	r.mu.Unlock()

	r.SetEnabled(false)
	r.SetEnabled(true)

	got := r.Status()
	if !got.Enabled || got.Restarts != 0 || got.GaveUp {
		t.Errorf("Status() = %+v, want enabled with a clean streak", got)
	}
	r.mu.Lock()
	failedStart := r.failedStart
	r.mu.Unlock()
	if failedStart {
		t.Error("failedStart was not cleared")
	}
}

func TestRestarterSetEnabledWhenAlreadyEnabledKeepsStreak(t *testing.T) {
	inst := New(config.MinecraftConfig{})
	r := NewRestarter(inst, true, 3, time.Hour)
	t.Cleanup(r.Disable)

	r.mu.Lock()
	r.crashes, r.gaveUp = 2, true
	r.mu.Unlock()

	r.SetEnabled(true)

	got := r.Status()
	if got.Restarts != 2 || !got.GaveUp {
		t.Errorf("Restarts, GaveUp = %d, %v, want 2, true", got.Restarts, got.GaveUp)
	}
}

func TestRestarterDisableIsTerminal(t *testing.T) {
	inst, r, c := setupRestarter(t, modeCrash, 3, testBackoff)

	r.Disable()
	r.SetEnabled(true) // must be refused

	if r.Status().Enabled {
		t.Error("Enabled = true after Disable(), want false")
	}

	if err := inst.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	collectStates(t, c, 3) // Starting, Running, Crashed
	expectNoStates(t, c, 10*testBackoff)

	if got := r.Status().Restarts; got != 0 {
		t.Errorf("Restarts = %d, want 0", got)
	}
}

func TestRestarterEnableAtRuntime(t *testing.T) {
	inst := newFakeInstance(t, modeCrash, 5*time.Second)
	sub := inst.SubscribeEvents()
	t.Cleanup(sub.Cancel)
	r := NewRestarter(inst, false, 1, testBackoff) // starts disabled
	t.Cleanup(r.Disable)

	if err := inst.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	collectStates(t, sub.C, 3)               // Starting, Running, Crashed
	expectNoStates(t, sub.C, 10*testBackoff) // disabled, so no retry

	r.SetEnabled(true)
	if err := inst.Start(); err != nil { // a manual start, then a crash
		t.Fatalf("manual Start() error: %v", err)
	}

	// the crash is now retried once, then the limit is hit
	want := []State{StateStarting, StateRunning, StateCrashed, StateStarting, StateRunning, StateCrashed}
	if got := collectStates(t, sub.C, len(want)); !slices.Equal(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
	waitUntil(t, "gaveUp", func() bool { return r.Status().GaveUp })
}

// The gen check

func TestRestarterStaleCallbackDoesNothing(t *testing.T) {
	inst := newCrashedInstance(t)
	r := NewRestarter(inst, true, 3, time.Hour)
	t.Cleanup(r.Disable)

	r.onCrash(0)
	r.mu.Lock()
	oldGen := r.gen
	r.mu.Unlock()

	// cancel, then re-enable, so only the gen check can reject the old callback
	r.SetEnabled(false)
	r.SetEnabled(true)

	r.fire(oldGen)

	if got := inst.State(); got != StateCrashed {
		t.Errorf("state = %v, want %v: a stale callback ran Start()", got, StateCrashed)
	}
}

func TestRestarterStaleCallbackLeavesNextRetryAtAlone(t *testing.T) {
	inst := newCrashedInstance(t)
	r := NewRestarter(inst, true, 3, time.Hour)
	t.Cleanup(r.Disable)

	r.onCrash(0)
	r.mu.Lock()
	oldGen := r.gen
	r.mu.Unlock()

	r.onCrash(0) // a newer timer takes over
	want := r.Status().NextRetryAt

	r.fire(oldGen)

	got := r.Status().NextRetryAt
	if got.IsZero() || !got.Equal(want) {
		t.Errorf("NextRetryAt = %v, want %v: a stale callback touched it", got, want)
	}
}
