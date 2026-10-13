package fanout

// Subscription represents a subscriber to a Hub.
//
// C is the receive-only channel through which published items are delivered.
// A subscription must be canceled when it is no longer needed.
type Subscription[T any] struct {
	C       <-chan T
	ch      chan T
	hub     *Hub[T]
	dropped int
}

// Dropped returns the number of items discarded because the subscriber's
// channel was full.
//
// It is safe to call concurrently with publishing and cancellation.
func (s *Subscription[T]) Dropped() int {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()

	return s.dropped
}

// Cancel removes the subscription from its Hub and closes its channel.
//
// Cancel is safe to call multiple times.
func (s *Subscription[T]) Cancel() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()

	if _, ok := s.hub.subs[s]; ok {
		delete(s.hub.subs, s)
		close(s.ch)
	}
}
