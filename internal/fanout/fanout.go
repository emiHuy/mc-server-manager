// Package fanout provides a generic publish-subscribe hub that distributes items to subscribers and optionally retains a history of recent items.
package fanout

import (
	"log/slog"
	"sync"
)

// Hub distributes published items to subscribers and optionally retains
// a bounded history of recent items.
//
// A Hub is safe for concurrent use.
type Hub[T any] struct {
	name     string
	capacity int

	mu      sync.Mutex // guards subs, subscriber drop counts, and history
	subs    map[*Subscription[T]]struct{}
	history *ring[T]
}

// New creates a Hub with the given name, subscriber channel capacity, and history capacity.
//
// A capacity less than one is treated as one. History is disabled when
// history is less than one.
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

// Publish sends item to every current subscriber and adds it to history when history is enabled.
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
					slog.Warn("subscriber is falling behind, dropping oldest items", "hub", h.name)
				}
			default:
			}
			sub.ch <- item
		}
	}
}

// Subscribe registers a new subscriber and returns the most recent n historical items along with the subscription.
//
// The returned history is a snapshot taken while registering the subscriber.
// Historical items are not automatically sent through the subscription channel.
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

// recentLocked returns recent history while h.mu is held.
func (h *Hub[T]) recentLocked(n int) []T {
	if h.history == nil {
		return []T{}
	}
	return h.history.recent(n)
}
