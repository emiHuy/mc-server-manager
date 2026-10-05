package minecraft

import (
	"sync"
)

type ringBuffer struct {
	lines []string
	next  int
	count int
	mu    sync.Mutex
}

func newRingBuffer(size int) *ringBuffer {
	if size < 1 {
		size = 1
	}

	return &ringBuffer{
		lines: make([]string, size),
	}
}

func (rb *ringBuffer) add(line string) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.lines[rb.next] = line
	rb.next = (rb.next + 1) % len(rb.lines)
	if rb.count < len(rb.lines) {
		rb.count += 1
	}
}

func (rb *ringBuffer) recent(n int) []string {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if n <= 0 {
		return []string{}
	} else if n > rb.count {
		n = rb.count
	}

	start := (rb.next - n + len(rb.lines)) % len(rb.lines)

	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = rb.lines[(start+i)%len(rb.lines)]
	}

	return result
}
