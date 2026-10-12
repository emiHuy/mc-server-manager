package fanout

import (
	"slices"
	"testing"
)

func TestRingRecent(t *testing.T) {
	tests := []struct {
		name  string
		size  int
		items []string
		n     int
		want  []string
	}{
		{
			name:  "empty ring",
			size:  5,
			items: nil,
			n:     5,
			want:  []string{},
		},
		{
			name:  "partial fill",
			size:  5,
			items: []string{"a", "b"},
			n:     5,
			want:  []string{"a", "b"},
		},
		{
			name:  "exactly full",
			size:  3,
			items: []string{"a", "b", "c"},
			n:     3,
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "wrapped",
			size:  3,
			items: []string{"a", "b", "c", "d", "e"},
			n:     3,
			want:  []string{"c", "d", "e"},
		},
		{
			name:  "fewer than count",
			size:  3,
			items: []string{"a", "b", "c", "d", "e"},
			n:     2,
			want:  []string{"d", "e"},
		},
		{
			name:  "over-ask after wrap",
			size:  3,
			items: []string{"a", "b", "c", "d", "e"},
			n:     100,
			want:  []string{"c", "d", "e"},
		},
		{
			name:  "zero",
			size:  3,
			items: []string{"a", "b", "c", "d", "e"},
			n:     0,
			want:  []string{},
		},
		{
			name:  "negative",
			size:  3,
			items: []string{"a", "b", "c", "d", "e"},
			n:     -1,
			want:  []string{},
		},
		{
			name:  "size clamp zero",
			size:  0,
			items: []string{"a", "b"},
			n:     1,
			want:  []string{"b"},
		},
		{
			name:  "size clamp negative",
			size:  -5,
			items: []string{"a", "b"},
			n:     1,
			want:  []string{"b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRing[string](tt.size)
			for _, item := range tt.items {
				r.add(item)
			}

			got := r.recent(tt.n)
			if !slices.Equal(got, tt.want) {
				t.Errorf("recent(%d) = %v, want %v", tt.n, got, tt.want)
			}
		})
	}
}

func TestRingRecentReturnsCopy(t *testing.T) {
	r := newRing[string](3)
	for _, item := range []string{"a", "b", "c"} {
		r.add(item)
	}

	got := r.recent(3)
	if len(got) != 3 {
		t.Fatalf("recent(3) returned %d items, want 3", len(got))
	}
	got[0] = "x"

	want := []string{"a", "b", "c"}
	if again := r.recent(3); !slices.Equal(again, want) {
		t.Errorf("recent(3) after modifying a previous result = %v, want %v", again, want)
	}
}

func TestRingWorksWithStructs(t *testing.T) {
	type point struct{ x, y int }

	r := newRing[point](2)
	r.add(point{1, 1})
	r.add(point{2, 2})
	r.add(point{3, 3})

	got := r.recent(2)
	want := []point{{2, 2}, {3, 3}}
	if !slices.Equal(got, want) {
		t.Errorf("recent(2) = %v, want %v", got, want)
	}
}
