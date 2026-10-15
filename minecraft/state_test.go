package minecraft

import "testing"

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateStopped, "stopped"},
		{StateStarting, "starting"},
		{StateRunning, "running"},
		{StateStopping, "stopping"},
		{StateCrashed, "crashed"},
		{State(99), "unknown"},
		{State(-1), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("State(%d).String() = %q, want %q", int(tt.state), got, tt.want)
			}
		})
	}
}

func TestStateNamesAreComplete(t *testing.T) {
	seen := make(map[string]State)

	for s := StateStopped; s <= StateCrashed; s++ {
		name := s.String()
		if name == "unknown" {
			t.Errorf("State(%d) has no name", int(s))
		}
		if other, dup := seen[name]; dup {
			t.Errorf("States %d and %d share the name %q", int(other), int(s), name)
		}
		seen[name] = s
	}
}

func TestStateLive(t *testing.T) {
	tests := []struct {
		state State
		want  bool
	}{
		{StateStarting, true},
		{StateRunning, true},
		{StateStopping, false},
		{StateStopped, false},
		{StateCrashed, false},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			if got := tt.state.live(); got != tt.want {
				t.Errorf("%v.live() = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}
