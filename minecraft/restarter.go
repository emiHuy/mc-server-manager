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
	closed      bool
	maxRestarts int
	initial     time.Duration
	stable      time.Duration

	crashes     int
	gaveUp      bool
	failedStart bool
	gen         uint64
	timer       *time.Timer

	nextRetryAt time.Time
}

type RestarterSnapshot struct {
	Enabled     bool
	Restarts    int
	MaxRestarts int
	NextRetryAt time.Time
	GaveUp      bool
}

func NewRestarter(inst *Instance, enabled bool, maxRestarts int, initial time.Duration) *Restarter {
	r := &Restarter{inst: inst, enabled: enabled, maxRestarts: maxRestarts, initial: initial, stable: stableUptime}
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
	if ranFor >= r.stable {
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
	r.nextRetryAt = time.Now().Add(delay)
	slog.Warn("auto-restart scheduled", "attempt", r.crashes, "max", r.maxRestarts, "in", delay.String())
	r.timer = time.AfterFunc(delay, func() { r.fire(gen) })
}

func (r *Restarter) fire(gen uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if gen != r.gen || !r.enabled {
		return
	}
	r.nextRetryAt = time.Time{}
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

// Disable is the terminal shutdown path: it cancels any retry and refuses to be re-enabled.
func (r *Restarter) Disable() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true
	r.enabled = false

	r.cancelLocked()
}

// cancelLocked must be called with r.mu held.
func (r *Restarter) cancelLocked() {
	r.gen++
	if r.timer != nil {
		r.timer.Stop()
	}
	r.nextRetryAt = time.Time{}
}

func (r *Restarter) Status() RestarterSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	s := RestarterSnapshot{
		Enabled:     r.enabled,
		Restarts:    r.crashes,
		MaxRestarts: r.maxRestarts,
		NextRetryAt: r.nextRetryAt,
		GaveUp:      r.gaveUp,
	}

	is := r.inst.Status()
	if is.State == StateRunning && is.Uptime >= r.stable {
		s.Restarts, s.GaveUp = 0, false
	}

	return s
}

func (r *Restarter) SetEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed || r.enabled == enabled {
		return
	}

	r.enabled = enabled
	if enabled {
		r.crashes, r.gaveUp, r.failedStart = 0, false, false
		return
	}

	r.cancelLocked()
}
