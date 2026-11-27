package minecraft

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testSamplerInterval = 5 * time.Millisecond

func newTestSampler(collect func(sampleFunc) (Metrics, error)) *metricsSampler {
	return newMetricsSampler(testSamplerInterval, collect)
}

func samplerRunning(s *metricsSampler) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestFirstSubscribeStartsLoop(t *testing.T) {
	var calls atomic.Int32
	s := newTestSampler(func(sampleFunc) (Metrics, error) {
		calls.Add(1)
		return Metrics{}, nil
	})

	_, sub := s.subscribe()
	defer sub.Cancel()

	waitUntil(t, "first collect call", func() bool { return calls.Load() >= 1 })
	require.True(t, samplerRunning(s))
}

func TestFirstSubscribeDropsStaleHistory(t *testing.T) {
	s := newTestSampler(func(sampleFunc) (Metrics, error) { return Metrics{}, nil })

	s.hub.Publish(Metrics{PlayersOnline: 7})

	latest, sub := s.subscribe()
	defer sub.Cancel()

	require.Len(t, latest, 0)
}

func TestStopWhenLastSubscriberLeaves(t *testing.T) {
	var calls atomic.Int32
	s := newTestSampler(func(sampleFunc) (Metrics, error) {
		calls.Add(1)
		return Metrics{}, nil
	})

	_, sub := s.subscribe()
	waitUntil(t, "first collect call", func() bool { return calls.Load() >= 1 })

	sub.Cancel()
	waitUntil(t, "loop to stop", func() bool { return !samplerRunning(s) })

	stopped := calls.Load()
	time.Sleep(10 * testSamplerInterval)
	require.Equal(t, stopped, calls.Load())
}

func TestRestartAfterStopping(t *testing.T) {
	var calls atomic.Int32
	s := newTestSampler(func(sampleFunc) (Metrics, error) {
		calls.Add(1)
		return Metrics{}, nil
	})

	_, first := s.subscribe()
	waitUntil(t, "first collect call", func() bool { return calls.Load() >= 1 })
	first.Cancel()
	waitUntil(t, "loop to stop", func() bool { return !samplerRunning(s) })

	before := calls.Load()
	_, second := s.subscribe()
	defer second.Cancel()

	waitUntil(t, "loop to restart", func() bool { return calls.Load() > before })
}

func TestOneLoopForManySubscribers(t *testing.T) {
	var inflight, maxInflight atomic.Int32
	s := newTestSampler(func(sampleFunc) (Metrics, error) {
		n := inflight.Add(1)
		for {
			prev := maxInflight.Load()
			if n <= prev || maxInflight.CompareAndSwap(prev, n) {
				break
			}
		}
		time.Sleep(2 * testSamplerInterval)
		inflight.Add(-1)
		return Metrics{}, nil
	})

	_, a := s.subscribe()
	defer a.Cancel()
	_, b := s.subscribe()
	defer b.Cancel()

	time.Sleep(20 * testSamplerInterval)

	require.Equal(t, int32(1), maxInflight.Load())
}

func TestPublishPartialReadingOnError(t *testing.T) {
	s := newTestSampler(func(sampleFunc) (Metrics, error) {
		return Metrics{PlayersOnline: 3}, errors.New("disk unavailable")
	})

	_, sub := s.subscribe()
	defer sub.Cancel()

	select {
	case m := <-sub.C:
		require.Equal(t, 3, m.PlayersOnline)
	case <-time.After(2 * time.Second):
		t.Fatal("no reading was published after collect returned an error")
	}
}

func TestConcurrentSubscribeCancel(t *testing.T) {
	s := newTestSampler(func(sampleFunc) (Metrics, error) { return Metrics{}, nil })

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				_, sub := s.subscribe()
				time.Sleep(time.Millisecond)
				sub.Cancel()
			}
		}()
	}
	wg.Wait()

	waitUntil(t, "loop to stop", func() bool { return !samplerRunning(s) })
}
