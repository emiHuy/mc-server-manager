package minecraft

import (
	"log/slog"
	"sync"
)

type eventHub struct {
	mu       sync.Mutex // guards subs and each subscription's dropped count
	subs     map[*EventSubscription]struct{}
	capacity int
}

type EventSubscription struct {
	Events  <-chan Event
	ch      chan Event
	hub     *eventHub
	dropped int
}

func newEventHub(capacity int) *eventHub {
	if capacity < 1 {
		capacity = 1
	}
	return &eventHub{subs: make(map[*EventSubscription]struct{}), capacity: capacity}
}

func (eh *eventHub) publish(e Event) {
	eh.mu.Lock()
	defer eh.mu.Unlock()

	for sub := range eh.subs {
		select {
		case sub.ch <- e:
		default:
			select {
			case <-sub.ch:
				sub.dropped++
				if sub.dropped == 1 {
					slog.Warn("event subscriber is falling behind, dropping oldest events")
				}
			default:
			}
			sub.ch <- e
		}
	}
}

func (eh *eventHub) subscribe() *EventSubscription {
	eh.mu.Lock()
	defer eh.mu.Unlock()

	ch := make(chan Event, eh.capacity)
	sub := &EventSubscription{Events: ch, ch: ch, hub: eh}
	eh.subs[sub] = struct{}{}

	return sub
}

func (s *EventSubscription) Dropped() int {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()

	return s.dropped
}

func (s *EventSubscription) Cancel() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()

	if _, ok := s.hub.subs[s]; ok {
		delete(s.hub.subs, s)
		close(s.ch)
	}
}
