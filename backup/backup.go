// Package backup creates, lists, and deletes archives of the Minecraft world.
// A backup of a running server flushes the world to disk and pauses autosaving while the files are archived, then resumes autosaving even if the archive fails.
// Backups are identified by ID only, and only the newest configured number are kept.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/internal/fanout"
	"github.com/emiHuy/mc-server-manager/minecraft"
)

var (
	ErrFlushTimeout  = errors.New("backup: timed out waiting for world save")
	errConsoleClosed = errors.New("backup: console stream closed")
)

type Controller interface {
	Send(cmd string) error
	State() minecraft.State
	SubscribeConsole(n int) ([]string, *fanout.Subscription[string])
	PublishEvent(e minecraft.Event)
}

type Manager struct {
	ctl       Controller
	sourceDir string
	conf      config.BackupConfig
	mu        sync.Mutex
}

type Info struct {
	ID        string
	CreatedAt time.Time
	SizeBytes int64
}

const idTimeLayout = "20060102-150405"

var idPattern = regexp.MustCompile(`^backup-\d{8}-\d{6}$`)

func New(ctl Controller, sourceDir string, conf config.BackupConfig) *Manager {
	return &Manager{ctl: ctl, sourceDir: sourceDir, conf: conf}
}

func (m *Manager) Create(ctx context.Context) (Info, error) {
	if !m.mu.TryLock() {
		return Info{}, ErrInProgress
	}
	defer m.mu.Unlock()

	id := newID(time.Now())
	m.publish(minecraft.EventBackupStarted, id, "")

	// The flush happens in its own function so its deferred save-on
	// runs as soon as the files are safe, before retention.
	info, err := m.createLocked(ctx, id)
	if err != nil {
		slog.Error("backup failed", "id", id, "error", err)
		m.publish(minecraft.EventBackupFailed, id, failureReason(err))
		return Info{}, err
	}

	if err := m.enforceRetention(); err != nil {
		slog.Warn("backup retention failed", "error", err) // the backup itself succeeded
	}
	m.publish(minecraft.EventBackupCompleted, id, "")
	return info, nil
}

func (m *Manager) createLocked(ctx context.Context, id string) (Info, error) {
	if m.ctl.State() == minecraft.StateRunning {
		_, sub := m.ctl.SubscribeConsole(0)
		defer sub.Cancel()

		if err := m.ctl.Send("save-off"); err != nil {
			return Info{}, err
		}
		defer func() {
			if err := m.ctl.Send("save-on"); err != nil {
				slog.Warn("save-on failed", "error", err)
			}
		}()

		if err := m.ctl.Send("save-all flush"); err != nil {
			return Info{}, err
		}
		if err := waitFor(ctx, sub, "Saved the game", m.conf.FlushTimeout); err != nil {
			return Info{}, err
		}
	}

	return m.archive(id) // runs while autosave is off; save-on fires when this returns
}

func (m *Manager) List() ([]Info, error) {
	entries, err := os.ReadDir(m.conf.Directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var infos []Info
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".zip")
		if e.IsDir() || !ok || !idPattern.MatchString(id) {
			continue // skips .tmp files and anything stray
		}

		created, err := parseID(id)
		if err != nil {
			continue
		}

		fileInfo, err := e.Info()
		if err != nil {
			continue // deleted between ReadDir and Info
		}

		infos = append(infos, Info{ID: id, CreatedAt: created, SizeBytes: fileInfo.Size()})
	}

	slices.SortFunc(infos, func(a, b Info) int {
		return strings.Compare(b.ID, a.ID) // newest first
	})
	return infos, nil
}

func (m *Manager) Delete(id string) error {
	if !idPattern.MatchString(id) {
		return ErrInvalidID
	}

	err := os.Remove(m.path(id))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

// archive zips the world folder into <id>.zip in the backup directory.
// It writes to a .tmp file first so a crash never leaves a half-written .zip.
func (m *Manager) archive(id string) (Info, error) {
	if err := os.MkdirAll(m.conf.Directory, 0o755); err != nil {
		return Info{}, err
	}

	final := m.path(id)
	tmp := final + ".tmp"

	if _, err := os.Stat(final); err == nil {
		return Info{}, fmt.Errorf("backup %s already exists", id) // same-second collision
	}

	if err := writeZip(tmp, m.sourceDir, m.conf.FoldersToSave); err != nil {
		os.Remove(tmp)
		return Info{}, err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return Info{}, err
	}

	fi, err := os.Stat(final)
	if err != nil {
		return Info{}, err
	}
	created, _ := parseID(id) // id was just made by newID, so this can't fail
	return Info{ID: id, CreatedAt: created, SizeBytes: fi.Size()}, nil
}

// enforceRetention deletes all but the newest conf.Keep backups.
func (m *Manager) enforceRetention() error {
	if m.conf.Keep < 1 {
		return nil // config validation should prevent this; never delete everything
	}

	infos, err := m.List()
	if err != nil {
		return err
	}
	if len(infos) <= m.conf.Keep {
		return nil
	}

	var errs []error
	for _, info := range infos[m.conf.Keep:] {
		if err := m.Delete(info.ID); err != nil && !errors.Is(err, ErrNotFound) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) path(id string) string {
	return filepath.Join(m.conf.Directory, id+".zip")
}

func (m *Manager) publish(t minecraft.EventType, id, message string) {
	m.ctl.PublishEvent(minecraft.Event{Type: t, Backup: id, Message: message})
}

// waitFor blocks until a console line contains substr, the timeout passes, or ctx is done.
func waitFor(ctx context.Context, sub *fanout.Subscription[string], substr string, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case line, ok := <-sub.C:
			if !ok {
				return errConsoleClosed
			}
			if strings.Contains(line, substr) {
				return nil
			}
		case <-timer.C:
			return ErrFlushTimeout
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// newID returns a backup ID for time t, e.g. "backup-20261112-153000".
func newID(t time.Time) string {
	return "backup-" + t.UTC().Format(idTimeLayout)
}

// parseID extracts the creation time from a valid backup ID.
func parseID(id string) (time.Time, error) {
	return time.Parse(idTimeLayout, strings.TrimPrefix(id, "backup-"))
}
