package backup

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func writeZip(dst string, sourceDir string, foldersToSave []string) error {
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	err = zipWorld(f, sourceDir, foldersToSave)
	if err == nil {
		err = f.Sync() // flush to disk before the rename makes it "real"
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func zipWorld(w io.Writer, sourceDir string, foldersToSave []string) error {
	zw := zip.NewWriter(w)

	for _, folder := range foldersToSave {
		root := filepath.Join(sourceDir, folder)

		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err // includes the world folder not existing
			}
			if d.IsDir() || !d.Type().IsRegular() || d.Name() == "session.lock" {
				return nil
			}
			rel, err := filepath.Rel(sourceDir, path) // keeps "world/" as the top folder
			if err != nil {
				return err
			}
			return addFile(zw, path, filepath.ToSlash(rel))
		})
		if err != nil {
			return err
		}
	}

	return zw.Close()
}

func addFile(zw *zip.Writer, path, name string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = name
	hdr.Method = zip.Deflate

	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, src)
	return err
}
