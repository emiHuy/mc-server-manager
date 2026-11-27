package pidfile

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWriteRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	want := Info{PID: 4242, StartedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}

	require.NoError(t, Write(path, want))

	got, err := Read(path)
	require.NoError(t, err)

	require.Equal(t, want.PID, got.PID)
	require.Equal(t, want.StartedAt, got.StartedAt)
}

func TestWriteReplacesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")

	require.NoError(t, Write(path, Info{PID: 1, StartedAt: time.Now()}))
	require.NoError(t, Write(path, Info{PID: 2, StartedAt: time.Now()}))

	got, err := Read(path)
	require.NoError(t, err)

	require.Equal(t, 2, got.PID)
}

func TestReadMissing(t *testing.T) {
	_, err := Read(filepath.Join(t.TempDir(), "missing.pid"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestReadInvalid(t *testing.T) {
	tests := []struct {
		name     string
		contents string
	}{
		{"empty file", ""},
		{"not json", "not json"},
		{"zero pid", `{"pid":0}`},
		{"negative pid", `{"pid":-5}`},
		{"missing pid", `{"started_at":"2026-01-01T00:00:00Z"}`},
		{"pid as string", `{"pid":"abc"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.pid")
			require.NoError(t, os.WriteFile(path, []byte(tt.contents), 0o644))

			info, err := Read(path)
			require.Error(t, err, fmt.Sprintf("Read() = %+v, nil, want error", info))

			require.NotErrorIs(t, err, os.ErrNotExist, fmt.Sprintf("Read() error = %v, want a parse error, not ErrNotExist", err))
		})
	}
}

func TestRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	require.NoError(t, Write(path, Info{PID: 1, StartedAt: time.Now()}))

	require.NoError(t, Remove(path))

	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRemoveMissing(t *testing.T) {
	require.NoError(t, Remove(filepath.Join(t.TempDir(), "missing.pid")))
}

func TestWriteBadDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "test.pid")
	require.Error(t, Write(path, Info{PID: 1, StartedAt: time.Now()}))
}
