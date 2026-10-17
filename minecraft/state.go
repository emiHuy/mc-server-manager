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

func (s *State) Unmarshal(text []byte) error {
	str := string(text)
	switch str {
	case "stopped":
		*s = StateStopped
	case "starting":
		*s = StateStarting
	case "running":
		*s = StateRunning
	case "stopping":
		*s = StateStopping
	case "crashed":
		*s = StateCrashed
	default:
		return fmt.Errorf("%w: %q", ErrInvalidState, str)
	}
	return nil
}

// live reports whether the server is starting or running.
func (s State) live() bool {
	return s == StateStarting || s == StateRunning
}
