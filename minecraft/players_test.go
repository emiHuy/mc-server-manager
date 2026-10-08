package minecraft

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestPlayerSet(t *testing.T) {
	tests := []struct {
		name string
		ops  func(ps *playerSet)
		want []string
	}{
		{
			name: "empty set",
			ops:  func(ps *playerSet) {},
			want: []string{},
		},
		{
			name: "add one",
			ops:  func(ps *playerSet) { ps.add("Steve") },
			want: []string{"Steve"},
		},
		{
			name: "duplicate add keeps one entry",
			ops: func(ps *playerSet) {
				ps.add("Steve")
				ps.add("Steve")
			},
			want: []string{"Steve"},
		},
		{
			name: "output is sorted regardless of join order",
			ops: func(ps *playerSet) {
				ps.add("Steve")
				ps.add("Alex")
				ps.add("Notch")
			},
			want: []string{"Alex", "Notch", "Steve"},
		},
		{
			name: "remove existing player",
			ops: func(ps *playerSet) {
				ps.add("Steve")
				ps.add("Alex")
				ps.remove("Steve")
			},
			want: []string{"Alex"},
		},
		{
			name: "remove unknown player is a no-op",
			ops: func(ps *playerSet) {
				ps.add("Steve")
				ps.remove("Alex")
			},
			want: []string{"Steve"},
		},
		{
			name: "remove from empty set is a no-op",
			ops:  func(ps *playerSet) { ps.remove("Steve") },
			want: []string{},
		},
		{
			name: "clear empties the set",
			ops: func(ps *playerSet) {
				ps.add("Steve")
				ps.add("Alex")
				ps.clear()
			},
			want: []string{},
		},
		{
			name: "add after clear",
			ops: func(ps *playerSet) {
				ps.add("Steve")
				ps.clear()
				ps.add("Alex")
			},
			want: []string{"Alex"},
		},
		{
			name: "rejoin after leaving",
			ops: func(ps *playerSet) {
				ps.add("Steve")
				ps.remove("Steve")
				ps.add("Steve")
			},
			want: []string{"Steve"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := newPlayerSet()
			tt.ops(ps)

			got := ps.list()
			if !slices.Equal(got, tt.want) {
				t.Errorf("list() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlayerSetListReturnsCopy(t *testing.T) {
	ps := newPlayerSet()
	ps.add("Steve")
	ps.add("Alex")

	got := ps.list()
	if len(got) != 2 {
		t.Fatalf("list() returned %d players, want 2", len(got))
	}
	got[0] = "changed"

	want := []string{"Alex", "Steve"}
	if again := ps.list(); !slices.Equal(again, want) {
		t.Errorf("list() after modifying a previous result = %v, want %v", again, want)
	}
}

// TestPlayerSetConcurrent is mainly useful with: go test -race ./minecraft/
func TestPlayerSetConcurrent(t *testing.T) {
	ps := newPlayerSet()

	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				name := fmt.Sprintf("player-%d-%d", g, i%5)
				ps.add(name)
				ps.list()
				ps.remove(name)
				if i%50 == 0 {
					ps.clear()
				}
			}
		}(g)
	}

	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent player set operations did not finish (possible deadlock)")
	}
}
