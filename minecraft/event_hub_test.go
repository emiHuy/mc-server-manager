package minecraft

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// testEvent builds an event whose Message identifies it in assertions.
func testEvent(id string) Event {
	return Event{Type: EventStateChanged, Message: id}
}

// recvEvent reads one event from ch, failing the test if nothing arrives in time.
// It relies on testTimeout from console_hub_test.go (same package).
func recvEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e, ok := <-ch:
		if !ok {
			t.Fatal("channel closed, expected an event")
		}
		return e
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for an event")
	}
	return Event{}
}

// drainEvents returns the Message of every event currently queued in ch
// without blocking.
func drainEvents(ch <-chan Event) []string {
	var ids []string
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return ids
			}
			ids = append(ids, e.Message)
		default:
			return ids
		}
	}
}

func TestEventHub(t *testing.T) {
	t.Run("receives published events in order", func(t *testing.T) {
		hub := newEventHub(8)
		sub := hub.subscribe()

		for _, id := range []string{"a", "b", "c"} {
			hub.publish(testEvent(id))
		}

		for _, want := range []string{"a", "b", "c"} {
			if got := recvEvent(t, sub.Events); got.Message != want {
				t.Errorf("got %q, want %q", got.Message, want)
			}
		}
	})

	t.Run("event fields are delivered intact", func(t *testing.T) {
		hub := newEventHub(8)
		sub := hub.subscribe()

		want := Event{
			Type:      EventStateChanged,
			Timestamp: time.Now(),
			State:     StateRunning,
			Message:   "m",
		}
		hub.publish(want)

		got := recvEvent(t, sub.Events)
		if got.Type != want.Type || got.State != want.State || got.Message != want.Message || !got.Timestamp.Equal(want.Timestamp) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("no history for late subscribers", func(t *testing.T) {
		hub := newEventHub(8)
		hub.publish(testEvent("before"))

		sub := hub.subscribe()
		hub.publish(testEvent("after"))

		if got := recvEvent(t, sub.Events); got.Message != "after" {
			t.Errorf("first event = %q, want %q", got.Message, "after")
		}
		if extra := drainEvents(sub.Events); len(extra) != 0 {
			t.Errorf("unexpected extra events: %v", extra)
		}
	})

	t.Run("publish with no subscribers is safe", func(t *testing.T) {
		hub := newEventHub(8)
		hub.publish(testEvent("a"))
	})

	t.Run("multiple subscribers", func(t *testing.T) {
		hub := newEventHub(8)
		first := hub.subscribe()
		second := hub.subscribe()

		hub.publish(testEvent("a"))
		hub.publish(testEvent("b"))

		for _, sub := range []*EventSubscription{first, second} {
			for _, want := range []string{"a", "b"} {
				if got := recvEvent(t, sub.Events); got.Message != want {
					t.Errorf("got %q, want %q", got.Message, want)
				}
			}
		}
	})

	t.Run("slow subscriber does not block", func(t *testing.T) {
		hub := newEventHub(2)
		stuck := hub.subscribe() // never reads
		live := hub.subscribe()

		finished := make(chan struct{})
		go func() {
			defer close(finished)
			for i := 0; i < 10; i++ {
				hub.publish(testEvent(fmt.Sprintf("event %d", i)))
				<-live.Events // keep the live subscriber caught up
			}
		}()

		select {
		case <-finished:
		case <-time.After(testTimeout):
			t.Fatal("publish blocked because of a subscriber that never reads")
		}

		if got := stuck.Dropped(); got != 8 {
			t.Errorf("stuck.Dropped() = %d, want 8", got)
		}
		if got := live.Dropped(); got != 0 {
			t.Errorf("live.Dropped() = %d, want 0", got)
		}
	})

	t.Run("drops oldest when full", func(t *testing.T) {
		hub := newEventHub(2)
		sub := hub.subscribe()

		for _, id := range []string{"a", "b", "c", "d"} {
			hub.publish(testEvent(id))
		}

		if got := sub.Dropped(); got != 2 {
			t.Errorf("Dropped() = %d, want 2", got)
		}
		if want, got := []string{"c", "d"}, drainEvents(sub.Events); !slices.Equal(got, want) {
			t.Errorf("queued events = %v, want %v", got, want)
		}
	})

	t.Run("cancel stops delivery", func(t *testing.T) {
		hub := newEventHub(8)
		sub := hub.subscribe()

		sub.Cancel()
		hub.publish(testEvent("a")) // must not panic or block

		select {
		case e, ok := <-sub.Events:
			if ok {
				t.Errorf("received %q after Cancel, want closed channel", e.Message)
			}
		case <-time.After(testTimeout):
			t.Fatal("channel was not closed by Cancel")
		}
	})

	t.Run("cancel twice is safe", func(t *testing.T) {
		hub := newEventHub(8)
		sub := hub.subscribe()

		sub.Cancel()
		sub.Cancel()
	})

	t.Run("cancel leaves other subscribers working", func(t *testing.T) {
		hub := newEventHub(8)
		first := hub.subscribe()
		second := hub.subscribe()

		first.Cancel()
		hub.publish(testEvent("a"))

		if got := recvEvent(t, second.Events); got.Message != "a" {
			t.Errorf("got %q, want %q", got.Message, "a")
		}
	})

	t.Run("capacity below one is clamped", func(t *testing.T) {
		hub := newEventHub(0)
		sub := hub.subscribe()

		finished := make(chan struct{})
		go func() {
			defer close(finished)
			hub.publish(testEvent("a"))
			hub.publish(testEvent("b"))
		}()

		select {
		case <-finished:
		case <-time.After(testTimeout):
			t.Fatal("publish blocked with capacity 0")
		}

		if got := recvEvent(t, sub.Events); got.Message != "b" {
			t.Errorf("got %q, want %q", got.Message, "b")
		}
	})
}

// TestEventHubConcurrent is mainly useful with: go test -race ./minecraft/
func TestEventHubConcurrent(t *testing.T) {
	hub := newEventHub(4)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			hub.publish(testEvent(fmt.Sprintf("event %d", i)))
		}
	}()

	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				sub := hub.subscribe()
				drainEvents(sub.Events)
				_ = sub.Dropped()
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
