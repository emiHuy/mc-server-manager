package minecraft

import (
	"slices"
	"testing"
)

func TestRecent(t *testing.T) {
	tests := []struct {
		name  string
		size  int
		lines []string
		n     int
		want  []string
	}{
		{
			name:  "empty buffer",
			size:  5,
			lines: nil,
			n:     5,
			want:  []string{},
		},
		{
			name:  "partial fill",
			size:  5,
			lines: []string{"a", "b"},
			n:     5,
			want:  []string{"a", "b"},
		},
		{
			name:  "exactly full",
			size:  3,
			lines: []string{"a", "b", "c"},
			n:     3,
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "wrapped",
			size:  3,
			lines: []string{"a", "b", "c", "d", "e"},
			n:     3,
			want:  []string{"c", "d", "e"},
		},
		{
			name:  "fewer than count",
			size:  3,
			lines: []string{"a", "b", "c", "d", "e"},
			n:     2,
			want:  []string{"d", "e"},
		},
		{
			name:  "over-ask after wrap",
			size:  3,
			lines: []string{"a", "b", "c", "d", "e"},
			n:     100,
			want:  []string{"c", "d", "e"},
		},
		{
			name:  "zero",
			size:  3,
			lines: []string{"a", "b", "c", "d", "e"},
			n:     0,
			want:  []string{},
		},
		{
			name:  "negative",
			size:  3,
			lines: []string{"a", "b", "c", "d", "e"},
			n:     -1,
			want:  []string{},
		},
		{
			name:  "size clamp zero",
			size:  0,
			lines: []string{"a", "b"},
			n:     1,
			want:  []string{"b"},
		},
		{
			name:  "size clamp negative",
			size:  -5,
			lines: []string{"a", "b"},
			n:     1,
			want:  []string{"b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := newRingBuffer(tt.size)
			for _, line := range tt.lines {
				rb.add(line)
			}
			got := rb.recent(tt.n)
			if !slices.Equal(got, tt.want) {
				t.Errorf("recent(%d) = %v, want %v", tt.n, got, tt.want)
			}
		})
	}
}

func TestRecentReturnsCopy(t *testing.T) {
	rb := newRingBuffer(3)
	lines := []string{"a", "b", "c"}

	for _, line := range lines {
		rb.add(line)
	}

	got := rb.recent(3)
	if len(got) != 3 {
		t.Fatalf("recent(3) returned %d lines, want 3", len(got))
	}
	got[0] = "x"

	want := lines
	again := rb.recent(3)

	if !slices.Equal(want, again) {
		t.Errorf("recent(3) after modifying a previous result = %v, want %v", again, want)
	}
}
