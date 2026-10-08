package minecraft

import "testing"

func TestEventTypeString(t *testing.T) {
	tests := []struct {
		name string
		et   EventType
		want string
	}{
		{name: "server ready", et: EventServerReady, want: "server_ready"},
		{name: "state changed", et: EventStateChanged, want: "state_changed"},
		{name: "zero value is unknown", et: EventUnknown, want: "unknown"},
		{name: "out of range is unknown", et: EventType(99), want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.et.String(); got != tt.want {
				t.Errorf("EventType(%d).String() = %q, want %q", int(tt.et), got, tt.want)
			}
		})
	}
}
