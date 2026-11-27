package minecraft

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/internal/pidfile"
	"github.com/stretchr/testify/require"
)

func TestLooksLikeJava(t *testing.T) {
	tests := []struct {
		name string
		proc string
		want bool
	}{
		{"java", "java", true},
		{"java.exe", "java.exe", true},
		{"javaw.exe", "javaw.exe", true},
		{"uppercase", "Java", true},
		{"mixed case exe", "JAVA.EXE", true},
		{"bash", "bash", false},
		{"test binary", "minecraft.test.exe", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, looksLikeJava(tt.proc))
		})
	}
}

func TestPidfileWriteRemove(t *testing.T) {
	dir := t.TempDir()

	before := time.Now().Add(-time.Second)
	writePidfile(dir, 1234)

	info, err := pidfile.Read(pidfilePath(dir))
	require.NoError(t, err)

	require.Equal(t, 1234, info.PID)
	require.False(t, info.StartedAt.Before(before))
	require.False(t, info.StartedAt.After(time.Now().Add(time.Second)))

	removePidfile(dir)
	_, err = os.Stat(pidfilePath(dir))
	require.ErrorIs(t, err, os.ErrNotExist)

	removePidfile(dir)
}

func TestCheckStale(t *testing.T) {
	writeInfo := func(t *testing.T, path string, pid int, startedAt time.Time) {
		t.Helper()
		require.NoError(t, pidfile.Write(path, pidfile.Info{PID: pid, StartedAt: startedAt}))
	}

	tests := []struct {
		name        string
		setup       func(t *testing.T, path string) // nil means no pidfile
		wantErr     bool
		wantRemoved bool
	}{
		{
			name:        "no pidfile",
			setup:       nil,
			wantRemoved: true, // nothing to keep either
		},
		{
			name: "corrupt pidfile",
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.WriteFile(path, []byte("not json"), 0o644))
			},
			wantRemoved: true,
		},
		{
			name: "dead pid",
			setup: func(t *testing.T, path string) {
				writeInfo(t, path, deadPID(t), time.Now())
			},
			wantRemoved: true,
		},
		{
			name: "live pid that is not java",
			setup: func(t *testing.T, path string) {
				writeInfo(t, path, os.Getpid(), time.Now())
			},
			wantRemoved: true,
		},
		{
			name: "live java process refuses and keeps the pidfile",
			setup: func(t *testing.T, path string) {
				writeInfo(t, path, startFakeJava(t), time.Now())
			},
			wantErr:     true,
			wantRemoved: false,
		},
		{
			name: "java process that started after the pidfile claims, so the pid was reused",
			setup: func(t *testing.T, path string) {
				writeInfo(t, path, startFakeJava(t), time.Now().Add(-time.Hour))
			},
			wantRemoved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := pidfilePath(dir)

			if tt.setup != nil {
				tt.setup(t, path)
			}

			err := checkStale(dir)
			require.Equal(t, tt.wantErr, err != nil)

			_, statErr := os.Stat(path)
			removed := errors.Is(statErr, os.ErrNotExist)
			require.Equal(t, tt.wantRemoved, removed)
		})
	}
}

func deadPID(t *testing.T) int {
	t.Helper()

	exe, err := os.Executable()
	require.NoError(t, err)

	cmd := exec.Command(exe, "-test.run=^$")
	err = cmd.Run()
	require.NoError(t, err)

	return cmd.Process.Pid
}

func startFakeJava(t *testing.T) int {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("needs a unix sleep binary")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not found")
	}

	data, err := os.ReadFile(sleep)
	if err != nil {
		t.Skipf("cannot read sleep binary: %v", err)
	}
	javaPath := filepath.Join(t.TempDir(), "java")
	if err := os.WriteFile(javaPath, data, 0o755); err != nil {
		t.Skipf("cannot create fake java: %v", err)
	}

	cmd := exec.Command(javaPath, "60")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start fake java: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	return cmd.Process.Pid
}
