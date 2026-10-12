package fanout

type Subscription[T any] struct {
	C       <-chan T
	ch      chan T
	hub     *Hub[T]
	dropped int
}

func (s *Subscription[T]) Dropped() int {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()

	return s.dropped
}

func (s *Subscription[T]) Cancel() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()

	if _, ok := s.hub.subs[s]; ok {
		delete(s.hub.subs, s)
		close(s.ch)
	}
}
