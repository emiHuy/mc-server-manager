package minecraft

import (
	"log/slog"
	"sync"
	"time"

	"github.com/emiHuy/mc-server-manager/internal/fanout"
	"github.com/emiHuy/mc-server-manager/internal/sysmetrics"
)

const (
	metricsSampleWindow       = 250 * time.Millisecond
	defaultMetricsInterval    = 3 * time.Second
	metricsSubscriberCapacity = 4
)

type Metrics struct {
	State         State
	Uptime        time.Duration
	PlayersOnline int

	CPUPercent        float64 // per core
	CPUMachinePercent float64
	CPUCores          int

	MemoryBytes uint64

	DiskUsedBytes  uint64
	DiskFreeBytes  uint64
	DiskTotalBytes uint64
}

type sampleFunc func(pid int32) (cpu float64, mem uint64, err error)

func sampleOnce(pid int32) (float64, uint64, error) {
	sampler, err := sysmetrics.NewSampler(pid)
	if err != nil {
		return 0, 0, err
	}

	return sampler.Sample(metricsSampleWindow)
}

type metricsSampler struct {
	hub      *fanout.Hub[Metrics]
	interval time.Duration
	collect  func(sample sampleFunc) (Metrics, error)

	mu      sync.Mutex
	running bool
}

func newMetricsSampler(interval time.Duration, collect func(sample sampleFunc) (Metrics, error)) *metricsSampler {
	return &metricsSampler{
		hub:      fanout.New[Metrics]("metrics", metricsSubscriberCapacity, 1),
		interval: interval,
		collect:  collect,
	}
}

func (ms *metricsSampler) subscribe() ([]Metrics, *fanout.Subscription[Metrics]) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	wasRunning := ms.running
	latest, sub := ms.hub.Subscribe(1)

	if !wasRunning {
		latest = nil
		ms.running = true
		go ms.loop()
	}

	return latest, sub
}

// sampleLoop publishes a reading every metricsInterval until nobody is subscribed.
func (ms *metricsSampler) loop() {
	var sampler *sysmetrics.Sampler
	var samplerPID int32

	// A new sampler measures over a short window for its first reading.
	// After that it compares against the previous call, so it need not wait.
	sample := func(pid int32) (float64, uint64, error) {
		var window time.Duration
		if sampler == nil || samplerPID != pid {
			s, err := sysmetrics.NewSampler(pid)
			if err != nil {
				return 0, 0, err
			}
			sampler, samplerPID = s, pid
			window = metricsSampleWindow
		}
		return sampler.Sample(window)
	}

	ticker := time.NewTicker(ms.interval)
	defer ticker.Stop()

	for {
		m, err := ms.collect(sample)
		if err != nil {
			slog.Debug("minecraft: metrics sample incomplete", "error", err)
		}
		ms.hub.Publish(m)

		<-ticker.C

		ms.mu.Lock()
		if ms.hub.Len() == 0 {
			ms.running = false
			ms.mu.Unlock()
			return
		}
		ms.mu.Unlock()
	}
}
