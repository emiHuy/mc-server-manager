package minecraft

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

const testTimeout = time.Second

// recv reads one line from ch, failing the test if nothing arrives in time.
func recv(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case line, ok := <-ch:
		if !ok {
			t.Fatal("channel closed, expected a line")
		}
		return line
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for a line")
	}
	return ""
}

// drain returns every line currently queued in ch without blocking.
func drain(ch <-chan string) []string {
	var lines []string
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return lines
			}
			lines = append(lines, line)
		default:
			return lines
		}
	}
}

func TestConsoleHub(t *testing.T) {
	t.Run("receives new lines", func(t *testing.T) {
		hub := newConsoleHub(10, 8)
		_, sub := hub.subscribe(0)

		for _, line := range []string{"a", "b", "c"} {
			hub.add(line)
		}

		for _, want := range []string{"a", "b", "c"} {
			if got := recv(t, sub.Lines); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		}
	})

	t.Run("snapshot then live", func(t *testing.T) {
		hub := newConsoleHub(10, 8)
		for _, line := range []string{"a", "b", "c"} {
			hub.add(line)
		}

		snapshot, sub := hub.subscribe(2)
		if want := []string{"b", "c"}; !slices.Equal(snapshot, want) {
			t.Errorf("snapshot = %v, want %v", snapshot, want)
		}

		hub.add("d")
		if got := recv(t, sub.Lines); got != "d" {
			t.Errorf("first live line = %q, want %q", got, "d")
		}
		if extra := drain(sub.Lines); len(extra) != 0 {
			t.Errorf("unexpected extra lines on channel: %v", extra)
		}
	})

	t.Run("non-positive snapshot", func(t *testing.T) {
		for _, n := range []int{0, -1} {
			hub := newConsoleHub(10, 8)
			hub.add("a")

			snapshot, sub := hub.subscribe(n)
			if len(snapshot) != 0 {
				t.Errorf("Subscribe(%d) snapshot = %v, want empty", n, snapshot)
			}

			hub.add("b")
			if got := recv(t, sub.Lines); got != "b" {
				t.Errorf("Subscribe(%d) live line = %q, want %q", n, got, "b")
			}
		}
	})

	t.Run("multiple subscribers", func(t *testing.T) {
		hub := newConsoleHub(10, 8)
		_, first := hub.subscribe(0)
		_, second := hub.subscribe(0)

		hub.add("a")
		hub.add("b")

		for _, sub := range []*Subscription{first, second} {
			for _, want := range []string{"a", "b"} {
				if got := recv(t, sub.Lines); got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
		}
	})

	t.Run("slow subscriber does not block", func(t *testing.T) {
		hub := newConsoleHub(10, 2)
		_, stuck := hub.subscribe(0) // never reads
		_, live := hub.subscribe(0)

		finished := make(chan struct{})
		go func() {
			defer close(finished)
			for i := 0; i < 10; i++ {
				hub.add(fmt.Sprintf("line %d", i))
				<-live.Lines // keep the live subscriber caught up
			}
		}()

		select {
		case <-finished:
		case <-time.After(testTimeout):
			t.Fatal("add blocked because of a subscriber that never reads")
		}

		if got := stuck.Dropped(); got != 8 {
			t.Errorf("stuck.Dropped() = %d, want 8", got)
		}
		if got := live.Dropped(); got != 0 {
			t.Errorf("live.Dropped() = %d, want 0", got)
		}
	})

	t.Run("drops oldest when full", func(t *testing.T) {
		hub := newConsoleHub(10, 2)
		_, sub := hub.subscribe(0)

		for _, line := range []string{"a", "b", "c", "d"} {
			hub.add(line)
		}

		if got := sub.Dropped(); got != 2 {
			t.Errorf("Dropped() = %d, want 2", got)
		}
		if want, got := []string{"c", "d"}, drain(sub.Lines); !slices.Equal(got, want) {
			t.Errorf("queued lines = %v, want %v", got, want)
		}
	})

	t.Run("history keeps every line even when a subscriber drops", func(t *testing.T) {
		hub := newConsoleHub(10, 1)
		_, sub := hub.subscribe(0)

		for _, line := range []string{"a", "b", "c"} {
			hub.add(line)
		}

		if got := sub.Dropped(); got != 2 {
			t.Errorf("Dropped() = %d, want 2", got)
		}
		if want, got := []string{"a", "b", "c"}, hub.recent(10); !slices.Equal(got, want) {
			t.Errorf("recent(10) = %v, want %v", got, want)
		}
	})

	t.Run("cancel stops delivery", func(t *testing.T) {
		hub := newConsoleHub(10, 8)
		_, sub := hub.subscribe(0)

		sub.Cancel()
		hub.add("a") // must not panic or block

		select {
		case line, ok := <-sub.Lines:
			if ok {
				t.Errorf("received %q after Cancel, want closed channel", line)
			}
		case <-time.After(testTimeout):
			t.Fatal("channel was not closed by Cancel")
		}
	})

	t.Run("cancel twice is safe", func(t *testing.T) {
		hub := newConsoleHub(10, 8)
		_, sub := hub.subscribe(0)

		sub.Cancel()
		sub.Cancel()
	})

	t.Run("cancel leaves other subscribers working", func(t *testing.T) {
		hub := newConsoleHub(10, 8)
		_, first := hub.subscribe(0)
		_, second := hub.subscribe(0)

		first.Cancel()
		hub.add("a")

		if got := recv(t, second.Lines); got != "a" {
			t.Errorf("got %q, want %q", got, "a")
		}
	})

	t.Run("capacity below one is clamped", func(t *testing.T) {
		hub := newConsoleHub(10, 0)
		_, sub := hub.subscribe(0)

		finished := make(chan struct{})
		go func() {
			defer close(finished)
			hub.add("a")
			hub.add("b")
		}()

		select {
		case <-finished:
		case <-time.After(testTimeout):
			t.Fatal("add blocked with capacity 0")
		}

		if got := recv(t, sub.Lines); got != "b" {
			t.Errorf("got %q, want %q", got, "b")
		}
	})
}

// TestConsoleHubConcurrent is mainly useful with: go test -race ./minecraft/
func TestConsoleHubConcurrent(t *testing.T) {
	hub := newConsoleHub(50, 4)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			hub.add(fmt.Sprintf("line %d", i))
		}
	}()

	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, sub := hub.subscribe(5)
				drain(sub.Lines)
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
