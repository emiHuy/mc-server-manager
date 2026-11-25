package backup

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
}

func TestWriteZip(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{
		"world/level.dat":        "level",
		"world/region/r.0.0.mca": "region",
		"world/session.lock":     "lock",
		"other/not-in-world.txt": "ignored",
	})

	dst := filepath.Join(t.TempDir(), "out.zip")
	require.NoError(t, writeZip(dst, src, "world"))

	zr, err := zip.OpenReader(dst)
	require.NoError(t, err)
	defer zr.Close()

	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)

		b, err := io.ReadAll(rc)
		rc.Close()
		require.NoError(t, err)

		got[f.Name] = string(b)
	}

	want := map[string]string{
		"world/level.dat":        "level",
		"world/region/r.0.0.mca": "region",
	}
	require.Equal(t, want, got)
}

func TestWriteZipErrors(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string]string{"world/level.dat": "x"})

	t.Run("destination exists", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "out.zip")
		require.NoError(t, os.WriteFile(dst, nil, 0o644))

		require.Error(t, writeZip(dst, src, "world"))
	})

	t.Run("missing world", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "out.zip")
		require.Error(t, writeZip(dst, src, "nope"))
	})
}
