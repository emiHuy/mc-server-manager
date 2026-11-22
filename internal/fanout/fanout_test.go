package fanout

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func drain[T any](ch <-chan T) []T {
	var out []T
	for {
		select {
		case v, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, v)
		default:
			return out
		}
	}
}

func TestNewNormalizesCapacity(t *testing.T) {
	h := New[int]("test", 0, 0)
	require.Equal(t, 1, h.capacity)
	require.Nil(t, h.history)
}

func TestHubPublishFansOutToAllSubscribers(t *testing.T) {
	h := New[int]("test", 4, 0)
	_, a := h.Subscribe(0)
	_, b := h.Subscribe(0)
	defer a.Cancel()
	defer b.Cancel()

	h.Publish(1)
	h.Publish(2)

	want := []int{1, 2}

	require.Equal(t, want, drain(a.C))
	require.Equal(t, want, drain(b.C))
}

func TestHubPublishDropsOldestWhenFull(t *testing.T) {
	h := New[int]("test", 2, 0)
	_, sub := h.Subscribe(0)
	defer sub.Cancel()

	for i := 1; i <= 4; i++ {
		h.Publish(i) // must never block
	}

	require.Equal(t, []int{3, 4}, drain(sub.C))
	require.Equal(t, 2, sub.Dropped())
}

func TestHubSlowSubscriberDoesNotAffectOthers(t *testing.T) {
	h := New[int]("test", 1, 0)
	_, slow := h.Subscribe(0)
	_, fast := h.Subscribe(0)
	defer slow.Cancel()
	defer fast.Cancel()

	h.Publish(1)
	require.Equal(t, []int{1}, drain(fast.C))

	h.Publish(2)
	require.Equal(t, []int{2}, drain(fast.C))

	require.Equal(t, 0, fast.Dropped())
	require.Equal(t, 1, slow.Dropped())
}

func TestHubSubscribeReturnsHistorySnapshotNotOnChannel(t *testing.T) {
	h := New[int]("test", 4, 3)
	for i := 1; i <= 5; i++ {
		h.Publish(i)
	}

	snapshot, sub := h.Subscribe(2)
	defer sub.Cancel()

	require.Equal(t, []int{4, 5}, snapshot)
	require.Equal(t, 0, len(drain(sub.C)))

	h.Publish(6)
	require.Equal(t, []int{6}, drain(sub.C))
}

func TestHubRecentWithHistoryDisabled(t *testing.T) {
	h := New[int]("test", 1, 0)
	h.Publish(1)

	require.Equal(t, 0, len(h.Recent(5)))
}

func TestHubRecent(t *testing.T) {
	h := New[int]("test", 1, 3)
	for i := 1; i <= 4; i++ {
		h.Publish(i)
	}

	require.Equal(t, []int{2, 3, 4}, h.Recent(10))
}

func TestHub_Len(t *testing.T) {
	h := New[int]("test", 1, 0)
	require.Equal(t, 0, h.Len())

	_, a := h.Subscribe(0)
	_, b := h.Subscribe(0)
	require.Equal(t, 2, h.Len())

	a.Cancel()
	b.Cancel()

	require.Equal(t, 0, h.Len())
}

func TestHubPublishAfterCancelDoesNotPanic(t *testing.T) {
	h := New[int]("test", 1, 1)
	_, sub := h.Subscribe(0)
	sub.Cancel()

	h.Publish(1)
}

// Run with -race. Publishers, subscribers and cancels all interleave.
func TestHubConcurrentPublishSubscribeCancel(t *testing.T) {
	h := New[int]("test", 2, 8)

	var wg sync.WaitGroup
	for p := 0; p < 4; p++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				h.Publish(i)
			}
		}()
	}
	for s := 0; s < 4; s++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, sub := h.Subscribe(4)
				drain(sub.C)
				_ = sub.Dropped()
				sub.Cancel()
			}
		}()
	}
	wg.Wait()

	require.Equal(t, 0, h.Len())
}
