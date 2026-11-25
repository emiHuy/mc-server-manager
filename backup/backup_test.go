package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/internal/fanout"
	"github.com/emiHuy/mc-server-manager/minecraft"
	"github.com/stretchr/testify/require"
)

// fakeController is a Controller for a server whose state is set by the test.
// The zero state is assumed to be a stopped server (no console handshake).
type fakeController struct {
	state  minecraft.State
	events []minecraft.Event
}

func (f *fakeController) Send(string) error      { return nil }
func (f *fakeController) State() minecraft.State { return f.state }
func (f *fakeController) SubscribeConsole(int) ([]string, *fanout.Subscription[string]) {
	return nil, nil
}
func (f *fakeController) PublishEvent(e minecraft.Event) { f.events = append(f.events, e) }

func (f *fakeController) eventTypes() []minecraft.EventType {
	var types []minecraft.EventType
	for _, e := range f.events {
		types = append(types, e.Type)
	}
	return types
}

func newTestManager(t *testing.T, keep int) (*Manager, *fakeController) {
	t.Helper()
	src := t.TempDir()
	writeTree(t, src, map[string]string{"world/level.dat": "level"})

	ctl := &fakeController{}
	m := New(ctl, src, config.BackupConfig{
		Directory:    filepath.Join(t.TempDir(), "backups"),
		World:        "world",
		Keep:         keep,
		FlushTimeout: time.Second,
	})
	return m, ctl
}

// seed creates empty backup files so List/Delete have something to work with.
func seed(t *testing.T, m *Manager, ids ...string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(m.cfg.Directory, 0o755))

	for _, id := range ids {
		require.NoError(t, os.WriteFile(m.path(id), []byte("x"), 0o644))
	}
}

func TestCreateStopped(t *testing.T) {
	m, ctl := newTestManager(t, 5)

	info, err := m.Create(context.Background())
	require.NoError(t, err)
	require.True(t, idPattern.MatchString(info.ID))
	require.Greater(t, info.SizeBytes, int64(0))

	infos, err := m.List()
	require.NoError(t, err)
	require.Len(t, infos, 1)
	require.Equal(t, info.ID, infos[0].ID)

	want := []minecraft.EventType{minecraft.EventBackupStarted, minecraft.EventBackupCompleted}
	require.Equal(t, want, ctl.eventTypes())
}

func TestCreateRejected(t *testing.T) {
	t.Run("server busy", func(t *testing.T) {
		m, ctl := newTestManager(t, 5)
		ctl.state = minecraft.StateStarting

		_, err := m.Create(context.Background())
		require.ErrorIs(t, err, ErrServerBusy)

		require.Len(t, ctl.events, 0)
	})

	t.Run("already in progress", func(t *testing.T) {
		m, _ := newTestManager(t, 5)
		m.mu.Lock()
		defer m.mu.Unlock()

		_, err := m.Create(context.Background())
		require.ErrorIs(t, err, ErrInProgress)
	})
}

func TestCreateFailurePublishesEvent(t *testing.T) {
	m, ctl := newTestManager(t, 5)
	m.cfg.World = "missing"

	_, err := m.Create(context.Background())
	require.Error(t, err)

	want := []minecraft.EventType{minecraft.EventBackupStarted, minecraft.EventBackupFailed}
	require.Equal(t, want, ctl.eventTypes())

	require.Equal(t, "backup failed", ctl.events[1].Message)

	left, _ := filepath.Glob(filepath.Join(m.cfg.Directory, "*.tmp"))
	require.Len(t, left, 0)
}

func TestListSortsAndSkipsJunk(t *testing.T) {
	m, _ := newTestManager(t, 5)
	seed(t, m, "backup-20260101-000000", "backup-20260103-000000", "backup-20260102-000000")
	for _, junk := range []string{"notes.txt", "backup-bad.zip", "backup-20260101-000000.zip.tmp"} {
		require.NoError(t, os.WriteFile(filepath.Join(m.cfg.Directory, junk), nil, 0o644))
	}

	infos, err := m.List()
	require.NoError(t, err)

	var ids []string
	for _, i := range infos {
		ids = append(ids, i.ID)
	}
	want := []string{"backup-20260103-000000", "backup-20260102-000000", "backup-20260101-000000"}
	require.Equal(t, want, ids)
}

func TestListMissingDirectory(t *testing.T) {
	m, _ := newTestManager(t, 5)

	infos, err := m.List()
	require.NoError(t, err)
	require.Nil(t, infos)
}

func TestDeleteAndExists(t *testing.T) {
	m, _ := newTestManager(t, 5)
	const id = "backup-20260101-000000"
	seed(t, m, id)

	p, err := m.BackupExists(id)
	require.NoError(t, err)
	require.Equal(t, m.path(id), p)

	require.ErrorIs(t, m.Delete("../etc/passwd"), ErrInvalidID)
	require.NoError(t, m.Delete(id))
	require.ErrorIs(t, m.Delete(id), ErrNotFound)

	_, err = m.BackupExists(id)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestEnforceRetention(t *testing.T) {
	m, _ := newTestManager(t, 2)
	seed(t, m, "backup-20260101-000000", "backup-20260102-000000", "backup-20260103-000000")

	require.NoError(t, m.enforceRetention())

	infos, _ := m.List()
	require.Len(t, infos, 2)
	require.Equal(t, "backup-20260103-000000", infos[0].ID)
	require.Equal(t, "backup-20260102-000000", infos[1].ID)

	m.cfg.Keep = 0 // retention disabled
	require.NoError(t, m.enforceRetention())

	infos, _ = m.List()
	require.Len(t, infos, 2)
}

func TestChangedSince(t *testing.T) {
	m, _ := newTestManager(t, 5)
	base := time.Now().Add(-time.Hour).Truncate(time.Second)

	world := filepath.Join(m.sourceDir, "world")
	require.NoError(t, os.Chtimes(filepath.Join(world, "level.dat"), base, base))

	// session.lock is touched constantly by a running server and must be ignored.
	writeTree(t, m.sourceDir, map[string]string{"world/session.lock": "lock"})

	changed, err := m.changedSince(base.Add(time.Minute))
	require.NoError(t, err)
	require.False(t, changed)

	changed, err = m.changedSince(base.Add(-time.Minute))
	require.NoError(t, err)
	require.True(t, changed)
}

func TestIDRoundTrip(t *testing.T) {
	at := time.Date(2026, 11, 12, 15, 30, 0, 0, time.UTC)
	id := newID(at)
	require.Equal(t, "backup-20261112-153000", id)

	got, err := parseID(id)
	require.NoError(t, err)
	require.Equal(t, at, got)
}

func TestFailureReason(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{ErrFlushTimeout, "timed out waiting for the world save"},
		{context.Canceled, "cancelled"},
		{errors.New("open /secret/path: denied"), "backup failed"},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, failureReason(tt.err))
	}
}
