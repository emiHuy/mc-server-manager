package minecraft

import (
	"sync"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/internal/fanout"
)

// These tests cover the live metrics sampler. They reuse the fake server and
// helpers from lifecycle_test.go.

const fastMetricsInterval = 20 * time.Millisecond

// withFastMetrics shortens the sampler's tick. Call it once, right after
// creating the instance and before the first subscription: the loop reads the
// interval when it starts.
func withFastMetrics(inst *Instance) *Instance {
	inst.metricsInterval = fastMetricsInterval
	return inst
}

// subscribeMetrics subscribes and cancels automatically when the test ends.
func subscribeMetrics(t *testing.T, inst *Instance) ([]Metrics, *fanout.Subscription[Metrics]) {
	t.Helper()

	snapshot, sub := inst.SubscribeMetrics()
	t.Cleanup(sub.Cancel)
	return snapshot, sub
}

func anyMetrics(Metrics) bool { return true }

// recvMetricsUntil reads readings until one satisfies match.
func recvMetricsUntil(t *testing.T, ch <-chan Metrics, match func(Metrics) bool) Metrics {
	t.Helper()

	deadline := time.After(lifecycleTimeout)
	for {
		select {
		case m, ok := <-ch:
			if !ok {
				t.Fatal("metrics channel closed while waiting for a reading")
			}
			if match(m) {
				return m
			}
		case <-deadline:
			t.Fatal("timed out waiting for a matching metrics reading")
		}
	}
}

// drainMetrics discards every reading currently queued in ch.
func drainMetrics(ch <-chan Metrics) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func isSamplerRunning(inst *Instance) bool {
	inst.samplerMu.Lock()
	defer inst.samplerMu.Unlock()
	return inst.samplerRunning
}

// waitForSamplerStop polls until the sampler loop has exited.
func waitForSamplerStop(t *testing.T, inst *Instance) {
	t.Helper()

	deadline := time.After(lifecycleTimeout)
	for isSamplerRunning(inst) {
		select {
		case <-deadline:
			t.Fatal("sampler loop did not stop after the last subscriber left")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestLiveMetricsReadingArrives(t *testing.T) {
	inst, events := startFake(t, modeNormal, 5*time.Second)
	withFastMetrics(inst)
	waitForState(t, events, StateRunning)

	_, sub := subscribeMetrics(t, inst)

	m := recvMetricsUntil(t, sub.C, func(m Metrics) bool {
		return m.State == StateRunning && m.MemoryBytes > 0
	})

	if m.CPUCores <= 0 {
		t.Errorf("CPUCores = %d, want a positive number", m.CPUCores)
	}
	if m.Uptime < 0 {
		t.Errorf("Uptime = %v, want 0 or more", m.Uptime)
	}
}

func TestLiveMetricsStoppedServer(t *testing.T) {
	inst := withFastMetrics(newFakeInstance(t, modeNormal, 5*time.Second))

	_, sub := subscribeMetrics(t, inst)

	m := recvMetricsUntil(t, sub.C, anyMetrics)

	if m.State != StateStopped {
		t.Errorf("State = %v, want %v", m.State, StateStopped)
	}
	if m.MemoryBytes != 0 || m.CPUPercent != 0 || m.Uptime != 0 {
		t.Errorf("process fields = memory %d, cpu %v, uptime %v, want all zero",
			m.MemoryBytes, m.CPUPercent, m.Uptime)
	}
	if m.DiskTotalBytes == 0 {
		t.Error("DiskTotalBytes = 0, want disk usage reported even when stopped")
	}
}

func TestLiveMetricsLoopStopsAfterLastCancel(t *testing.T) {
	inst := withFastMetrics(newFakeInstance(t, modeNormal, 5*time.Second))

	_, sub := subscribeMetrics(t, inst)
	recvMetricsUntil(t, sub.C, anyMetrics)

	if !isSamplerRunning(inst) {
		t.Fatal("sampler is not running while a subscriber exists")
	}

	sub.Cancel()

	waitForSamplerStop(t, inst)
}

func TestLiveMetricsTwoSubscribersShareOneLoop(t *testing.T) {
	inst := withFastMetrics(newFakeInstance(t, modeNormal, 5*time.Second))

	_, first := subscribeMetrics(t, inst)
	_, second := subscribeMetrics(t, inst)

	recvMetricsUntil(t, first.C, anyMetrics)
	recvMetricsUntil(t, second.C, anyMetrics)

	if !isSamplerRunning(inst) {
		t.Fatal("sampler is not running with two subscribers")
	}

	// Cancelling one subscriber must not stop the loop for the other.
	first.Cancel()
	drainMetrics(second.C)
	recvMetricsUntil(t, second.C, anyMetrics)

	if !isSamplerRunning(inst) {
		t.Error("sampler stopped while a subscriber was still listening")
	}

	second.Cancel()
	waitForSamplerStop(t, inst)
}

func TestLiveMetricsSurviveRestart(t *testing.T) {
	inst, events := startFake(t, modeNormal, 5*time.Second)
	withFastMetrics(inst)
	waitForState(t, events, StateRunning)

	_, sub := subscribeMetrics(t, inst)

	recvMetricsUntil(t, sub.C, func(m Metrics) bool {
		return m.State == StateRunning && m.MemoryBytes > 0
	})

	err := inst.Stop()
	if err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	recvMetricsUntil(t, sub.C, func(m Metrics) bool {
		return m.State == StateStopped && m.MemoryBytes == 0
	})

	// The next run has a different process id, and readings must follow it.
	err = inst.Start()
	if err != nil {
		t.Fatalf("second Start() error: %v", err)
	}

	recvMetricsUntil(t, sub.C, func(m Metrics) bool {
		return m.State == StateRunning && m.MemoryBytes > 0
	})
}

func TestLiveMetricsNoStaleSnapshot(t *testing.T) {
	inst := withFastMetrics(newFakeInstance(t, modeNormal, 5*time.Second))

	snapshot, sub := subscribeMetrics(t, inst)
	if len(snapshot) != 0 {
		t.Errorf("first snapshot = %v, want empty", snapshot)
	}

	recvMetricsUntil(t, sub.C, anyMetrics)
	sub.Cancel()
	waitForSamplerStop(t, inst)

	// The hub still remembers the last reading, but it is from before nobody
	// was watching, so it must not be handed out as current.
	snapshot, _ = subscribeMetrics(t, inst)
	if len(snapshot) != 0 {
		t.Errorf("snapshot after the sampler had stopped = %v, want empty", snapshot)
	}
}

func TestLiveMetricsLateSubscriberGetsLatest(t *testing.T) {
	inst := withFastMetrics(newFakeInstance(t, modeNormal, 5*time.Second))

	_, first := subscribeMetrics(t, inst)
	recvMetricsUntil(t, first.C, anyMetrics)

	snapshot, _ := subscribeMetrics(t, inst)
	if len(snapshot) != 1 {
		t.Errorf("snapshot while the sampler runs = %d readings, want 1", len(snapshot))
	}
}

// TestLiveMetricsConcurrentSubscribers is mainly useful with:
// go test -race ./minecraft/
func TestLiveMetricsConcurrentSubscribers(t *testing.T) {
	inst := withFastMetrics(newFakeInstance(t, modeNormal, 5*time.Second))

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				_, sub := inst.SubscribeMetrics()
				drainMetrics(sub.C)
				sub.Cancel()
				sub.Cancel()
			}
		}()
	}
	wg.Wait()

	// After all that churn, a new subscriber must still get readings. This
	// catches a subscriber arriving just as the loop decided to stop.
	_, sub := subscribeMetrics(t, inst)
	recvMetricsUntil(t, sub.C, anyMetrics)

	sub.Cancel()
	waitForSamplerStop(t, inst)
}
