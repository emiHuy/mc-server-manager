package backup

import (
	"archive/zip"
	"bytes"
	"io"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeSource builds a temp "server directory". Keys are slash-separated paths
// relative to it, e.g. "world/level.dat".
func makeSource(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// readZip returns every entry of a zip as name -> content.
func readZip(t *testing.T, data []byte) map[string]string {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}

	out := make(map[string]string)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = string(b)
	}
	return out
}

func dirEntryNames(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestZipWorldContents(t *testing.T) {
	src := makeSource(t, map[string]string{
		"world/level.dat":        "level",
		"world/region/r.0.0.mca": "region",
		"world/session.lock":     "lock",  // must be skipped
		"server.properties":      "props", // outside the world folder
		"world_nether/level.dat": "nether",
	})

	var buf bytes.Buffer
	if err := zipWorld(&buf, src, []string{"world"}); err != nil {
		t.Fatalf("zipWorld: %v", err)
	}

	want := map[string]string{
		"world/level.dat":        "level",
		"world/region/r.0.0.mca": "region",
	}
	got := readZip(t, buf.Bytes())
	if !maps.Equal(got, want) {
		t.Fatalf("zip entries = %v, want %v", got, want)
	}
}

func TestZipWorldMissingWorld(t *testing.T) {
	src := makeSource(t, map[string]string{"server.properties": "props"})

	var buf bytes.Buffer
	if err := zipWorld(&buf, src, []string{"world"}); err == nil {
		t.Fatal("expected an error when the world folder does not exist")
	}
}

func TestZipWorldSkipsSymlinks(t *testing.T) {
	src := makeSource(t, map[string]string{"world/level.dat": "level"})
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(src, "world", "link.dat")); err != nil {
		t.Skipf("symlinks not available here: %v", err)
	}

	var buf bytes.Buffer
	if err := zipWorld(&buf, src, []string{"world"}); err != nil {
		t.Fatalf("zipWorld: %v", err)
	}

	got := readZip(t, buf.Bytes())
	if _, ok := got["world/link.dat"]; ok {
		t.Fatal("symlink was followed into the archive")
	}
	if len(got) != 1 {
		t.Fatalf("zip entries = %v, want only world/level.dat", got)
	}
}

func TestArchiveWritesZipAndLeavesNoTmp(t *testing.T) {
	src := makeSource(t, map[string]string{"world/level.dat": "data"})
	m := newCreateManager(t, nil, src, 3)
	const id = "backup-20261112-153000"

	info, err := m.archive(id)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}

	if info.ID != id {
		t.Errorf("ID = %q, want %q", info.ID, id)
	}
	if info.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want > 0", info.SizeBytes)
	}
	wantTime := time.Date(2026, time.November, 12, 15, 30, 0, 0, time.UTC)
	if !info.CreatedAt.Equal(wantTime) {
		t.Errorf("CreatedAt = %v, want %v", info.CreatedAt, wantTime)
	}

	data, err := os.ReadFile(m.path(id))
	if err != nil {
		t.Fatal(err)
	}
	if got := readZip(t, data); !maps.Equal(got, map[string]string{"world/level.dat": "data"}) {
		t.Errorf("zip entries = %v", got)
	}

	names := dirEntryNames(t, m.conf.Directory)
	if len(names) != 1 || names[0] != id+".zip" {
		t.Errorf("backup directory = %v, want only %s.zip", names, id)
	}
}

func TestArchiveRefusesToOverwrite(t *testing.T) {
	src := makeSource(t, map[string]string{"world/level.dat": "data"})
	m := newCreateManager(t, nil, src, 3)
	const id = "backup-20261112-153000"

	if err := os.MkdirAll(m.conf.Directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.path(id), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := m.archive(id); err == nil {
		t.Fatal("expected an error when the backup already exists")
	}

	got, err := os.ReadFile(m.path(id))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep" {
		t.Fatalf("existing backup was modified: %q", got)
	}
}

func TestArchiveCleansUpTmpOnFailure(t *testing.T) {
	src := makeSource(t, map[string]string{"server.properties": "props"}) // no world folder
	m := newCreateManager(t, nil, src, 3)

	if _, err := m.archive("backup-20261112-153000"); err == nil {
		t.Fatal("expected an error for a missing world folder")
	}

	if names := dirEntryNames(t, m.conf.Directory); len(names) != 0 {
		t.Fatalf("backup directory should be empty after a failed archive, got %v", names)
	}
}
