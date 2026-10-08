package minecraft

import (
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
)

func TestReadConsoleFillsBuffer(t *testing.T) {
	inst := New(config.MinecraftConfig{})

	runReadConsole(t, inst, "a", "b", "c")

	got := inst.RecentConsole(10)
	want := []string{"a", "b", "c"}

	if !slices.Equal(got, want) {
		t.Errorf("RecentConsole(10) mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func runReadConsole(t *testing.T, inst *Instance, lines ...string) {
	t.Helper()

	input := strings.Join(lines, "\n") + "\n"
	reader := io.NopCloser(strings.NewReader(input))
	done := make(chan struct{})

	go inst.readConsole(reader, done)

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("timeout waiting for readConsole to complete")
	}
}

func stateOf(inst *Instance) State {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.state
}

func newInstanceInState(state State) *Instance {
	inst := New(config.MinecraftConfig{})
	inst.state = state
	return inst
}

func TestReadConsoleReadyLine(t *testing.T) {
	t.Run("ready line moves starting to running", func(t *testing.T) {
		inst := newInstanceInState(StateStarting)

		runReadConsole(t, inst, readyLine)

		if got := stateOf(inst); got != StateRunning {
			t.Errorf("state = %v, want %v", got, StateRunning)
		}
	})

	t.Run("ready line publishes state change then ready event", func(t *testing.T) {
		inst := newInstanceInState(StateStarting)
		sub := inst.SubscribeEvents()
		defer sub.Cancel()

		runReadConsole(t, inst, readyLine)

		first := recvEvent(t, sub.Events)
		if first.Type != EventStateChanged || first.State != StateRunning {
			t.Errorf("first event = %+v, want state_changed to running", first)
		}

		second := recvEvent(t, sub.Events)
		if second.Type != EventServerReady {
			t.Errorf("second event type = %v, want %v", second.Type, EventServerReady)
		}
		if want := `Done (0.439s)! For help, type "help"`; second.Message != want {
			t.Errorf("ready event message = %q, want %q", second.Message, want)
		}
	})

	t.Run("ready line is kept in console history", func(t *testing.T) {
		inst := newInstanceInState(StateStarting)

		runReadConsole(t, inst, "[20:26:33] [Server thread/INFO]: Preparing level", readyLine)

		got := inst.RecentConsole(10)
		want := []string{"[20:26:33] [Server thread/INFO]: Preparing level", readyLine}
		if !slices.Equal(got, want) {
			t.Errorf("RecentConsole(10) = %v, want %v", got, want)
		}
	})

	t.Run("repeated ready line only flips once", func(t *testing.T) {
		inst := newInstanceInState(StateStarting)
		sub := inst.SubscribeEvents()
		defer sub.Cancel()

		runReadConsole(t, inst, readyLine, readyLine)

		// One state_changed and one server_ready, nothing more.
		recvEvent(t, sub.Events)
		recvEvent(t, sub.Events)
		if extra := drainEvents(sub.Events); len(extra) != 0 {
			t.Errorf("unexpected extra events: %v", extra)
		}
	})

	t.Run("ready line is ignored unless starting", func(t *testing.T) {
		for _, state := range []State{StateStopping, StateStopped, StateCrashed, StateRunning} {
			inst := newInstanceInState(state)
			sub := inst.SubscribeEvents()

			runReadConsole(t, inst, readyLine)

			if got := stateOf(inst); got != state {
				t.Errorf("state %v changed to %v, want it unchanged", state, got)
			}
			if events := drainEvents(sub.Events); len(events) != 0 {
				t.Errorf("state %v published events %v, want none", state, events)
			}
			sub.Cancel()
		}
	})

	t.Run("ordinary lines change nothing", func(t *testing.T) {
		inst := newInstanceInState(StateStarting)
		sub := inst.SubscribeEvents()
		defer sub.Cancel()

		runReadConsole(
			t, inst,
			"[20:26:33] [Server thread/INFO]: Preparing spawn area: 100%",
			`[20:26:34] [Server thread/INFO]: <Steve> Done (0.439s)! For help, type "help"`,
			`Exception in thread "main" java.lang.Error`,
		)

		if got := stateOf(inst); got != StateStarting {
			t.Errorf("state = %v, want %v", got, StateStarting)
		}
		if events := drainEvents(sub.Events); len(events) != 0 {
			t.Errorf("published events %v, want none", events)
		}
	})
}
