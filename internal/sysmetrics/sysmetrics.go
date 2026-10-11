package sysmetrics

import (
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/process"
)

type Sampler struct { // not safe for concurrent use
	proc *process.Process
}

func NewSampler(pid int32) (*Sampler, error) {
	proc, err := process.NewProcess(pid)
	if err != nil {
		return nil, fmt.Errorf("cannot find process %d: %w", pid, err)
	}
	return &Sampler{proc: proc}, nil
}

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

type Disk struct {
	Used  uint64
	Free  uint64
	Total uint64
}

func DiskUsage(path string) (Disk, error) {
	usage, err := disk.Usage(path)
	if err != nil {
		return Disk{}, fmt.Errorf("cannot read disk usage for %s: %w", path, err)
	}
	return Disk{Used: usage.Used, Free: usage.Free, Total: usage.Total}, nil
}
