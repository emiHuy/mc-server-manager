package minecraft

import (
	"log/slog"
	"sync"
)

type consoleHub struct {
	mu       sync.Mutex // guards subs, each subscription's dropped, and buffer writes
	buf      *ringBuffer
	subs     map[*Subscription]struct{}
	capacity int
}

type Subscription struct {
	Lines   <-chan string
	ch      chan string
	dropped int
	hub     *consoleHub
}

func (c *consoleHub) subscribe(n int) ([]string, *Subscription) {
	c.mu.Lock()
	defer c.mu.Unlock()

	snapshot := c.buf.recent(n)
	ch := make(chan string, c.capacity)

	sub := &Subscription{Lines: ch, ch: ch, hub: c}
	c.subs[sub] = struct{}{}

	return snapshot, sub
}

func (s *Subscription) Dropped() int {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()

	return s.dropped
}

func (s *Subscription) Cancel() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()

	_, ok := s.hub.subs[s]
	if ok {
		delete(s.hub.subs, s)
		close(s.ch)
	}
}

func newConsoleHub(bufferSize, capacity int) *consoleHub {
	buf := newRingBuffer(bufferSize)

	if capacity < 1 {
		capacity = 1
	}

	return &consoleHub{buf: buf, capacity: capacity, subs: make(map[*Subscription]struct{})}
}

func (c *consoleHub) add(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.buf.add(line)

	for sub := range c.subs {
		select {
		case sub.ch <- line:
			// room in channel
		default:
			// channel full: drop oldest line
			select {
			case <-sub.ch:
				sub.dropped++
				if sub.dropped == 1 {
					slog.Warn("console subscriber is falling behind, dropping oldest lines")
				}
			default:
			}
			sub.ch <- line
		}
	}
}

func (c *consoleHub) recent(n int) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.buf.recent(n)
}
