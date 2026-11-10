// Package pidfile reads and writes a small file recording a running process.
package pidfile

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"time"
)

// Info describes the process a pidfile points at.
type Info struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// Write saves info to path, replacing any existing file.
func Write(path string, info Info) error {
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("cannot encode pidfile: %w", err)
	}
	err = os.WriteFile(path, data, 0o644)
	if err != nil {
		return fmt.Errorf("cannot write pidfile: %w", err)
	}
	return nil
}

// Read loads the pidfile at path. A missing file returns an error that
// satisfies errors.Is(err, os.ErrNotExist).
func Read(path string) (Info, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Info{}, err
	}
	var info Info
	err = json.Unmarshal(data, &info)
	if err != nil {
		return Info{}, fmt.Errorf("cannot parse pidfile %s: %w", path, err)
	}
	if info.PID <= 0 {
		return Info{}, fmt.Errorf("invalid pid %d in pidfile %s", info.PID, path)
	}
	return info, nil
}

// Remove deletes the pidfile. A missing file is not an error.
func Remove(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
