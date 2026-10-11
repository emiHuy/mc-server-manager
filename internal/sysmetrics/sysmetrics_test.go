package sysmetrics

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSamplerOwnProcess(t *testing.T) {
	sampler, err := NewSampler(int32(os.Getpid()))
	if err != nil {
		t.Fatalf("NewSampler(own pid) error: %v", err)
	}

	cpu, memory, err := sampler.Sample(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("Sample() error: %v", err)
	}

	if memory == 0 {
		t.Errorf("memory = 0, want a positive number of bytes")
	}
	if cpu < 0 {
		t.Errorf("cpu = %v, want 0 or more", cpu)
	}
}

func TestSamplerMissingProcess(t *testing.T) {
	// A pid this large should not exist. Depending on the platform, the error
	// shows up either when creating the sampler or when sampling, so accept both.
	const missingPID = 1 << 30

	sampler, err := NewSampler(missingPID)
	if err != nil {
		return
	}

	if _, _, err := sampler.Sample(10 * time.Millisecond); err == nil {
		t.Errorf("expected an error for nonexistent pid %d, got none", missingPID)
	}
}

// TestSampleScale spins one goroutine while sampling this process. One busy
// goroutine saturates one core, so a per-core reading should be near 100
// whatever the core count. It only logs the number, because timing varies.
func TestSampleScale(t *testing.T) {
	sampler, err := NewSampler(int32(os.Getpid()))
	if err != nil {
		t.Fatalf("NewSampler(own pid) error: %v", err)
	}

	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
		}
	}()

	cpu, _, err := sampler.Sample(500 * time.Millisecond)
	close(stop)
	if err != nil {
		t.Fatalf("Sample() error: %v", err)
	}

	t.Logf("cores=%d cpu=%.1f (expect roughly 100 if the reading is per-core)", runtime.NumCPU(), cpu)

	if cpu <= 0 {
		t.Errorf("cpu = %v while a goroutine was spinning, want above 0", cpu)
	}
}

func TestDiskUsage(t *testing.T) {
	usage, err := DiskUsage(t.TempDir())
	if err != nil {
		t.Fatalf("DiskUsage(temp dir) error: %v", err)
	}

	if usage.Total == 0 {
		t.Errorf("Total = 0, want a positive number of bytes")
	}
	if usage.Used > usage.Total {
		t.Errorf("Used = %d, want at most Total = %d", usage.Used, usage.Total)
	}
	if usage.Free > usage.Total {
		t.Errorf("Free = %d, want at most Total = %d", usage.Free, usage.Total)
	}
}

func TestDiskUsageMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does", "not", "exist")

	if _, err := DiskUsage(missing); err == nil {
		t.Errorf("DiskUsage(%q) returned no error, want one", missing)
	}
}
