package pidfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	want := Info{PID: 4242, StartedAt: time.Now()}

	if err := Write(path, want); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if got.PID != want.PID {
		t.Errorf("PID = %d, want %d", got.PID, want.PID)
	}
	if !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("StartedAt = %v, want %v", got.StartedAt, want.StartedAt)
	}
}

func TestWriteReplacesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")

	if err := Write(path, Info{PID: 1, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, Info{PID: 2, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if got.PID != 2 {
		t.Errorf("PID = %d, want 2", got.PID)
	}
}

func TestReadMissing(t *testing.T) {
	_, err := Read(filepath.Join(t.TempDir(), "missing.pid"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Read() error = %v, want ErrNotExist", err)
	}
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
			if err := os.WriteFile(path, []byte(tt.contents), 0o644); err != nil {
				t.Fatal(err)
			}

			info, err := Read(path)
			if err == nil {
				t.Fatalf("Read() = %+v, nil, want error", info)
			}
			if errors.Is(err, os.ErrNotExist) {
				t.Errorf("Read() error = %v, want a parse error, not ErrNotExist", err)
			}
		})
	}
}

func TestRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	if err := Write(path, Info{PID: 1, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	if err := Remove(path); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still exists after Remove(): stat error = %v", err)
	}
}

func TestRemoveMissing(t *testing.T) {
	if err := Remove(filepath.Join(t.TempDir(), "missing.pid")); err != nil {
		t.Errorf("Remove() on a missing file = %v, want nil", err)
	}
}

func TestWriteBadDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "test.pid")
	if err := Write(path, Info{PID: 1, StartedAt: time.Now()}); err == nil {
		t.Error("Write() into a missing directory = nil, want error")
	}
}
