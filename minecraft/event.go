package minecraft

import "time"

type EventType int

type Event struct {
	Type      EventType
	Timestamp time.Time
	Player    string // populated for player-related events
	Message   string // populated for chat, warning, and error events
}

const (
	EventUnknown EventType = iota
	EventServerReady
	EventPlayerJoined
	EventPlayerLeft
	EventPlayerChat
	EventServerWarning
	EventServerError
)

func (et EventType) String() string {
	switch et {
	case EventServerReady:
		return "server_ready"
	default:
		return "unknown"
	}
}
