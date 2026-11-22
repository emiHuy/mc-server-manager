package fanout

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRingRecent(t *testing.T) {
	tests := []struct {
		name string
		size int
		add  []int
		n    int
		want []int
	}{
		{"empty", 3, nil, 2, []int{}},
		{"partially filled", 5, []int{1, 2, 3}, 2, []int{2, 3}},
		{"partially filled, all", 5, []int{1, 2, 3}, 3, []int{1, 2, 3}},
		{"exactly full", 3, []int{1, 2, 3}, 3, []int{1, 2, 3}},
		{"wrapped keeps newest", 3, []int{1, 2, 3, 4, 5}, 3, []int{3, 4, 5}},
		{"wrapped, fewer than count", 3, []int{1, 2, 3, 4, 5}, 2, []int{4, 5}},
		{"n larger than count is clamped", 3, []int{1, 2, 3, 4}, 10, []int{2, 3, 4}},
		{"zero n", 3, []int{1, 2}, 0, []int{}},
		{"negative n", 3, []int{1, 2}, -1, []int{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRing[int](tt.size)
			for _, v := range tt.add {
				r.add(v)
			}
			require.Equal(t, tt.want, r.recent(tt.n))
		})
	}
}

func TestNewRingSizeNormalizedToOne(t *testing.T) {
	r := newRing[int](0)
	r.add(1)
	r.add(2)

	require.Equal(t, []int{2}, r.recent(5))
}

func TestRingRecentReturnsCopy(t *testing.T) {
	r := newRing[int](3)
	r.add(1)
	r.add(2)

	got := r.recent(2)
	got[0] = 99

	again := r.recent(2)
	require.Equal(t, 1, again[0])
}
