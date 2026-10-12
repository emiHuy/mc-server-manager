package fanout

import (
	"log/slog"
	"sync"
)

type Hub[T any] struct {
	name     string
	capacity int

	mu      sync.Mutex // guards subs, sub dropped count, and history
	subs    map[*Subscription[T]]struct{}
	history *ring[T]
}

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

func (h *Hub[T]) Subscribe(n int) ([]T, *Subscription[T]) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan T, h.capacity)
	sub := &Subscription[T]{C: ch, ch: ch, hub: h}
	h.subs[sub] = struct{}{}

	return h.recentLocked(n), sub
}

func (h *Hub[T]) Recent(n int) []T {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.recentLocked(n)
}

func (h *Hub[T]) recentLocked(n int) []T {
	if h.history == nil {
		return []T{}
	}
	return h.history.recent(n)
}
