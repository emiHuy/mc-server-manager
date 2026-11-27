package minecraft

import (
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/stretchr/testify/require"
)

func newRestarter(t *testing.T, state State, enabled bool, max int, initial time.Duration) (*Restarter, *Instance) {
	t.Helper()
	inst := New(config.MinecraftConfig{Command: "no-such-binary-xyz", Directory: t.TempDir()})
	inst.mu.Lock()
	inst.state = state
	inst.mu.Unlock()
	r := NewRestarter(inst, enabled, max, initial)
	t.Cleanup(r.Disable)
	return r, inst
}

type snapshot struct {
	crashes                              int
	gaveUp, failedStart, enabled, closed bool
	gen                                  uint64
	retry                                time.Time
}

func snap(r *Restarter) snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return snapshot{r.crashes, r.gaveUp, r.failedStart, r.enabled, r.closed, r.gen, r.nextRetryAt}
}

func TestBackoff(t *testing.T) {
	tests := []struct {
		initial time.Duration
		attempt int
		want    time.Duration
	}{
		{time.Second, 0, time.Second},
		{time.Second, 1, time.Second},
		{time.Second, 2, 2 * time.Second},
		{time.Second, 5, 16 * time.Second},
		{time.Second, 20, maxBackoff},
		{10 * time.Minute, 1, maxBackoff},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, backoff(tt.initial, tt.attempt))
	}
}

func TestNewRestarter(t *testing.T) {
	r, inst := newRestarter(t, StateStopped, true, 3, time.Hour)

	s := r.Status()
	require.True(t, s.Enabled)
	require.Equal(t, 3, s.MaxRestarts)
	require.Equal(t, 0, s.Restarts)
	require.False(t, s.GaveUp)

	inst.mu.Lock()
	hook := inst.onCrash
	inst.mu.Unlock()

	require.NotNil(t, hook)
	hook(0)
	require.Equal(t, 1, snap(r).crashes)
}

func TestOnCrash(t *testing.T) {
	t.Run("disabled does nothing", func(t *testing.T) {
		r, _ := newRestarter(t, StateCrashed, false, 3, time.Hour)
		r.onCrash(0)

		s := snap(r)
		require.Equal(t, 0, s.crashes)
		require.Zero(t, s.retry)
	})

	t.Run("schedules with growing backoff", func(t *testing.T) {
		r, _ := newRestarter(t, StateCrashed, true, 5, 30*time.Second)
		r.onCrash(0)
		first := time.Until(snap(r).retry)
		r.onCrash(0)
		second := time.Until(snap(r).retry)

		require.Equal(t, 2, snap(r).crashes)

		require.Greater(t, first, 25*time.Second)
		require.LessOrEqual(t, first, 30*time.Second)

		require.Greater(t, second, 55*time.Second)
		require.LessOrEqual(t, second, 60*time.Second)

		st := r.Status()
		require.Equal(t, 2, st.Restarts)
		require.NotZero(t, st.NextRetryAt)
	})

	t.Run("gives up at max", func(t *testing.T) {
		r, _ := newRestarter(t, StateCrashed, true, 2, time.Hour)
		r.onCrash(0)
		r.onCrash(0)
		gen := snap(r).gen
		r.onCrash(0)

		s := snap(r)
		require.True(t, s.gaveUp)
		require.Equal(t, 2, s.crashes)
		require.Equal(t, gen, s.gen)

		require.True(t, r.Status().GaveUp)
	})

	t.Run("zero max gives up immediately", func(t *testing.T) {
		r, _ := newRestarter(t, StateCrashed, true, 0, time.Hour)
		r.onCrash(0)

		s := snap(r)
		require.True(t, s.gaveUp)
		require.Equal(t, 0, s.crashes)
	})

	t.Run("stable run resets counters", func(t *testing.T) {
		r, _ := newRestarter(t, StateCrashed, true, 2, time.Hour)
		for range 3 {
			r.onCrash(0)
		}
		r.onCrash(r.stable)
		s := snap(r)
		require.False(t, s.gaveUp)
		require.Equal(t, 1, s.crashes)
	})

	t.Run("short run does not reset", func(t *testing.T) {
		r, _ := newRestarter(t, StateCrashed, true, 5, time.Hour)
		r.onCrash(0)
		r.onCrash(r.stable - time.Nanosecond)
		require.Equal(t, 2, snap(r).crashes)
	})

	t.Run("clears failedStart", func(t *testing.T) {
		r, _ := newRestarter(t, StateCrashed, true, 5, time.Hour)
		r.mu.Lock()
		r.failedStart = true
		r.mu.Unlock()
		r.onCrash(0)
		require.False(t, snap(r).failedStart)
	})
}

func TestDisableAndSetEnabled(t *testing.T) {
	t.Run("Disable is terminal", func(t *testing.T) {
		r, _ := newRestarter(t, StateCrashed, true, 3, time.Hour)
		r.onCrash(0)
		gen := snap(r).gen

		r.Disable()
		r.Disable() // idempotent
		r.SetEnabled(true)

		s := snap(r)
		require.True(t, s.closed)
		require.False(t, s.enabled)
		require.NotEqual(t, gen, s.gen)
		require.Zero(t, s.retry)
	})

	t.Run("Disable without timer is safe", func(t *testing.T) {
		r, _ := newRestarter(t, StateStopped, true, 3, time.Second)
		r.Disable()
	})

	t.Run("SetEnabled(false) cancels retry but stays reusable", func(t *testing.T) {
		r, _ := newRestarter(t, StateCrashed, true, 3, time.Hour)
		r.onCrash(0)
		gen := snap(r).gen

		r.SetEnabled(false)

		s := snap(r)
		require.False(t, s.enabled)
		require.False(t, s.closed)
		require.NotEqual(t, gen, s.gen)
		require.Zero(t, s.retry)
	})

	t.Run("SetEnabled(true) resets counters", func(t *testing.T) {
		r, _ := newRestarter(t, StateCrashed, false, 1, time.Hour)
		r.mu.Lock()
		r.crashes, r.gaveUp, r.failedStart = 4, true, true
		r.mu.Unlock()

		r.SetEnabled(true)

		s := snap(r)
		require.True(t, s.enabled)
		require.Equal(t, 0, s.crashes)
		require.False(t, s.gaveUp)
		require.False(t, s.failedStart)
	})

	t.Run("same value is a no-op", func(t *testing.T) {
		r, _ := newRestarter(t, StateCrashed, true, 3, time.Hour)
		r.onCrash(0)
		before := snap(r)
		r.SetEnabled(true)
		require.Equal(t, before, snap(r))
	})
}

func TestStatus(t *testing.T) {
	tests := []struct {
		name      string
		state     State
		stable    time.Duration
		uptime    time.Duration
		wantReset bool
	}{
		{"running past stable uptime reports reset", StateRunning, time.Minute, 2 * time.Minute, true},
		{"running before stable uptime keeps counters", StateRunning, time.Hour, time.Minute, false},
		{"crashed keeps counters", StateCrashed, time.Minute, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, inst := newRestarter(t, tt.state, true, 3, time.Hour)
			r.mu.Lock()
			r.crashes, r.gaveUp, r.stable = 2, true, tt.stable
			r.mu.Unlock()
			if tt.uptime > 0 {
				inst.mu.Lock()
				inst.startedAt = time.Now().Add(-tt.uptime)
				inst.mu.Unlock()
			}

			s := r.Status()
			if tt.wantReset {
				require.Equal(t, 0, s.Restarts)
				require.False(t, s.GaveUp)
			}
			if !tt.wantReset {
				require.Equal(t, 2, s.Restarts)
				require.True(t, s.GaveUp)
			}
			require.Equal(t, 2, snap(r).crashes)
		})
	}
}

func TestFire(t *testing.T) {
	// crashed schedules one retry and returns the restarter plus its generation.
	crashed := func(t *testing.T, state State, max int) (*Restarter, uint64) {
		r, _ := newRestarter(t, state, true, max, time.Hour)
		r.onCrash(0)
		return r, snap(r).gen
	}

	t.Run("stale generation ignored", func(t *testing.T) {
		r, gen := crashed(t, StateCrashed, 3)
		r.fire(gen - 1)
		s := snap(r)
		require.Equal(t, 1, s.crashes)
		require.False(t, s.failedStart)
		require.NotZero(t, s.retry)
	})

	t.Run("disabled ignored", func(t *testing.T) {
		r, gen := crashed(t, StateCrashed, 3)
		r.mu.Lock()
		r.enabled = false
		r.mu.Unlock()
		r.fire(gen)

		s := snap(r)
		require.Equal(t, 1, s.crashes)
		require.False(t, s.failedStart)
	})

	for name, state := range map[string]State{"running": StateRunning, "starting": StateStarting, "stopping": StateStopping} {
		t.Run("bails when "+name, func(t *testing.T) {
			r, gen := crashed(t, state, 3)
			r.fire(gen)
			s := snap(r)
			require.Equal(t, 1, s.crashes)
			require.False(t, s.failedStart)
			require.Zero(t, s.retry)
		})
	}

	t.Run("manual stop is not restarted", func(t *testing.T) {
		r, gen := crashed(t, StateStopped, 3)
		r.fire(gen)
		s := snap(r)
		require.Equal(t, 1, s.crashes)
		require.False(t, s.failedStart)
	})

	t.Run("failed start reschedules", func(t *testing.T) {
		r, gen := crashed(t, StateCrashed, 5)
		r.fire(gen)
		s := snap(r)
		require.True(t, s.failedStart)
		require.Equal(t, 2, s.crashes)
		require.NotEqual(t, gen, s.gen)
		require.NotZero(t, s.retry)
	})

	t.Run("retries from stopped after a failed start", func(t *testing.T) {
		r, _ := newRestarter(t, StateStopped, true, 5, time.Hour)
		r.mu.Lock()
		r.crashes, r.failedStart = 1, true
		r.gen++
		gen := r.gen
		r.mu.Unlock()

		r.fire(gen)

		s := snap(r)
		require.Equal(t, 2, s.crashes)
		require.True(t, s.failedStart)
	})

	t.Run("repeated failures give up", func(t *testing.T) {
		r, gen := crashed(t, StateCrashed, 2)
		r.fire(gen)
		r.fire(snap(r).gen)

		s := snap(r)
		require.True(t, s.gaveUp)
		require.Equal(t, 2, s.crashes)
	})
}

func TestTimers(t *testing.T) {
	t.Run("retries until giving up", func(t *testing.T) {
		r, _ := newRestarter(t, StateCrashed, true, 3, 10*time.Millisecond)
		r.onCrash(0)

		deadline := time.Now().Add(5 * time.Second)
		for !snap(r).gaveUp {
			require.False(t, time.Now().After(deadline), "timed out waiting for give up")
			time.Sleep(5 * time.Millisecond)
		}
		s := snap(r)
		require.Equal(t, 3, s.crashes)
		require.Zero(t, s.retry)
	})

	t.Run("Disable stops a pending timer", func(t *testing.T) {
		r, _ := newRestarter(t, StateCrashed, true, 3, 30*time.Millisecond)
		r.onCrash(0)
		r.Disable()
		time.Sleep(150 * time.Millisecond)

		s := snap(r)
		require.False(t, s.failedStart)
		require.Equal(t, 1, s.crashes)
	})
}
