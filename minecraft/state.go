package minecraft

import "fmt"

// State represents the lifecycle state of a Minecraft server instance.
type State int

const (
	StateStopped State = iota
	StateStarting
	StateRunning
	StateStopping
	StateCrashed
)

func (s State) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateCrashed:
		return "crashed"
	default:
		return "unknown"
	}
}

func (s State) MarshalText() ([]byte, error) {
	return []byte(s.String()), nil
}

func (s *State) UnmarshalText(text []byte) error {
	for _, c := range []State{StateStopped, StateStarting, StateRunning, StateStopping, StateCrashed} {
		if c.String() == string(text) {
			*s = c
			return nil
		}
	}
	return fmt.Errorf("unknown state %q", text)
}

// live reports whether the server is starting or running.
func (s State) live() bool {
	return s == StateStarting || s == StateRunning
}
