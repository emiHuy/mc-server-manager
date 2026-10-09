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
			`Exception in thread "main" java.lang.Error`,
		)

		if got := stateOf(inst); got != StateStarting {
			t.Errorf("state = %v, want %v", got, StateStarting)
		}
		if events := drainEvents(sub.Events); len(events) != 0 {
			t.Errorf("published events %v, want none", events)
		}
	})

	t.Run("chat imitating the ready line does not flip state", func(t *testing.T) {
		inst := newInstanceInState(StateStarting)
		sub := inst.SubscribeEvents()
		defer sub.Cancel()

		runReadConsole(t, inst, `[20:26:34] [Server thread/INFO]: <Steve> Done (0.439s)! For help, type "help"`)

		if got := stateOf(inst); got != StateStarting {
			t.Errorf("state = %v, want %v", got, StateStarting)
		}

		e := recvEvent(t, sub.Events)
		if e.Type != EventPlayerChat || e.Player != "Steve" {
			t.Errorf("event = %+v, want a player_chat event from Steve", e)
		}
		if extra := drainEvents(sub.Events); len(extra) != 0 {
			t.Errorf("unexpected extra events: %v", extra)
		}
	})
}

// infoLine builds a console line the way Minecraft prints INFO messages.
func infoLine(message string) string {
	return "[20:30:01] [Server thread/INFO]: " + message
}

func TestReadConsolePlayers(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  []string
	}{
		{
			name:  "one join",
			lines: []string{infoLine("Steve joined the game")},
			want:  []string{"Steve"},
		},
		{
			name: "two joins are listed sorted",
			lines: []string{
				infoLine("Steve joined the game"),
				infoLine("Alex joined the game"),
			},
			want: []string{"Alex", "Steve"},
		},
		{
			name: "join then leave",
			lines: []string{
				infoLine("Steve joined the game"),
				infoLine("Steve left the game"),
			},
			want: []string{},
		},
		{
			name: "duplicate join keeps one entry",
			lines: []string{
				infoLine("Steve joined the game"),
				infoLine("Steve joined the game"),
			},
			want: []string{"Steve"},
		},
		{
			name: "leave for an unknown player changes nothing",
			lines: []string{
				infoLine("Steve joined the game"),
				infoLine("Alex left the game"),
			},
			want: []string{"Steve"},
		},
		{
			name: "rejoin after leaving",
			lines: []string{
				infoLine("Steve joined the game"),
				infoLine("Steve left the game"),
				infoLine("Steve joined the game"),
			},
			want: []string{"Steve"},
		},
		{
			name:  "chat imitating a join adds nobody",
			lines: []string{infoLine("<Steve> Alex joined the game")},
			want:  []string{},
		},
		{
			name: "chat imitating a leave removes nobody",
			lines: []string{
				infoLine("Alex joined the game"),
				infoLine("<Steve> Alex left the game"),
			},
			want: []string{"Alex"},
		},
		{
			name:  "name with a space is ignored",
			lines: []string{infoLine("Some One joined the game")},
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := newInstanceInState(StateRunning)

			runReadConsole(t, inst, tt.lines...)

			if got := inst.Players(); !slices.Equal(got, tt.want) {
				t.Errorf("Players() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadConsolePublishesPlayerEvents(t *testing.T) {
	t.Run("join, chat, and leave are published in order", func(t *testing.T) {
		inst := newInstanceInState(StateRunning)
		sub := inst.SubscribeEvents()
		defer sub.Cancel()

		runReadConsole(
			t, inst,
			infoLine("Steve joined the game"),
			infoLine("<Steve> hi"),
			infoLine("Steve left the game"),
		)

		joined := recvEvent(t, sub.Events)
		if joined.Type != EventPlayerJoined || joined.Player != "Steve" {
			t.Errorf("first event = %+v, want player_joined for Steve", joined)
		}

		chat := recvEvent(t, sub.Events)
		if chat.Type != EventPlayerChat || chat.Player != "Steve" || chat.Message != "hi" {
			t.Errorf("second event = %+v, want player_chat from Steve saying %q", chat, "hi")
		}

		left := recvEvent(t, sub.Events)
		if left.Type != EventPlayerLeft || left.Player != "Steve" {
			t.Errorf("third event = %+v, want player_left for Steve", left)
		}

		if extra := drainEvents(sub.Events); len(extra) != 0 {
			t.Errorf("unexpected extra events: %v", extra)
		}
	})

	t.Run("chat imitating a join publishes chat, not a join", func(t *testing.T) {
		inst := newInstanceInState(StateRunning)
		sub := inst.SubscribeEvents()
		defer sub.Cancel()

		runReadConsole(t, inst, infoLine("<Steve> Alex joined the game"))

		got := recvEvent(t, sub.Events)
		if got.Type != EventPlayerChat || got.Player != "Steve" || got.Message != "Alex joined the game" {
			t.Errorf("event = %+v, want player_chat from Steve saying %q", got, "Alex joined the game")
		}
		if extra := drainEvents(sub.Events); len(extra) != 0 {
			t.Errorf("unexpected extra events: %v", extra)
		}
	})

	t.Run("warnings and errors are published", func(t *testing.T) {
		inst := newInstanceInState(StateRunning)
		sub := inst.SubscribeEvents()
		defer sub.Cancel()

		runReadConsole(
			t, inst,
			"[20:30:01] [Server thread/WARN]: Can't keep up!",
			"[20:30:02] [Server thread/ERROR]: Failed to save level",
		)

		warn := recvEvent(t, sub.Events)
		if warn.Type != EventServerWarning || warn.Message != "Can't keep up!" {
			t.Errorf("first event = %+v, want server_warning %q", warn, "Can't keep up!")
		}

		errEvent := recvEvent(t, sub.Events)
		if errEvent.Type != EventServerError || errEvent.Message != "Failed to save level" {
			t.Errorf("second event = %+v, want server_error %q", errEvent, "Failed to save level")
		}
	})

	t.Run("ordinary lines publish nothing", func(t *testing.T) {
		inst := newInstanceInState(StateRunning)
		sub := inst.SubscribeEvents()
		defer sub.Cancel()

		runReadConsole(
			t, inst,
			infoLine("Preparing spawn area: 100%"),
			`Exception in thread "main" java.lang.Error`,
		)

		if events := drainEvents(sub.Events); len(events) != 0 {
			t.Errorf("published events %v, want none", events)
		}
	})
}
