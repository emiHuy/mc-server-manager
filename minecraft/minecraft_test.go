package minecraft

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
)

// recordingWriter stands in for the server's stdin. It is deliberately not
// safe for concurrent use, so a test using it also checks that Send
// serializes its writes.
type recordingWriter struct {
	bytes.Buffer
}

func (w *recordingWriter) Close() error { return nil }

// failingWriter is a stdin whose writes always fail.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }
func (failingWriter) Close() error                { return nil }

func TestReadConsoleFillsHistory(t *testing.T) {
	inst := New(config.MinecraftConfig{})

	runReadConsole(t, inst, "a", "b", "c")

	got := inst.RecentConsole(10)
	want := []string{"a", "b", "c"}
	if !slices.Equal(got, want) {
		t.Errorf("RecentConsole(10) = %v, want %v", got, want)
	}
}

func TestSubscribeConsole(t *testing.T) {
	inst := New(config.MinecraftConfig{})
	runReadConsole(t, inst, "a", "b", "c")

	snapshot, sub := inst.SubscribeConsole(2)
	defer sub.Cancel()

	if want := []string{"b", "c"}; !slices.Equal(snapshot, want) {
		t.Errorf("snapshot = %v, want %v", snapshot, want)
	}

	runReadConsole(t, inst, "d")
	if got := recvLine(t, sub.C); got != "d" {
		t.Errorf("first live line = %q, want %q", got, "d")
	}
}

func TestReadConsoleReadyLine(t *testing.T) {
	t.Run("ready line moves starting to running", func(t *testing.T) {
		inst := newInstanceInState(StateStarting)

		runReadConsole(t, inst, readyLine)

		if got := inst.State(); got != StateRunning {
			t.Errorf("state = %v, want %v", got, StateRunning)
		}
	})

	t.Run("ready line publishes state change then ready event", func(t *testing.T) {
		inst := newInstanceInState(StateStarting)
		sub := inst.SubscribeEvents()
		defer sub.Cancel()

		runReadConsole(t, inst, readyLine)

		first := recvEvent(t, sub.C)
		if first.Type != EventStateChanged || first.State != StateRunning {
			t.Errorf("first event = %+v, want state_changed to running", first)
		}

		second := recvEvent(t, sub.C)
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
		recvEvent(t, sub.C)
		recvEvent(t, sub.C)
		if extra := drainEvents(sub.C); len(extra) != 0 {
			t.Errorf("unexpected extra events: %v", extra)
		}
	})

	t.Run("ready line is ignored unless starting", func(t *testing.T) {
		for _, state := range []State{StateStopping, StateStopped, StateCrashed, StateRunning} {
			inst := newInstanceInState(state)
			sub := inst.SubscribeEvents()

			runReadConsole(t, inst, readyLine)

			if got := inst.State(); got != state {
				t.Errorf("state %v changed to %v, want it unchanged", state, got)
			}
			if events := drainEvents(sub.C); len(events) != 0 {
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

		if got := inst.State(); got != StateStarting {
			t.Errorf("state = %v, want %v", got, StateStarting)
		}
		if events := drainEvents(sub.C); len(events) != 0 {
			t.Errorf("published events %v, want none", events)
		}
	})

	t.Run("chat imitating the ready line does not flip state", func(t *testing.T) {
		inst := newInstanceInState(StateStarting)
		sub := inst.SubscribeEvents()
		defer sub.Cancel()

		runReadConsole(t, inst, `[20:26:34] [Server thread/INFO]: <Steve> Done (0.439s)! For help, type "help"`)

		if got := inst.State(); got != StateStarting {
			t.Errorf("state = %v, want %v", got, StateStarting)
		}

		e := recvEvent(t, sub.C)
		if e.Type != EventPlayerChat || e.Player != "Steve" {
			t.Errorf("event = %+v, want a player_chat event from Steve", e)
		}
		if extra := drainEvents(sub.C); len(extra) != 0 {
			t.Errorf("unexpected extra events: %v", extra)
		}
	})
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

		joined := recvEvent(t, sub.C)
		if joined.Type != EventPlayerJoined || joined.Player != "Steve" {
			t.Errorf("first event = %+v, want player_joined for Steve", joined)
		}

		chat := recvEvent(t, sub.C)
		if chat.Type != EventPlayerChat || chat.Player != "Steve" || chat.Message != "hi" {
			t.Errorf("second event = %+v, want player_chat from Steve saying %q", chat, "hi")
		}

		left := recvEvent(t, sub.C)
		if left.Type != EventPlayerLeft || left.Player != "Steve" {
			t.Errorf("third event = %+v, want player_left for Steve", left)
		}

		if extra := drainEvents(sub.C); len(extra) != 0 {
			t.Errorf("unexpected extra events: %v", extra)
		}
	})

	t.Run("chat imitating a join publishes chat, not a join", func(t *testing.T) {
		inst := newInstanceInState(StateRunning)
		sub := inst.SubscribeEvents()
		defer sub.Cancel()

		runReadConsole(t, inst, infoLine("<Steve> Alex joined the game"))

		got := recvEvent(t, sub.C)
		if got.Type != EventPlayerChat || got.Player != "Steve" || got.Message != "Alex joined the game" {
			t.Errorf("event = %+v, want player_chat from Steve saying %q", got, "Alex joined the game")
		}
		if extra := drainEvents(sub.C); len(extra) != 0 {
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

		warn := recvEvent(t, sub.C)
		if warn.Type != EventServerWarning || warn.Message != "Can't keep up!" {
			t.Errorf("first event = %+v, want server_warning %q", warn, "Can't keep up!")
		}

		errEvent := recvEvent(t, sub.C)
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

		if events := drainEvents(sub.C); len(events) != 0 {
			t.Errorf("published events %v, want none", events)
		}
	})
}

func TestSendRejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		command string
		state   State
		want    string // substring of the error
	}{
		{"empty", "", StateRunning, "empty"},
		{"whitespace only", "  \t ", StateRunning, "empty"},
		{"embedded newline", "say hi\nstop", StateRunning, "control character"},
		{"embedded carriage return", "say hi\rstop", StateRunning, "control character"},
		{"stopped", "list", StateStopped, "stopped"},
		{"stopping", "list", StateStopping, "stopping"},
		{"crashed", "list", StateCrashed, "crashed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := newInstanceInState(tt.state)
			stdin := &recordingWriter{}
			inst.stdin = stdin

			err := inst.Send(tt.command)
			if err == nil {
				t.Fatalf("Send(%q) in state %v returned no error", tt.command, tt.state)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Send(%q) error = %q, want it to contain %q", tt.command, err, tt.want)
			}
			if stdin.Len() != 0 {
				t.Errorf("Send(%q) wrote %q to stdin despite the error", tt.command, stdin.String())
			}
		})
	}
}

func TestSendWritesTrimmedCommand(t *testing.T) {
	// Commands are accepted while the server is still loading as well.
	for _, state := range []State{StateStarting, StateRunning} {
		t.Run(state.String(), func(t *testing.T) {
			inst := newInstanceInState(state)
			stdin := &recordingWriter{}
			inst.stdin = stdin

			err := inst.Send("  say hello world \n")
			if err != nil {
				t.Fatalf("Send() error: %v", err)
			}

			if got, want := stdin.String(), "say hello world\n"; got != want {
				t.Errorf("stdin received %q, want %q", got, want)
			}
		})
	}
}

func TestSendReturnsWriteError(t *testing.T) {
	boom := errors.New("pipe closed")

	inst := newInstanceInState(StateRunning)
	inst.stdin = failingWriter{err: boom}

	err := inst.Send("list")
	if !errors.Is(err, boom) {
		t.Errorf("Send() error = %v, want it to wrap %v", err, boom)
	}
}

func TestSendSerializesWrites(t *testing.T) {
	const senders = 50

	inst := newInstanceInState(StateRunning)
	stdin := &recordingWriter{}
	inst.stdin = stdin

	var wg sync.WaitGroup
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := inst.Send(fmt.Sprintf("say message-%d", i)); err != nil {
				t.Errorf("Send() error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	got := strings.Split(strings.TrimSuffix(stdin.String(), "\n"), "\n")

	want := make([]string, senders)
	for i := range want {
		want[i] = fmt.Sprintf("say message-%d", i)
	}

	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("stdin lines were interleaved or lost:\n got: %v\nwant: %v", got, want)
	}
}

func TestMetricsStopped(t *testing.T) {
	inst := New(config.MinecraftConfig{Directory: t.TempDir()})

	m, err := inst.Metrics()
	if err != nil {
		t.Fatalf("Metrics() error: %v", err)
	}

	if m.State != StateStopped {
		t.Errorf("State = %v, want %v", m.State, StateStopped)
	}
	if m.Uptime != 0 {
		t.Errorf("Uptime = %v, want 0", m.Uptime)
	}
	if m.PlayersOnline != 0 {
		t.Errorf("PlayersOnline = %d, want 0", m.PlayersOnline)
	}
	if m.CPUPercent != 0 || m.CPUMachinePercent != 0 || m.MemoryBytes != 0 {
		t.Errorf("process fields = cpu %v / %v, memory %d, want all zero",
			m.CPUPercent, m.CPUMachinePercent, m.MemoryBytes)
	}
	if m.CPUCores != runtime.NumCPU() {
		t.Errorf("CPUCores = %d, want %d", m.CPUCores, runtime.NumCPU())
	}
	if m.DiskTotalBytes == 0 {
		t.Errorf("DiskTotalBytes = 0, want disk usage reported even when stopped")
	}
}

func TestMetricsCountsPlayers(t *testing.T) {
	inst := New(config.MinecraftConfig{Directory: t.TempDir()})
	inst.players.add("Steve")
	inst.players.add("Alex")

	m, err := inst.Metrics()
	if err != nil {
		t.Fatalf("Metrics() error: %v", err)
	}
	if m.PlayersOnline != 2 {
		t.Errorf("PlayersOnline = %d, want 2", m.PlayersOnline)
	}
}

func TestMetricsMissingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does", "not", "exist")
	inst := New(config.MinecraftConfig{Directory: missing})

	m, err := inst.Metrics()
	if err == nil {
		t.Fatal("Metrics() returned no error for a missing directory")
	}
	if m.State != StateStopped {
		t.Errorf("State = %v, want the partly filled result to keep %v", m.State, StateStopped)
	}
}

func TestStatusStopped(t *testing.T) {
	inst := New(config.MinecraftConfig{})

	st := inst.Status()
	if st.State != StateStopped {
		t.Errorf("State = %v, want %v", st.State, StateStopped)
	}
	if st.Uptime != 0 {
		t.Errorf("Uptime = %v, want 0", st.Uptime)
	}
	if len(st.Players) != 0 {
		t.Errorf("Players = %v, want none", st.Players)
	}
}

func TestStatusRunningReportsUptimeAndPlayers(t *testing.T) {
	inst := newInstanceInState(StateRunning)
	inst.startedAt = time.Now().Add(-90 * time.Second)
	inst.players.add("Steve")
	inst.players.add("Alex")

	st := inst.Status()
	if st.Uptime < 90*time.Second || st.Uptime > 95*time.Second {
		t.Errorf("Uptime = %v, want about 90s", st.Uptime)
	}
	if want := []string{"Alex", "Steve"}; !slices.Equal(st.Players, want) {
		t.Errorf("Players = %v, want %v", st.Players, want)
	}
}

func TestStatusUptimeIsZeroUnlessRunning(t *testing.T) {
	for _, state := range []State{StateStopped, StateStarting, StateStopping, StateCrashed} {
		inst := newInstanceInState(state)
		if got := inst.Status().Uptime; got != 0 {
			t.Errorf("state %v: Uptime = %v, want 0", state, got)
		}
	}
}
