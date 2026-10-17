package fanout

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

const testTimeout = time.Second

// recv reads one item from ch, failing the test if nothing arrives in time.
func recv[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case item, ok := <-ch:
		if !ok {
			t.Fatal("channel closed, expected an item")
		}
		return item
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for an item")
	}
	var zero T
	return zero
}

// drain returns every item currently queued in ch without blocking.
func drain[T any](ch <-chan T) []T {
	var items []T
	for {
		select {
		case item, ok := <-ch:
			if !ok {
				return items
			}
			items = append(items, item)
		default:
			return items
		}
	}
}

func TestHub(t *testing.T) {
	t.Run("receives published items in order", func(t *testing.T) {
		hub := New[string]("test", 8, 0)
		_, sub := hub.Subscribe(0)

		for _, item := range []string{"a", "b", "c"} {
			hub.Publish(item)
		}

		for _, want := range []string{"a", "b", "c"} {
			if got := recv(t, sub.C); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		}
	})

	t.Run("publish with no subscribers is safe", func(t *testing.T) {
		hub := New[string]("test", 8, 10)
		hub.Publish("a")

		if got := hub.Recent(10); !slices.Equal(got, []string{"a"}) {
			t.Errorf("Recent(10) = %v, want [a]", got)
		}
	})

	t.Run("multiple subscribers", func(t *testing.T) {
		hub := New[string]("test", 8, 0)
		_, first := hub.Subscribe(0)
		_, second := hub.Subscribe(0)

		hub.Publish("a")
		hub.Publish("b")

		for _, sub := range []*Subscription[string]{first, second} {
			for _, want := range []string{"a", "b"} {
				if got := recv(t, sub.C); got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		}
	})

	t.Run("slow subscriber does not block", func(t *testing.T) {
		hub := New[string]("test", 2, 0)
		_, stuck := hub.Subscribe(0) // never reads
		_, live := hub.Subscribe(0)

		finished := make(chan struct{})
		go func() {
			defer close(finished)
			for i := 0; i < 10; i++ {
				hub.Publish(fmt.Sprintf("item %d", i))
				<-live.C // keep the live subscriber caught up
			}
		}()

		select {
		case <-finished:
		case <-time.After(testTimeout):
			t.Fatal("Publish blocked because of a subscriber that never reads")
		}

		if got := stuck.Dropped(); got != 8 {
			t.Errorf("stuck.Dropped() = %d, want 8", got)
		}
		if got := live.Dropped(); got != 0 {
			t.Errorf("live.Dropped() = %d, want 0", got)
		}
	})

	t.Run("drops oldest when full", func(t *testing.T) {
		hub := New[string]("test", 2, 0)
		_, sub := hub.Subscribe(0)

		for _, item := range []string{"a", "b", "c", "d"} {
			hub.Publish(item)
		}

		if got := sub.Dropped(); got != 2 {
			t.Errorf("Dropped() = %d, want 2", got)
		}
		if want, got := []string{"c", "d"}, drain(sub.C); !slices.Equal(got, want) {
			t.Errorf("queued items = %v, want %v", got, want)
		}
	})

	t.Run("capacity below one is clamped", func(t *testing.T) {
		for _, capacity := range []int{0, -3} {
			hub := New[string]("test", capacity, 0)
			_, sub := hub.Subscribe(0)

			finished := make(chan struct{})
			go func() {
				defer close(finished)
				hub.Publish("a")
				hub.Publish("b")
			}()

			select {
			case <-finished:
			case <-time.After(testTimeout):
				t.Fatalf("Publish blocked with capacity %d", capacity)
			}

			if got := recv(t, sub.C); got != "b" {
				t.Errorf("capacity %d: got %q, want %q", capacity, got, "b")
			}
		}
	})
}

func TestHubHistory(t *testing.T) {
	t.Run("snapshot then live", func(t *testing.T) {
		hub := New[string]("test", 8, 10)
		for _, item := range []string{"a", "b", "c"} {
			hub.Publish(item)
		}

		snapshot, sub := hub.Subscribe(2)
		if want := []string{"b", "c"}; !slices.Equal(snapshot, want) {
			t.Errorf("snapshot = %v, want %v", snapshot, want)
		}

		hub.Publish("d")
		if got := recv(t, sub.C); got != "d" {
			t.Errorf("first live item = %q, want %q", got, "d")
		}
		if extra := drain(sub.C); len(extra) != 0 {
			t.Errorf("unexpected extra items on channel: %v", extra)
		}
	})

	t.Run("non-positive snapshot is empty", func(t *testing.T) {
		for _, n := range []int{0, -1} {
			hub := New[string]("test", 8, 10)
			hub.Publish("a")

			snapshot, sub := hub.Subscribe(n)
			if len(snapshot) != 0 {
				t.Errorf("Subscribe(%d) snapshot = %v, want empty", n, snapshot)
			}

			hub.Publish("b")
			if got := recv(t, sub.C); got != "b" {
				t.Errorf("Subscribe(%d) live item = %q, want %q", n, got, "b")
			}
		}
	})

	t.Run("history keeps only the most recent items", func(t *testing.T) {
		hub := New[string]("test", 8, 3)
		for _, item := range []string{"a", "b", "c", "d", "e"} {
			hub.Publish(item)
		}

		if want, got := []string{"c", "d", "e"}, hub.Recent(10); !slices.Equal(got, want) {
			t.Errorf("Recent(10) = %v, want %v", got, want)
		}
	})

	t.Run("history keeps every item even when a subscriber drops", func(t *testing.T) {
		hub := New[string]("test", 1, 10)
		_, sub := hub.Subscribe(0)

		for _, item := range []string{"a", "b", "c"} {
			hub.Publish(item)
		}

		if got := sub.Dropped(); got != 2 {
			t.Errorf("Dropped() = %d, want 2", got)
		}
		if want, got := []string{"a", "b", "c"}, hub.Recent(10); !slices.Equal(got, want) {
			t.Errorf("Recent(10) = %v, want %v", got, want)
		}
	})

	t.Run("zero history keeps nothing", func(t *testing.T) {
		hub := New[string]("test", 8, 0)
		hub.Publish("before")

		snapshot, sub := hub.Subscribe(5)
		if len(snapshot) != 0 {
			t.Errorf("snapshot = %v, want empty", snapshot)
		}
		if got := hub.Recent(5); len(got) != 0 {
			t.Errorf("Recent(5) = %v, want empty", got)
		}

		hub.Publish("after")
		if got := recv(t, sub.C); got != "after" {
			t.Errorf("live item = %q, want %q", got, "after")
		}
	})
}

func TestSubscriptionCancel(t *testing.T) {
	t.Run("cancel stops delivery and closes the channel", func(t *testing.T) {
		hub := New[string]("test", 8, 0)
		_, sub := hub.Subscribe(0)

		sub.Cancel()
		hub.Publish("a") // must not panic or block

		select {
		case item, ok := <-sub.C:
			if ok {
				t.Errorf("received %q after Cancel, want closed channel", item)
			}
		case <-time.After(testTimeout):
			t.Fatal("channel was not closed by Cancel")
		}
	})

	t.Run("queued items stay readable after cancel", func(t *testing.T) {
		hub := New[string]("test", 8, 0)
		_, sub := hub.Subscribe(0)

		hub.Publish("a")
		sub.Cancel()

		if got := recv(t, sub.C); got != "a" {
			t.Errorf("got %q, want %q", got, "a")
		}

		select {
		case _, ok := <-sub.C:
			if ok {
				t.Error("channel still open after queued items were read")
			}
		case <-time.After(testTimeout):
			t.Fatal("channel was not closed")
		}
	})

	t.Run("cancel twice is safe", func(t *testing.T) {
		hub := New[string]("test", 8, 0)
		_, sub := hub.Subscribe(0)

		sub.Cancel()
		sub.Cancel()
	})

	t.Run("cancel leaves other subscribers working", func(t *testing.T) {
		hub := New[string]("test", 8, 0)
		_, first := hub.Subscribe(0)
		_, second := hub.Subscribe(0)

		first.Cancel()
		hub.Publish("a")

		if got := recv(t, second.C); got != "a" {
			t.Errorf("got %q, want %q", got, "a")
		}
	})
}

// TestHubWithStructs checks the hub works for a non-string element type, the
// way Instance uses it for events.
func TestHubWithStructs(t *testing.T) {
	type event struct {
		kind string
		id   int
	}

	hub := New[event]("test", 8, 0)
	_, sub := hub.Subscribe(0)

	want := event{kind: "state_changed", id: 7}
	hub.Publish(want)

	if got := recv(t, sub.C); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestHubConcurrent is mainly useful with: go test -race ./internal/fanout/
func TestHubConcurrent(t *testing.T) {
	hub := New[string]("test", 4, 50)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			hub.Publish(fmt.Sprintf("item %d", i))
		}
	}()

	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, sub := hub.Subscribe(5)
				drain(sub.C)
				_ = sub.Dropped()
				_ = hub.Recent(5)
				sub.Cancel()
				sub.Cancel()
			}
		}()
	}

	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent hub operations did not finish (possible deadlock)")
	}
}

func TestHubLen(t *testing.T) {
	hub := New[string]("test", 8, 0)

	if got := hub.Len(); got != 0 {
		t.Fatalf("Len() with no subscribers = %d, want 0", got)
	}

	_, first := hub.Subscribe(0)
	_, second := hub.Subscribe(0)

	if got := hub.Len(); got != 2 {
		t.Errorf("Len() after two subscribes = %d, want 2", got)
	}

	first.Cancel()
	if got := hub.Len(); got != 1 {
		t.Errorf("Len() after one cancel = %d, want 1", got)
	}

	// A second cancel of the same subscription changes nothing.
	first.Cancel()
	if got := hub.Len(); got != 1 {
		t.Errorf("Len() after cancelling twice = %d, want 1", got)
	}

	second.Cancel()
	if got := hub.Len(); got != 0 {
		t.Errorf("Len() after every cancel = %d, want 0", got)
	}
}
