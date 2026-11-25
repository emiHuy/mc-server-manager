package backup

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

const (
	startupDelay = time.Minute
	busyRetry    = time.Minute
	failureRetry = 10 * time.Minute
)

// backupper is the part of Manager that the scheduler uses.
type backupper interface {
	Create(ctx context.Context) (Info, error)
	List() ([]Info, error)
	changedSince(t time.Time) (bool, error)
}

// Scheduler creates a backup every interval while the world is changing.
//
// A nil *Scheduler is safe to use and does nothing.
type Scheduler struct {
	backups  backupper
	interval time.Duration

	mu     sync.Mutex
	next   time.Time // when the next attempt is planned; zero while an attempt is running
	cancel context.CancelFunc
	done   chan struct{}
}

func NewScheduler(backups backupper, interval time.Duration) *Scheduler {
	return &Scheduler{backups: backups, interval: interval}
}

// Start begins scheduling in the background. Calling it again does nothing.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.cancel = cancel
	s.done = done

	go func() {
		defer close(done)
		s.run(ctx)
	}()
}

// Stop cancels the scheduler and waits for it to finish.
//
// A backup that is waiting for the world save is cancelled and autosaving is restored.
// A backup that is already writing its archive is allowed to finish.
func (s *Scheduler) Stop() {
	if s == nil {
		return
	}

	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.mu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	<-done
	s.setNext(time.Time{})
}

// NextRun reports when the next backup attempt is planned.
//
// ok is false when scheduling is off or an attempt is running right now.
func (s *Scheduler) NextRun() (t time.Time, ok bool) {
	if s == nil {
		return time.Time{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next, !s.next.IsZero()
}

func (s *Scheduler) setNext(t time.Time) {
	s.mu.Lock()
	s.next = t
	s.mu.Unlock()
}

func (s *Scheduler) run(ctx context.Context) {
	delay := s.initialDelay()
	for {
		if !s.wait(ctx, delay) {
			return
		}
		delay = s.attempt(ctx)
	}
}

// wait sleeps for d. It reports false if ctx ended first.
func (s *Scheduler) wait(ctx context.Context, d time.Duration) bool {
	s.setNext(time.Now().Add(d))

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Scheduler) initialDelay() time.Duration {
	infos, err := s.backups.List()
	if err != nil {
		slog.Warn("cannot read existing backups, scheduling the first backup soon", "error", err)
		return startupDelay
	}
	if len(infos) == 0 {
		return startupDelay
	}
	return delayAfter(infos[0].CreatedAt, time.Now(), s.interval)
}

// attempt tries one scheduled backup and returns how long to wait before the next try.
func (s *Scheduler) attempt(ctx context.Context) time.Duration {
	s.setNext(time.Time{})

	if s.worldUnchanged() {
		slog.Info("scheduled backup skipped, the world has not changed since the last backup")
		return s.interval
	}

	info, err := s.backups.Create(ctx)
	switch {
	case err == nil:
		slog.Info("scheduled backup created", "id", info.ID)
		return s.interval
	case errors.Is(err, ErrInProgress), errors.Is(err, ErrServerBusy):
		slog.Info("scheduled backup postponed", "reason", err)
		return min(busyRetry, s.interval)
	case ctx.Err() != nil:
		return s.interval // shutting down; run sees the cancelled context at the next wait
	default:
		// Create has already logged the details and published the failure event.
		slog.Error("scheduled backup failed", "error", err)
		return min(failureRetry, s.interval)
	}
}

// worldUnchanged reports whether nothing in the world was written since the newest backup.
//
// A server that has been stopped for days would otherwise fill the retention limit with identical copies.
// If it cannot tell, it says false, so the answer errs towards taking a backup.
func (s *Scheduler) worldUnchanged() bool {
	infos, err := s.backups.List()
	if err != nil {
		slog.Warn("cannot read existing backups, backing up anyway", "error", err)
		return false
	}
	if len(infos) == 0 {
		return false
	}

	changed, err := s.backups.changedSince(infos[0].CreatedAt)
	if err != nil {
		slog.Warn("cannot check the world for changes, backing up anyway", "error", err)
		return false
	}
	return !changed
}

// delayAfter returns how long to wait for the next backup when the newest one was taken at last.
//
// A backup that is already overdue runs after startupDelay.
func delayAfter(last, now time.Time, interval time.Duration) time.Duration {
	remaining := min(last.Add(interval).Sub(now), interval)
	return max(remaining, startupDelay)
}
