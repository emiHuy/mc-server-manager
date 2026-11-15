package backup

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// The runner tests use testing/synctest (Go 1.25 or later). Inside a synctest bubble,
// time.Now and timers run on a fake clock that jumps forward whenever every goroutine
// is blocked, so a six-hour interval passes instantly. The clock starts at midnight
// UTC on 1 January 2000, so tests measure everything as an offset from time.Now() at the start.

const testInterval = 6 * time.Hour

// fakeBackupper stands in for Manager.
type fakeBackupper struct {
	mu sync.Mutex

	infos      []Info // newest first; a successful Create adds to the front
	listErr    error
	changed    bool // what changedSince reports
	changedErr error
	createErrs []error // consumed one per Create call; a nil entry, or none left, means success
	block      bool    // Create waits until its context ends

	creates []time.Time // when each Create call started
	since   []time.Time // the argument of each changedSince call
}

var _ backupper = (*fakeBackupper)(nil)

func (f *fakeBackupper) Create(ctx context.Context) (Info, error) {
	f.mu.Lock()
	f.creates = append(f.creates, time.Now())
	block := f.block
	var err error
	if len(f.createErrs) > 0 {
		err, f.createErrs = f.createErrs[0], f.createErrs[1:]
	}
	f.mu.Unlock()

	if block {
		<-ctx.Done()
		return Info{}, ctx.Err()
	}
	if err != nil {
		return Info{}, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	info := Info{ID: newID(now), CreatedAt: now}
	f.infos = append([]Info{info}, f.infos...)
	return info, nil
}

func (f *fakeBackupper) List() ([]Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.listErr != nil {
		return nil, f.listErr
	}
	return slices.Clone(f.infos), nil
}

func (f *fakeBackupper) changedSince(t time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.since = append(f.since, t)
	return f.changed, f.changedErr
}

func (f *fakeBackupper) setChanged(changed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changed = changed
}

// createOffsets returns when each Create call started, measured from start.
func (f *fakeBackupper) createOffsets(start time.Time) []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()

	var offsets []time.Duration
	for _, at := range f.creates {
		offsets = append(offsets, at.Sub(start))
	}
	return offsets
}

func (f *fakeBackupper) sinceCalls() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.since)
}

// advance moves the fake clock forward by d, then lets the scheduler finish whatever became due.
func advance(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func checkOffsets(t *testing.T, f *fakeBackupper, start time.Time, want ...time.Duration) {
	t.Helper()

	got := f.createOffsets(start)
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !slices.Equal(got, want) {
		t.Fatalf("backups were created at %v, want %v", got, want)
	}
}

func checkNextRun(t *testing.T, s *Scheduler, want time.Time) {
	t.Helper()

	got, ok := s.NextRun()
	if !ok || !got.Equal(want) {
		t.Fatalf("NextRun() = %v, %v, want %v, true", got, ok, want)
	}
}

// ---------------------------------------------------------------------------
// delayAfter
// ---------------------------------------------------------------------------

func TestDelayAfter(t *testing.T) {
	now := time.Date(2026, time.November, 14, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		last time.Time
		want time.Duration
	}{
		{"taken an hour ago", now.Add(-time.Hour), 5 * time.Hour},
		{"due right now", now.Add(-testInterval), startupDelay},
		{"overdue", now.Add(-10 * time.Hour), startupDelay},
		{"due sooner than the startup delay", now.Add(-testInterval + 30*time.Second), startupDelay},
		{"due just after the startup delay", now.Add(-testInterval + 2*time.Minute), 2 * time.Minute},
		{"dated in the future waits one interval at most", now.Add(time.Hour), testInterval},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := delayAfter(tt.last, now, testInterval); got != tt.want {
				t.Fatalf("delayAfter() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Planning
// ---------------------------------------------------------------------------

func TestSchedulerFirstBackupWhenNoneExist(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		f := &fakeBackupper{changed: true}
		s := NewScheduler(f, testInterval)
		s.Start()
		defer s.Stop()
		synctest.Wait()

		checkNextRun(t, s, start.Add(startupDelay))

		advance(startupDelay)
		advance(testInterval)

		checkOffsets(t, f, start, startupDelay, startupDelay+testInterval)
	})
}

func TestSchedulerPlansFromNewestBackup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		taken := start.Add(-time.Hour)
		f := &fakeBackupper{changed: true, infos: []Info{{ID: newID(taken), CreatedAt: taken}}}
		s := NewScheduler(f, testInterval)
		s.Start()
		defer s.Stop()
		synctest.Wait()

		// One hour into a six-hour interval, so five hours remain, not a fresh interval.
		checkNextRun(t, s, start.Add(5*time.Hour))

		advance(5*time.Hour - time.Second)
		checkOffsets(t, f, start) // nothing yet

		advance(time.Second)
		checkOffsets(t, f, start, 5*time.Hour)
	})
}

func TestSchedulerOverdueBackupRunsAfterStartupDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		taken := start.Add(-10 * time.Hour)
		f := &fakeBackupper{changed: true, infos: []Info{{ID: newID(taken), CreatedAt: taken}}}
		s := NewScheduler(f, testInterval)
		s.Start()
		defer s.Stop()

		advance(startupDelay)

		checkOffsets(t, f, start, startupDelay)
	})
}

func TestSchedulerStartTwiceRunsOneLoop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		f := &fakeBackupper{changed: true}
		s := NewScheduler(f, testInterval)
		s.Start()
		s.Start()
		defer s.Stop()

		advance(startupDelay)

		checkOffsets(t, f, start, startupDelay)
	})
}

// ---------------------------------------------------------------------------
// Skipping unchanged worlds
// ---------------------------------------------------------------------------

func TestSchedulerSkipsUnchangedWorld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		taken := start.Add(-time.Hour)
		f := &fakeBackupper{changed: false, infos: []Info{{ID: newID(taken), CreatedAt: taken}}}
		s := NewScheduler(f, testInterval)
		s.Start()
		defer s.Stop()

		advance(5 * time.Hour) // the first check

		checkOffsets(t, f, start) // skipped
		if got := f.sinceCalls(); len(got) != 1 || !got[0].Equal(taken) {
			t.Fatalf("changedSince was asked about %v, want [%v] (the newest backup)", got, taken)
		}
		// Skipped, not delayed: the next check is a full interval later.
		checkNextRun(t, s, start.Add(5*time.Hour+testInterval))

		f.setChanged(true)
		advance(testInterval)

		checkOffsets(t, f, start, 5*time.Hour+testInterval)
	})
}

func TestSchedulerBacksUpWhenItCannotTellIfTheWorldChanged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		taken := start.Add(-time.Hour)
		f := &fakeBackupper{
			changedErr: errors.New("walk failed"),
			infos:      []Info{{ID: newID(taken), CreatedAt: taken}},
		}
		s := NewScheduler(f, testInterval)
		s.Start()
		defer s.Stop()

		advance(5 * time.Hour)

		checkOffsets(t, f, start, 5*time.Hour)
	})
}

func TestSchedulerBacksUpWhenBackupsCannotBeListed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		f := &fakeBackupper{listErr: errors.New("directory unreadable")}
		s := NewScheduler(f, testInterval)
		s.Start()
		defer s.Stop()

		advance(startupDelay)

		checkOffsets(t, f, start, startupDelay)
	})
}

// ---------------------------------------------------------------------------
// Retries
// ---------------------------------------------------------------------------

func TestSchedulerRetriesSoonWhenPostponed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		f := &fakeBackupper{
			changed: true,
			createErrs: []error{
				ErrServerBusy,
				fmt.Errorf("manual backup running: %w", ErrInProgress),
				nil,
			},
		}
		s := NewScheduler(f, testInterval)
		s.Start()
		defer s.Stop()

		advance(startupDelay) // postponed: the server is starting or stopping
		advance(busyRetry)    // postponed: another backup is running
		advance(busyRetry)    // succeeds
		advance(testInterval) // and then the normal interval applies

		checkOffsets(t, f, start,
			startupDelay,
			startupDelay+busyRetry,
			startupDelay+2*busyRetry,
			startupDelay+2*busyRetry+testInterval,
		)
	})
}

func TestSchedulerRetriesAfterFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		f := &fakeBackupper{changed: true, createErrs: []error{errors.New("disk full"), nil}}
		s := NewScheduler(f, testInterval)
		s.Start()
		defer s.Stop()

		advance(startupDelay)
		advance(failureRetry)
		advance(testInterval)

		checkOffsets(t, f, start,
			startupDelay,
			startupDelay+failureRetry,
			startupDelay+failureRetry+testInterval,
		)
	})
}

func TestSchedulerRetryIsNeverLongerThanTheInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const interval = 5 * time.Minute // shorter than failureRetry

		start := time.Now()
		f := &fakeBackupper{changed: true, createErrs: []error{errors.New("disk full"), nil}}
		s := NewScheduler(f, interval)
		s.Start()
		defer s.Stop()

		advance(startupDelay)
		advance(interval)

		checkOffsets(t, f, start, startupDelay, startupDelay+interval)
	})
}

// ---------------------------------------------------------------------------
// Stopping
// ---------------------------------------------------------------------------

func TestSchedulerStopWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		f := &fakeBackupper{changed: true}
		s := NewScheduler(f, testInterval)
		s.Start()
		synctest.Wait()

		s.Stop()
		s.Stop() // a second call is harmless

		if next, ok := s.NextRun(); ok {
			t.Errorf("NextRun() = %v after Stop, want nothing planned", next)
		}

		advance(2 * testInterval)
		checkOffsets(t, f, start) // never fired
	})
}

func TestSchedulerStopCancelsRunningBackup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		f := &fakeBackupper{block: true}
		s := NewScheduler(f, testInterval)
		s.Start()
		defer s.Stop()

		advance(startupDelay) // the scheduler is now inside Create, waiting for its context

		if next, ok := s.NextRun(); ok {
			t.Errorf("NextRun() = %v while a backup is running, want nothing", next)
		}

		s.Stop() // must return even though Create is blocked

		checkOffsets(t, f, start, startupDelay)
		if next, ok := s.NextRun(); ok {
			t.Errorf("NextRun() = %v after Stop, want nothing planned", next)
		}
	})
}

func TestNilSchedulerIsSafe(t *testing.T) {
	var s *Scheduler

	s.Stop()
	if next, ok := s.NextRun(); ok {
		t.Fatalf("NextRun() = %v on a nil scheduler, want nothing", next)
	}
}
