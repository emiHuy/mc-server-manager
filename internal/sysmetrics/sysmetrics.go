// Package sysmetrics provides utilities for sampling process CPU and memory usage and retrieving disk usage statistics.
package sysmetrics

import (
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/process"
)

// Sampler samples CPU and memory usage for a specific process.
//
// Sampler is not safe for concurrent use.
type Sampler struct {
	proc *process.Process
}

func NewSampler(pid int32) (*Sampler, error) {
	proc, err := process.NewProcess(pid)
	if err != nil {
		return nil, fmt.Errorf("cannot find process %d: %w", pid, err)
	}
	return &Sampler{proc: proc}, nil
}

// Sample measures the process's CPU usage over the specified window and retrieves its resident memory usage in bytes.
//
// The CPU measurement may block for the duration of window.
func (s *Sampler) Sample(window time.Duration) (cpuPercent float64, memoryBytes uint64, err error) {
	cpuPercent, err = s.proc.Percent(window)
	if err != nil {
		return 0, 0, fmt.Errorf("cannot read cpu usage: %w", err)
	}

	mem, err := s.proc.MemoryInfo()
	if err != nil {
		return 0, 0, fmt.Errorf("cannot read memory usage: %w", err)
	}

	return cpuPercent, mem.RSS, nil
}

// Disk represents disk usage statistics for a filesystem (in bytes).
type Disk struct {
	Used  uint64
	Free  uint64
	Total uint64
}

// DiskUsage retrieves disk usage statistics for the filesystem containing path.
func DiskUsage(path string) (Disk, error) {
	usage, err := disk.Usage(path)
	if err != nil {
		return Disk{}, fmt.Errorf("cannot read disk usage for %s: %w", path, err)
	}
	return Disk{Used: usage.Used, Free: usage.Free, Total: usage.Total}, nil
}
