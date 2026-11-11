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
