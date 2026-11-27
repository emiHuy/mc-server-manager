package minecraft

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emiHuy/mc-server-manager/internal/pidfile"
	"github.com/emiHuy/mc-server-manager/internal/proc"
)

const (
	pidfileName         = "mcsm.pid"
	staleStartTolerance = 5 * time.Second
)

// CheckStale refuses if the pidfile points at a live Java process.
// A pidfile that is unreadable, or that points at a dead or unrelated process, is a leftover and is removed.
// Caller must hold mu
func checkStale(dir string) error {
	path := pidfilePath(dir)

	info, err := pidfile.Read(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		slog.Warn("minecraft: removing unreadable pidfile", "path", path, "error", err)
		removePidfile(dir)
		return nil
	}

	p, err := proc.Lookup(info.PID)
	if errors.Is(err, proc.ErrNotFound) {
		slog.Info("minecraft: removing stale pidfile, process is gone", "pid", info.PID)
		removePidfile(dir)
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot check whether minecraft server (pid %d) is still running: %w", info.PID, err)
	}

	// The real server started before the pidfile was written. A process that
	// started later is an unrelated one that reused the PID.
	if !looksLikeJava(p.Name) || p.StartedAt.After(info.StartedAt.Add(staleStartTolerance)) {
		slog.Info("minecraft: removing stale pidfile, pid belongs to another process", "pid", info.PID, "name", p.Name)
		removePidfile(dir)
		return nil
	}

	return fmt.Errorf("a minecraft server is already running (pid %d, started %s); stop it or delete %s if it is not one", info.PID, info.StartedAt.Format(time.RFC3339), path)
}

// writePidfile records the server's PID and start time.
func writePidfile(dir string, pid int) {
	path := pidfilePath(dir)

	info := pidfile.Info{PID: pid, StartedAt: time.Now()}
	if err := pidfile.Write(path, info); err != nil {
		slog.Warn("minecraft: cannot write pidfile", "path", path, "error", err)
	}
}

// removePidfile deletes the pidfile. It runs with inst.mu held.
func removePidfile(dir string) {
	path := pidfilePath(dir)
	if err := pidfile.Remove(path); err != nil {
		slog.Warn("minecraft: cannot remove pidfile", "path", path, "error", err)
	}
}

func looksLikeJava(name string) bool {
	return strings.Contains(strings.ToLower(name), "java")
}

func pidfilePath(dir string) string {
	return filepath.Join(dir, pidfileName)
}
