// Package proc looks up information about running processes.
package proc

import (
	"errors"
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// ErrNotFound means no process with that PID is running.
var ErrNotFound = errors.New("process not found")

// Info describes a running process.
type Info struct {
	Name      string
	StartedAt time.Time
}

// Lookup returns the name and start time of the process with the given PID.
// It returns ErrNotFound if the process is not running.
func Lookup(pid int) (Info, error) {
	exists, err := process.PidExists(int32(pid))
	if err != nil {
		return Info{}, fmt.Errorf("cannot check pid %d: %w", pid, err)
	}
	if !exists {
		return Info{}, ErrNotFound
	}

	p, err := process.NewProcess(int32(pid))
	if err != nil {
		if errors.Is(err, process.ErrorProcessNotRunning) {
			return Info{}, ErrNotFound
		}
		return Info{}, fmt.Errorf("cannot open pid %d: %w", pid, err)
	}
	name, err := p.Name()
	if err != nil {
		return Info{}, fmt.Errorf("cannot read name of pid %d: %w", pid, err)
	}
	created, err := p.CreateTime() // milliseconds since the Unix epoch
	if err != nil {
		return Info{}, fmt.Errorf("cannot read start time of pid %d: %w", pid, err)
	}
	return Info{Name: name, StartedAt: time.UnixMilli(created)}, nil
}
