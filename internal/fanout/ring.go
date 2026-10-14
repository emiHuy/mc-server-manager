package fanout

// ring is a fixed-size circular buffer that retains the most recently added items.
type ring[T any] struct {
	items []T
	next  int
	count int
}

func newRing[T any](size int) *ring[T] {
	if size < 1 {
		size = 1
	}
	return &ring[T]{items: make([]T, size)}
}

func (r *ring[T]) add(item T) {
	r.items[r.next] = item
	r.next = (r.next + 1) % len(r.items)
	if r.count < len(r.items) {
		r.count++
	}
}

func (r *ring[T]) recent(n int) []T {
	if n <= 0 {
		return []T{}
	} else if n > r.count {
		n = r.count
	}

	start := (r.next - n + len(r.items)) % len(r.items)

	result := make([]T, n)
	for i := 0; i < n; i++ {
		result[i] = r.items[(start+i)%len(r.items)]
	}
	return result
}
