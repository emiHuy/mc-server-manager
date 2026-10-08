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
	EventServerWarning
	EventServerError
	EventServerReady
	EventStateChanged
	EventPlayerJoined
	EventPlayerLeft
	EventPlayerChat
)

func (et EventType) String() string {
	switch et {
	case EventServerWarning:
		return "server_warning"
	case EventServerError:
		return "server_error"
	case EventServerReady:
		return "server_ready"
	case EventStateChanged:
		return "state_changed"
	case EventPlayerJoined:
		return "player_joined"
	case EventPlayerLeft:
		return "player_left"
	case EventPlayerChat:
		return "player_chat"
	default:
		return "unknown"
	}
}
