package minecraft

import (
	"errors"
	"log/slog"
	"sync"
	"time"
)

const (
	maxBackoff   = 5 * time.Minute
	stableUptime = 10 * time.Minute
)

type Restarter struct {
	inst *Instance

	mu          sync.Mutex
	enabled     bool
	maxRestarts int
	initial     time.Duration

	crashes     int
	gaveUp      bool
	failedStart bool
	gen         uint64
	timer       *time.Timer
}

func NewRestarter(inst *Instance, enabled bool, maxRestarts int, initial time.Duration) *Restarter {
	r := &Restarter{inst: inst, enabled: enabled, maxRestarts: maxRestarts, initial: initial}
	inst.SetOnCrash(r.onCrash)
	return r
}

func backoff(initial time.Duration, attempt int) time.Duration {
	d := initial
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= maxBackoff {
			return maxBackoff
		}
	}
	return min(d, maxBackoff)
}

func (r *Restarter) onCrash(ranFor time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.enabled {
		return
	}
	r.failedStart = false
	if ranFor >= stableUptime {
		r.crashes, r.gaveUp = 0, false
	}
	r.schedule()
}

// schedule must be called with r.mu held.
func (r *Restarter) schedule() {
	if r.crashes >= r.maxRestarts {
		r.gaveUp = true
		slog.Error("auto-restart giving up", "restarts", r.crashes)
		return
	}
	r.crashes++
	r.gen++
	gen := r.gen
	delay := backoff(r.initial, r.crashes)
	slog.Warn("auto-restart scheduled", "attempt", r.crashes, "max", r.maxRestarts, "in", delay.String())
	r.timer = time.AfterFunc(delay, func() { r.fire(gen) })
}

func (r *Restarter) fire(gen uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if gen != r.gen || !r.enabled {
		return
	}
	s := r.inst.State()
	if s != StateCrashed && !(s == StateStopped && r.failedStart) {
		return // a manual start or stop got there first
	}
	if err := r.inst.Start(); err != nil {
		if errors.Is(err, ErrInvalidState) {
			return
		}
		slog.Error("auto-restart start failed", "error", err)
		r.failedStart = true
		r.schedule()
	}
}

func (r *Restarter) Disable() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = false
	r.gen++
	if r.timer != nil {
		r.timer.Stop()
	}
}
