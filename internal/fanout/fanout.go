// Package fanout provides a generic publish-subscribe hub that distributes items to subscribers and optionally retains a history of recent items.
package fanout

import (
	"log/slog"
	"sync"
)

// Hub fans out items to active subscribers and optionally retains recent history.
//
// It is safe for concurrent use.
type Hub[T any] struct {
	name     string
	capacity int

	mu      sync.Mutex
	subs    map[*Subscription[T]]struct{}
	history *ring[T]
}

// New returns a Hub with the given name, per-subscriber buffer capacity, and history size.
//
// Values less than 1 normalize capacity to 1 and disable history.
func New[T any](name string, capacity int, history int) *Hub[T] {
	if capacity < 1 {
		capacity = 1
	}
	h := &Hub[T]{
		name:     name,
		capacity: capacity,
		subs:     make(map[*Subscription[T]]struct{}),
	}

	if history > 0 {
		h.history = newRing[T](history)
	}

	return h
}

// Publish sends item to all subscribers without blocking.
//
// If a subscriber's channel is full, the oldest queued item is discarded to make room for the new item.
// Publishing does not wait for subscribers to consume items.
func (h *Hub[T]) Publish(item T) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.history != nil {
		h.history.add(item)
	}

	for sub := range h.subs {
		select {
		case sub.ch <- item:
		default:
			select {
			case <-sub.ch:
				sub.dropped++
				if sub.dropped == 1 {
					slog.Warn("fanout: subscriber is falling behind, dropping oldest items", "hub", h.name)
				}
			default:
			}
			sub.ch <- item
		}
	}
}

// Subscribe registers a subscriber and returns a snapshot of up to n recent items alongside the subscription.
//
// Historical items are not sent on the channel.
func (h *Hub[T]) Subscribe(n int) ([]T, *Subscription[T]) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan T, h.capacity)
	sub := &Subscription[T]{C: ch, ch: ch, hub: h}
	h.subs[sub] = struct{}{}

	return h.recentLocked(n), sub
}

// Recent returns up to the most recent n historical items, oldest first.
//
// It returns an empty slice when history is disabled.
func (h *Hub[T]) Recent(n int) []T {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.recentLocked(n)
}

// Len returns the number of subscribers.
func (h *Hub[T]) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return len(h.subs)
}

// recentLocked returns recent history while h.mu is held.
func (h *Hub[T]) recentLocked(n int) []T {
	if h.history == nil {
		return []T{}
	}
	return h.history.recent(n)
}
