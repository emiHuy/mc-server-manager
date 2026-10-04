package minecraft

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
