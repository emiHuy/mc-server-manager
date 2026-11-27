package sysmetrics

import (
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSamplerOwnProcess(t *testing.T) {
	sampler, err := NewSampler(int32(os.Getpid()))
	require.NoError(t, err)

	cpu, memory, err := sampler.Sample(100 * time.Millisecond)
	require.NoError(t, err)

	require.NotEqual(t, 0, memory)
	require.GreaterOrEqual(t, cpu, 0.0)
}

func TestSamplerMissingProcess(t *testing.T) {
	// A pid this large should not exist.
	// Depending on the platform, the error shows up either when creating the sampler or when sampling, so accept both.
	const missingPID = 1 << 30

	sampler, err := NewSampler(missingPID)
	if err != nil {
		return
	}

	_, _, err = sampler.Sample(10 * time.Millisecond)
	require.Error(t, err)
}

// TestSampleScale spins one goroutine while sampling this process.
func TestSampleScale(t *testing.T) {
	sampler, err := NewSampler(int32(os.Getpid()))
	require.NoError(t, err)

	var stop atomic.Bool
	go func() {
		for !stop.Load() {
		}
	}()

	cpu, _, err := sampler.Sample(500 * time.Millisecond)
	stop.Store(true)
	require.NoError(t, err)

	t.Logf("cores=%d cpu=%.1f", runtime.NumCPU(), cpu)

	require.Greater(t, cpu, 50.0)
	require.Less(t, cpu, 150.0)
}

func TestDiskUsage(t *testing.T) {
	usage, err := DiskUsage(t.TempDir())
	require.NoError(t, err)

	require.NotEqual(t, usage.Total, 0)
	require.LessOrEqual(t, usage.Used, usage.Total)
	require.LessOrEqual(t, usage.Free, usage.Total)
}

func TestDiskUsageMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does", "not", "exist")

	_, err := DiskUsage(missing)
	require.Error(t, err)
}
