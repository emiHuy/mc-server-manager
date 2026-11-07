package minecraft

import (
	"encoding/json/v2"
	"testing"
)

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

func TestStateJSONRoundTrip(t *testing.T) {
	for _, want := range []State{StateStopped, StateStarting, StateRunning, StateStopping, StateCrashed} {
		data, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal %v: %v", want, err)
		}
		if string(data) != `"`+want.String()+`"` {
			t.Fatalf("marshal %v = %s, want a quoted string", want, data)
		}

		var got State
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if got != want {
			t.Fatalf("round trip: got %v, want %v", got, want)
		}
	}
}

func TestStateUnmarshalUnknown(t *testing.T) {
	var s State
	if err := json.Unmarshal([]byte(`"bogus"`), &s); err == nil {
		t.Fatal("expected an error for an unknown state")
	}
}
