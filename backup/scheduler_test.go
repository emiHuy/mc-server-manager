package backup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeBackupper struct {
	infos     []Info
	listErr   error
	changed   bool
	changeErr error
	createErr error
	creates   int
}

func (f *fakeBackupper) Create(context.Context) (Info, error) {
	f.creates++
	return Info{ID: "backup-20260101-000000"}, f.createErr
}
func (f *fakeBackupper) List() ([]Info, error)                { return f.infos, f.listErr }
func (f *fakeBackupper) changedSince(time.Time) (bool, error) { return f.changed, f.changeErr }

func TestAttempt(t *testing.T) {
	const interval = time.Hour
	existing := []Info{{ID: "backup-20260101-000000", CreatedAt: time.Now()}}

	tests := []struct {
		name        string
		fake        fakeBackupper
		wantDelay   time.Duration
		wantCreates int
	}{
		{"first backup", fakeBackupper{}, interval, 1},
		{"world changed", fakeBackupper{infos: existing, changed: true}, interval, 1},
		{"world unchanged", fakeBackupper{infos: existing}, interval, 0},
		{"change check fails, backs up anyway", fakeBackupper{infos: existing, changeErr: errors.New("x")}, interval, 1},
		{"list fails, backs up anyway", fakeBackupper{listErr: errors.New("x")}, interval, 1},
		{"in progress", fakeBackupper{createErr: ErrInProgress}, busyRetry, 1},
		{"server busy", fakeBackupper{createErr: ErrServerBusy}, busyRetry, 1},
		{"other failure", fakeBackupper{createErr: errors.New("boom")}, failureRetry, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScheduler(&tt.fake, interval)
			require.Equal(t, tt.wantDelay, s.attempt(context.Background()))
			require.Equal(t, tt.wantCreates, tt.fake.creates)

			_, ok := s.NextRun()
			require.False(t, ok)
		})
	}
}

func TestAttemptRetryNeverExceedsInterval(t *testing.T) {
	s := NewScheduler(&fakeBackupper{createErr: errors.New("boom")}, 30*time.Second)
	require.Equal(t, 30*time.Second, s.attempt(context.Background()))
}

func TestAttemptCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := NewScheduler(&fakeBackupper{createErr: context.Canceled}, time.Hour)
	require.Equal(t, time.Hour, s.attempt(ctx))
}

func TestDelayAfter(t *testing.T) {
	now := time.Now()
	const interval = time.Hour

	tests := []struct {
		name string
		last time.Time
		want time.Duration
	}{
		{"just taken", now, interval},
		{"halfway", now.Add(-30 * time.Minute), 30 * time.Minute},
		{"overdue", now.Add(-5 * time.Hour), startupDelay},
		{"almost due", now.Add(-interval + time.Second), startupDelay},
		{"clock skew, in the future", now.Add(24 * time.Hour), interval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, delayAfter(tt.last, now, interval))
		})
	}
}

func TestInitialDelay(t *testing.T) {
	s := NewScheduler(&fakeBackupper{}, time.Hour)
	require.Equal(t, startupDelay, s.initialDelay())

	s = NewScheduler(&fakeBackupper{listErr: errors.New("x")}, time.Hour)
	require.Equal(t, startupDelay, s.initialDelay())
}

func TestNilScheduler(t *testing.T) {
	var s *Scheduler
	s.Stop() // must not panic

	_, ok := s.NextRun()
	require.False(t, ok)
}

func TestStartStop(t *testing.T) {
	s := NewScheduler(&fakeBackupper{}, time.Hour)
	s.Start()
	s.Start() // second call is a no-op

	deadline := time.Now().Add(time.Second)
	for {
		if next, ok := s.NextRun(); ok {
			require.Greater(t, time.Until(next), time.Duration(0))
			break
		}
		require.False(t, time.Now().After(deadline))
		time.Sleep(time.Millisecond)
	}

	s.Stop()

	_, ok := s.NextRun()
	require.False(t, ok)
}
