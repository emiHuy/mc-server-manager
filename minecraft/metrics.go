package minecraft

import (
	"fmt"
	"runtime"
	"time"

	"github.com/emiHuy/mc-server-manager/internal/sysmetrics"
)

const metricsSampleWindow = 250 * time.Millisecond

type Metrics struct {
	State         State
	Uptime        time.Duration
	PlayersOnline int

	CPUPercent        float64
	CPUMachinePercent float64
	CPUCores          int

	MemoryBytes uint64

	DiskUsedBytes  uint64
	DiskFreeBytes  uint64
	DiskTotalBytes uint64
}

func (inst *Instance) Metrics() (Metrics, error) {
	inst.mu.Lock()
	state := inst.state
	startedAt := inst.startedAt

	var pid int32
	hasProcess := false
	if inst.cmd != nil && inst.cmd.Process != nil {
		pid = int32(inst.cmd.Process.Pid)
		hasProcess = true
	}
	inst.mu.Unlock()

	m := Metrics{
		State:         state,
		PlayersOnline: len(inst.Players()),
		CPUCores:      runtime.NumCPU(),
	}

	if !startedAt.IsZero() {
		m.Uptime = time.Since(startedAt)
	}

	if hasProcess {
		sampler, err := sysmetrics.NewSampler(pid)
		if err == nil {
			cpu, mem, err := sampler.Sample(metricsSampleWindow)
			if err == nil {
				m.CPUPercent = cpu
				m.CPUMachinePercent = min(cpu/float64(m.CPUCores), 100)
				m.MemoryBytes = mem
			}
		}
	}

	usage, err := sysmetrics.DiskUsage(inst.conf.Directory)
	if err != nil {
		return m, fmt.Errorf("cannot read disk usage: %w", err)
	}
	m.DiskUsedBytes = usage.Used
	m.DiskFreeBytes = usage.Free
	m.DiskTotalBytes = usage.Total

	return m, nil
}
