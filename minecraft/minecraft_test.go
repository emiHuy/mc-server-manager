package minecraft

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/stretchr/testify/require"
)

const readyLine = `[20:26:33] [Server thread/INFO]: Done (0.439s)! For help, type "help"`

func infoLine(msg string) string {
	return "[20:26:34] [Server thread/INFO]: " + msg
}

// newInstanceInState returns an instance that reports the given state without having launched anything or published any event.
func newInstanceInState(state State) *Instance {
	inst := New(config.MinecraftConfig{})
	inst.state = state
	return inst
}

// runReadConsole feeds the lines through readConsole and returns once it has handled all of them.
func runReadConsole(t *testing.T, inst *Instance, lines ...string) {
	t.Helper()

	done := make(chan struct{})
	input := strings.Join(lines, "\n") + "\n"
	inst.readConsole(io.NopCloser(strings.NewReader(input)), done)

	select {
	case <-done:
	default:
		t.Fatal("readConsole returned without closing done")
	}
}

func recvEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event")
		return Event{}
	}
}

// recordingWriter stands in for the server's stdin.
// It is deliberately not safe for concurrent use.
type recordingWriter struct {
	bytes.Buffer
}

func (w *recordingWriter) Close() error { return nil }

// failingWriter is a stdin whose writes always fail.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }
func (failingWriter) Close() error                { return nil }

// fakeProcess makes collect believe a process with this PID is running.
func fakeProcess(inst *Instance, pid int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.cmd = &exec.Cmd{Process: &os.Process{Pid: pid}}
	inst.state = StateRunning
	inst.startedAt = time.Now().Add(-30 * time.Second)
}

func TestInvalidStateErrors(t *testing.T) {
	tests := []struct {
		name  string
		state State
		call  func(*Instance) error
	}{
		{"Start while starting", StateStarting, (*Instance).Start},
		{"Start while running", StateRunning, (*Instance).Start},
		{"Start while stopping", StateStopping, (*Instance).Start},
		{"RequestStop while stopped", StateStopped, (*Instance).RequestStop},
		{"RequestStop while stopping", StateStopping, (*Instance).RequestStop},
		{"RequestStop while crashed", StateCrashed, (*Instance).RequestStop},
		{"Restart while stopping", StateStopping, (*Instance).Restart},
		{"Kill without a process", StateStopped, (*Instance).Kill},
		{"Send while stopped", StateStopped, func(i *Instance) error { return i.Send("list") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := newInstanceInState(tt.state)
			require.ErrorIs(t, tt.call(inst), ErrInvalidState)
			require.Equal(t, tt.state, inst.State())
		})
	}
}

func TestStartFailureLeavesInstanceStopped(t *testing.T) {
	inst := New(config.MinecraftConfig{Directory: t.TempDir()})
	require.Error(t, inst.Start())
	require.Equal(t, StateStopped, inst.State())
}

func TestRequestStopWritesStop(t *testing.T) {
	inst := newInstanceInState(StateRunning)
	stdin := &recordingWriter{}
	inst.stdin = stdin

	require.NoError(t, inst.RequestStop())
	require.Equal(t, "stop\n", stdin.String())
	require.Equal(t, StateStopping, inst.State())
}

func TestRequestStopRollsBackWhenWriteFails(t *testing.T) {
	boom := errors.New("pipe closed")
	inst := newInstanceInState(StateRunning)
	inst.stdin = failingWriter{err: boom}

	require.ErrorIs(t, inst.RequestStop(), boom)
	require.Equal(t, StateRunning, inst.State())
}

func TestSendWritesTrimmedCommand(t *testing.T) {
	inst := newInstanceInState(StateRunning)
	stdin := &recordingWriter{}
	inst.stdin = stdin

	require.NoError(t, inst.Send("  /say hello world \n"))
	require.Equal(t, "say hello world\n", stdin.String())
}

func TestSendRejectsBadCommands(t *testing.T) {
	for _, command := range []string{"", "  ", "/", "say hi\nstop", "say hi\rstop"} {
		inst := newInstanceInState(StateRunning)
		stdin := &recordingWriter{}
		inst.stdin = stdin

		require.ErrorIs(t, inst.Send(command), ErrInvalidCommand)
		require.Equal(t, 0, stdin.Len())
	}
}

func TestSendSerializesWrites(t *testing.T) {
	const senders = 50

	inst := newInstanceInState(StateRunning)
	stdin := &recordingWriter{}
	inst.stdin = stdin

	var wg sync.WaitGroup
	for i := range senders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			require.NoError(t, inst.Send(fmt.Sprintf("say message-%d", i)))
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
	require.Equal(t, want, got)
}

func TestStatusReportsUptimeAndPlayers(t *testing.T) {
	inst := newInstanceInState(StateRunning)
	inst.startedAt = time.Now().Add(-90 * time.Second)
	inst.players.add("Steve")
	inst.players.add("Alex")

	st := inst.Status()
	require.GreaterOrEqual(t, st.Uptime, 90*time.Second)
	require.LessOrEqual(t, st.Uptime, 95*time.Second)
	require.Equal(t, []string{"Alex", "Steve"}, st.Players)
}

func TestMetricsStopped(t *testing.T) {
	inst := New(config.MinecraftConfig{Directory: t.TempDir()})

	m, err := inst.Metrics()
	require.NoError(t, err)
	require.Equal(t, StateStopped, m.State)
	require.Equal(t, float64(0), m.CPUPercent)
	require.Equal(t, uint64(0), m.MemoryBytes)
	require.Greater(t, m.DiskTotalBytes, uint64(0))
}

func TestPublishEventTimestamp(t *testing.T) {
	inst := New(config.MinecraftConfig{})
	sub := inst.SubscribeEvents()
	defer sub.Cancel()

	before := time.Now()
	inst.PublishEvent(Event{Type: EventServerWarning})

	got := recvEvent(t, sub.C)
	require.NotZero(t, got.Timestamp)
	require.False(t, got.Timestamp.Before(before))

	given := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	inst.PublishEvent(Event{Type: EventServerWarning, Timestamp: given})
	require.Equal(t, given, recvEvent(t, sub.C).Timestamp)
}

func TestReadConsoleReadyLine(t *testing.T) {
	t.Run("moves starting to running, publishing the state change then the ready event", func(t *testing.T) {
		inst := newInstanceInState(StateStarting)
		sub := inst.SubscribeEvents()
		defer sub.Cancel()

		runReadConsole(t, inst, readyLine)
		require.Equal(t, StateRunning, inst.State())

		first := recvEvent(t, sub.C)
		require.Equal(t, EventStateChanged, first.Type)
		require.Equal(t, StateRunning, first.State)

		require.Equal(t, EventServerReady, recvEvent(t, sub.C).Type)
	})

	t.Run("is ignored unless starting", func(t *testing.T) {
		for _, state := range []State{StateStopping, StateStopped, StateCrashed, StateRunning} {
			inst := newInstanceInState(state)
			runReadConsole(t, inst, readyLine)
			require.Equal(t, state, inst.State())
		}
	})

	t.Run("chat imitating the ready line does not flip state", func(t *testing.T) {
		inst := newInstanceInState(StateStarting)
		sub := inst.SubscribeEvents()
		defer sub.Cancel()

		runReadConsole(t, inst, infoLine(`<Steve> Done (0.439s)! For help, type "help"`))
		require.Equal(t, StateStarting, inst.State())

		e := recvEvent(t, sub.C)
		require.Equal(t, EventPlayerChat, e.Type)
		require.Equal(t, "Steve", e.Player)
	})
}

func TestReadConsolePlayers(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  []string
	}{
		{
			name:  "two joins are listed sorted",
			lines: []string{infoLine("Steve joined the game"), infoLine("Alex joined the game")},
			want:  []string{"Alex", "Steve"},
		},
		{
			name:  "join then leave",
			lines: []string{infoLine("Steve joined the game"), infoLine("Steve left the game")},
			want:  []string{},
		},
		{
			name:  "duplicate join keeps one entry",
			lines: []string{infoLine("Steve joined the game"), infoLine("Steve joined the game")},
			want:  []string{"Steve"},
		},
		{
			name:  "chat imitating a join adds nobody",
			lines: []string{infoLine("<Steve> Alex joined the game")},
			want:  []string{},
		},
		{
			name:  "chat imitating a leave removes nobody",
			lines: []string{infoLine("Alex joined the game"), infoLine("<Steve> Alex left the game")},
			want:  []string{"Alex"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := newInstanceInState(StateRunning)
			runReadConsole(t, inst, tt.lines...)
			require.Equal(t, tt.want, inst.Players())
		})
	}
}

func TestReadConsolePublishesPlayerEvents(t *testing.T) {
	inst := newInstanceInState(StateRunning)
	sub := inst.SubscribeEvents()
	defer sub.Cancel()

	runReadConsole(
		t, inst,
		infoLine("Steve joined the game"),
		infoLine("<Steve> hi"),
		infoLine("Steve left the game"),
	)

	for _, want := range []EventType{EventPlayerJoined, EventPlayerChat, EventPlayerLeft} {
		got := recvEvent(t, sub.C)
		require.Equal(t, want, got.Type)
		require.Equal(t, "Steve", got.Player)
	}
}

func TestSetStateLockedStartedAt(t *testing.T) {
	inst := New(config.MinecraftConfig{})
	set := func(s State) {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		inst.setStateLocked(s)
	}
	startedAt := func() time.Time {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		return inst.startedAt
	}

	set(StateStarting)
	require.Zero(t, startedAt())

	set(StateRunning)
	first := startedAt()
	require.NotZero(t, first)

	// A stop that fails goes back to running. That must not restart the uptime clock.
	time.Sleep(2 * time.Millisecond)
	set(StateStopping)
	set(StateRunning)
	require.Equal(t, first, startedAt())

	set(StateStopped)
	require.Zero(t, startedAt())
}

func TestCollect(t *testing.T) {
	t.Run("no process skips the sampler", func(t *testing.T) {
		inst := New(config.MinecraftConfig{Directory: t.TempDir()})

		called := false
		_, err := inst.collect(func(int32) (float64, uint64, error) {
			called = true
			return 100, 1 << 20, nil
		})
		require.NoError(t, err)
		require.False(t, called)
	})

	t.Run("uses the sampled readings", func(t *testing.T) {
		inst := New(config.MinecraftConfig{Directory: t.TempDir()})
		fakeProcess(inst, 4242)

		var gotPID int32
		m, err := inst.collect(func(pid int32) (float64, uint64, error) {
			gotPID = pid
			return 150, 1 << 20, nil
		})
		require.NoError(t, err)

		require.Equal(t, int32(4242), gotPID)
		require.Equal(t, float64(150), m.CPUPercent)
		require.Equal(t, uint64(1<<20), m.MemoryBytes)
		require.GreaterOrEqual(t, m.Uptime, 30*time.Second)
	})

	t.Run("a failed sample keeps the rest of the reading", func(t *testing.T) {
		inst := New(config.MinecraftConfig{Directory: t.TempDir()})
		fakeProcess(inst, 4242)

		m, err := inst.collect(func(int32) (float64, uint64, error) {
			return 99, 99, errors.New("process vanished")
		})
		require.NoError(t, err)

		require.Equal(t, float64(0), m.CPUPercent)
		require.Equal(t, uint64(0), m.MemoryBytes)
		require.Greater(t, m.DiskTotalBytes, uint64(0))
	})
}

// Sampling can block for a while, so collect must not hold mu during it.
func TestCollectDoesNotHoldLockWhileSampling(t *testing.T) {
	inst := New(config.MinecraftConfig{Directory: t.TempDir()})
	fakeProcess(inst, 4242)

	_, err := inst.collect(func(int32) (float64, uint64, error) {
		require.True(t, inst.mu.TryLock())
		inst.mu.Unlock()
		return 0, 0, nil
	})
	require.NoError(t, err)
}
