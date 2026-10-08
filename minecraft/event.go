package minecraft

import "time"

type EventType int

type Event struct {
	Type      EventType
	Timestamp time.Time
	Player    string // only meaningful for EventPlayerJoined, EventPlayerLeft, EventPlayerChat
	Message   string // the console message, for events parsed from a console line
	State     State  // only meaningful for EventStateChanged
}

const (
	EventUnknown EventType = iota
	EventServerReady
	EventStateChanged
	// not emitted yet
	// EventPlayerJoined
	// EventPlayerLeft
	// EventPlayerChat
	// EventServerWarning
	// EventServerError
)

func (et EventType) String() string {
	switch et {
	case EventServerReady:
		return "server_ready"
	case EventStateChanged:
		return "state_changed"
	default:
		return "unknown"
	}
}
