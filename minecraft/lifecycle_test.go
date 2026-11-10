package minecraft

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/internal/fanout"
	"github.com/emiHuy/mc-server-manager/internal/pidfile"
)

// These tests run the real Start, Stop, and Kill code against a fake server.
// The test binary re-runs itself as the fake: when fakeServerEnv is set,
// TestMain behaves like Minecraft and never runs any tests.

const (
	fakeServerEnv    = "MCSM_FAKE_SERVER"
	lifecycleTimeout = 10 * time.Second
	fakeExitDelay    = 50 * time.Millisecond // keeps the stop-to-exit gap wider than Stop's state flip
)

// Behaviors of the fake server, chosen through fakeServerEnv.
const (
	modeNormal          = "normal"            // becomes ready, exits 0 on "stop"
	modeNeverReady      = "never-ready"       // never prints the ready line, exits 0 on "stop"
	modeIgnoreStop      = "ignore-stop"       // becomes ready, ignores "stop" until killed
	modeExitBeforeReady = "exit-before-ready" // exits 0 while still loading
	modeExitAfterReady  = "exit-after-ready"  // becomes ready, then exits 0 by itself
	modeCrash           = "crash"             // becomes ready, then exits 1
	modePlayers         = "players"           // becomes ready, Steve joins, exits 0 on "stop"
	modeExitOnStop      = "exit-on-stop"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeServerEnv); mode != "" {
		runFakeServer(mode)
		return
	}
	os.Exit(m.Run())
}

// runFakeServer imitates the parts of Minecraft's console that Instance cares
// about. It never returns.
func runFakeServer(mode string) {
	say := func(message string) {
		fmt.Printf("[12:00:00] [Server thread/INFO]: %s\n", message)
	}
	ready := func() { say(`Done (0.1s)! For help, type "help"`) }

	say("Loading properties")

	switch mode {
	case modeExitBeforeReady:
		os.Exit(0)
	case modeExitAfterReady:
		ready()
		os.Exit(0)
	case modeCrash:
		ready()
		os.Exit(1)
	case modeIgnoreStop:
		ready()
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(1)
	case modeNeverReady:
		// No ready line.
	case modePlayers:
		ready()
		say("Steve joined the game")
	case modeExitOnStop:
		ready()
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == "stop" {
				say("Stopping server")
				os.Exit(1)
			}
		}
		scanner.Err()
		// Stdin closed without a stop.
		os.Exit(1)
	default:
		ready()
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "stop" {
			say("Stopping server")
			time.Sleep(fakeExitDelay)
			os.Exit(0)
		}
	}

	scanner.Err()

	// Stdin closed without a stop, for example because the test crashed.
	os.Exit(1)
}

// newFakeInstance returns an instance that launches the fake server.
// Any process still running when the test ends is killed.
func newFakeInstance(t *testing.T, mode string, stopTimeout time.Duration) *Instance {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("cannot find the test binary: %v", err)
	}

	t.Setenv(fakeServerEnv, mode)

	dir := t.TempDir()
	writeServerProperties(t, dir, fmt.Sprintf("server-port=%d\n", freePort(t)))

	inst := New(config.MinecraftConfig{
		Directory:      dir,
		Command:        exe,
		JarFile:        "server.jar",
		MinMemory:      "1M",
		MaxMemory:      "1M",
		StopTimeout:    stopTimeout,
		StartupTimeout: lifecycleTimeout,
	})

	t.Cleanup(func() {
		_ = inst.Kill()
		select {
		case <-inst.Done():
		case <-time.After(lifecycleTimeout):
			t.Error("fake server did not exit during cleanup")
		}
	})

	return inst
}

// startFake starts a fake server, subscribing to events first so none are missed.
func startFake(t *testing.T, mode string, stopTimeout time.Duration) (*Instance, *fanout.Subscription[Event]) {
	t.Helper()

	inst := newFakeInstance(t, mode, stopTimeout)

	sub := inst.SubscribeEvents()
	t.Cleanup(sub.Cancel)

	err := inst.Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	return inst, sub
}

// waitForState reads events until the instance reports want.
// It returns every state change seen on the way, ending with want.
func waitForState(t *testing.T, sub *fanout.Subscription[Event], want State) []State {
	t.Helper()

	var seen []State
	deadline := time.After(lifecycleTimeout)
	for {
		select {
		case e, ok := <-sub.C:
			if !ok {
				t.Fatalf("event channel closed while waiting for state %v", want)
			}
			if e.Type == EventStateChanged {
				seen = append(seen, e.State)
				if e.State == want {
					return seen
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for state %v, saw %v", want, seen)
		}
	}
}

// waitForEvent reads events until one of the given type arrives.
func waitForEvent(t *testing.T, sub *fanout.Subscription[Event], typ EventType) Event {
	t.Helper()

	deadline := time.After(lifecycleTimeout)
	for {
		select {
		case e, ok := <-sub.C:
			if !ok {
				t.Fatalf("event channel closed while waiting for %v", typ)
			}
			if e.Type == typ {
				return e
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a %v event", typ)
		}
	}
}

// drainStates returns the state changes currently queued in ch, without blocking.
func drainStates(ch <-chan Event) []State {
	var states []State
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return states
			}
			if e.Type == EventStateChanged {
				states = append(states, e.State)
			}
		default:
			return states
		}
	}
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func freePort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("cannot find a free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func writeServerProperties(t *testing.T, dir, contents string) {
	t.Helper()

	path := filepath.Join(dir, "server.properties")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("cannot write server.properties: %v", err)
	}
}

func TestStartPortTaken(t *testing.T) {
	inst := newFakeInstance(t, modeNormal, lifecycleTimeout)
	sub := inst.SubscribeEvents()
	t.Cleanup(sub.Cancel)

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	port := ln.Addr().(*net.TCPAddr).Port
	writeServerProperties(t, inst.conf.Directory, fmt.Sprintf("server-port=%d\n", port))

	err = inst.Start()
	if err == nil {
		t.Fatal("Start() = nil with the port taken, want error")
	}
	if !errors.Is(err, ErrPreflight) {
		t.Errorf("Start() error = %v, want ErrPreflight", err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Errorf("Start() error = %q, want it to mention port %d", err, port)
	}
	if got := drainStates(sub.C); len(got) != 0 {
		t.Errorf("states = %v, want none", got)
	}
	if s := inst.State(); s != StateStopped {
		t.Errorf("state = %v, want %v", s, StateStopped)
	}
}

func TestStartBadServerProperties(t *testing.T) {
	inst := newFakeInstance(t, modeNormal, lifecycleTimeout)
	sub := inst.SubscribeEvents()
	t.Cleanup(sub.Cancel)

	writeServerProperties(t, inst.conf.Directory, "server-port=abc\n")

	err := inst.Start()
	if !errors.Is(err, ErrPreflight) {
		t.Fatalf("Start() error = %v, want ErrPreflight", err)
	}
	if got := drainStates(sub.C); len(got) != 0 {
		t.Errorf("states = %v, want none", got)
	}
}

func TestStartMissingServerPropertiesUsesDefault(t *testing.T) {
	// With no server.properties the check targets 25565. Hold it so the result
	// does not depend on whether something else on the machine already has it.
	ln, err := net.Listen("tcp", ":25565")
	if err != nil {
		t.Skipf("cannot bind 25565 to run this test: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	inst := newFakeInstance(t, modeNormal, lifecycleTimeout)
	if err := os.Remove(filepath.Join(inst.conf.Directory, "server.properties")); err != nil {
		t.Fatal(err)
	}

	err = inst.Start()
	if !errors.Is(err, ErrPreflight) || !strings.Contains(err.Error(), "25565") {
		t.Errorf("Start() error = %v, want ErrPreflight mentioning 25565", err)
	}
}

func TestLifecycleStartAndStop(t *testing.T) {
	inst, sub := startFake(t, modeNormal, 5*time.Second)

	seen := waitForState(t, sub, StateRunning)
	if want := []State{StateStarting, StateRunning}; !slices.Equal(seen, want) {
		t.Errorf("states before stop = %v, want %v", seen, want)
	}

	// The fake server's output reaches the console history.
	foundReady := false
	for _, line := range inst.RecentConsole(10) {
		if strings.Contains(line, "Done (") {
			foundReady = true
		}
	}
	if !foundReady {
		t.Errorf("console history %v has no ready line", inst.RecentConsole(10))
	}

	err := inst.Stop()
	if err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if got := inst.State(); got != StateStopped {
		t.Errorf("state after Stop() = %v, want %v", got, StateStopped)
	}
	if want, got := []State{StateStopping, StateStopped}, drainStates(sub.C); !slices.Equal(got, want) {
		t.Errorf("states after stop = %v, want %v", got, want)
	}
	if !isClosed(inst.Done()) {
		t.Error("Done() is not closed after Stop()")
	}
}

func TestLifecycleStartRefusedWhileRunning(t *testing.T) {
	inst, sub := startFake(t, modeNormal, 5*time.Second)
	waitForState(t, sub, StateRunning)

	err := inst.Start()
	if err == nil {
		t.Fatal("second Start() returned no error")
	}
	if !errors.Is(err, ErrInvalidState) {
		t.Errorf("second Start() error = %v, want it to wrap %v", err, ErrInvalidState)
	}
	if !strings.Contains(err.Error(), "running") {
		t.Errorf("second Start() error = %q, want it to mention the running state", err)
	}
	if got := inst.State(); got != StateRunning {
		t.Errorf("state after refused Start() = %v, want %v", got, StateRunning)
	}
}

func TestLifecycleStopTimeoutKillsProcess(t *testing.T) {
	const timeout = 300 * time.Millisecond

	inst, sub := startFake(t, modeIgnoreStop, timeout)
	waitForState(t, sub, StateRunning)

	began := time.Now()
	err := inst.Stop()
	if err != nil {
		t.Fatalf("Stop() error: %v, want nil after the kill", err)
	}

	if elapsed := time.Since(began); elapsed < timeout {
		t.Errorf("Stop() returned after %v, before the %v timeout", elapsed, timeout)
	}
	if got := inst.State(); got != StateStopped {
		t.Errorf("state after forced stop = %v, want %v", got, StateStopped)
	}
}

func TestLifecycleStopWhileStarting(t *testing.T) {
	inst, sub := startFake(t, modeNeverReady, 5*time.Second)

	if got := inst.State(); got != StateStarting {
		t.Fatalf("state = %v, want %v", got, StateStarting)
	}

	err := inst.Stop()
	if err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if got := inst.State(); got != StateStopped {
		t.Errorf("state after Stop() = %v, want %v", got, StateStopped)
	}
	if want, got := []State{StateStarting, StateStopping, StateStopped}, drainStates(sub.C); !slices.Equal(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
}

func TestLifecycleKill(t *testing.T) {
	inst, sub := startFake(t, modeIgnoreStop, 5*time.Second)
	waitForState(t, sub, StateRunning)

	done := inst.Done()

	err := inst.Kill()
	if err != nil {
		t.Fatalf("Kill() error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(lifecycleTimeout):
		t.Fatal("process did not exit after Kill()")
	}

	// An intentional kill is recorded as a stop, not a crash.
	if got := inst.State(); got != StateStopped {
		t.Errorf("state after Kill() = %v, want %v", got, StateStopped)
	}
	if want, got := []State{StateStopping, StateStopped}, drainStates(sub.C); !slices.Equal(got, want) {
		t.Errorf("states after kill = %v, want %v", got, want)
	}
}

func TestLifecycleExitClassification(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want []State
	}{
		{
			name: "exit while loading is a crash",
			mode: modeExitBeforeReady,
			want: []State{StateStarting, StateCrashed},
		},
		{
			name: "clean exit after ready is a stop",
			mode: modeExitAfterReady,
			want: []State{StateStarting, StateRunning, StateStopped},
		},
		{
			name: "non-zero exit after ready is a crash",
			mode: modeCrash,
			want: []State{StateStarting, StateRunning, StateCrashed},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst, sub := startFake(t, tt.mode, 5*time.Second)

			final := tt.want[len(tt.want)-1]
			seen := waitForState(t, sub, final)

			if !slices.Equal(seen, tt.want) {
				t.Errorf("states = %v, want %v", seen, tt.want)
			}
			if got := inst.State(); got != final {
				t.Errorf("final state = %v, want %v", got, final)
			}
		})
	}
}

func TestLifecycleStopWhenNothingRunning(t *testing.T) {
	inst := New(config.MinecraftConfig{})

	err := inst.Stop()
	if err == nil {
		t.Fatal("Stop() on a stopped instance returned no error")
	}
	if !errors.Is(err, ErrInvalidState) {
		t.Errorf("Stop() error = %v, want it to wrap %v", err, ErrInvalidState)
	}
	if !strings.Contains(err.Error(), "stopped") {
		t.Errorf("Stop() error = %q, want it to mention the stopped state", err)
	}
}

func TestLifecycleKillWhenNothingRunning(t *testing.T) {
	inst := New(config.MinecraftConfig{})

	err := inst.Kill()
	if err == nil {
		t.Fatal("Kill() on a stopped instance returned no error")
	}
	if !errors.Is(err, ErrInvalidState) {
		t.Errorf("Stop() error = %v, want it to wrap %v", err, ErrInvalidState)
	}
	if !strings.Contains(err.Error(), "stopped") {
		t.Errorf("Kill() error = %q, want it to mention the stopped state", err)
	}
}

func TestLifecycleDone(t *testing.T) {
	inst := newFakeInstance(t, modeNormal, 5*time.Second)

	if !isClosed(inst.Done()) {
		t.Error("Done() before Start() should be already closed")
	}

	err := inst.Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	done := inst.Done()
	if isClosed(done) {
		t.Error("Done() is closed while the server is running")
	}

	err = inst.Stop()
	if err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if !isClosed(done) {
		t.Error("the channel from Done() is not closed after Stop()")
	}
	if !isClosed(inst.Done()) {
		t.Error("Done() after the exit should be already closed")
	}
}

func TestLifecycleRestart(t *testing.T) {
	inst, sub := startFake(t, modeNormal, 5*time.Second)
	waitForState(t, sub, StateRunning)

	err := inst.Stop()
	if err != nil {
		t.Fatalf("first Stop() error: %v", err)
	}
	drainStates(sub.C)

	err = inst.Start()
	if err != nil {
		t.Fatalf("second Start() error: %v", err)
	}

	seen := waitForState(t, sub, StateRunning)
	if want := []State{StateStarting, StateRunning}; !slices.Equal(seen, want) {
		t.Errorf("states on second run = %v, want %v", seen, want)
	}
	if isClosed(inst.Done()) {
		t.Error("Done() is closed during the second run")
	}

	err = inst.Stop()
	if err != nil {
		t.Fatalf("second Stop() error: %v", err)
	}
}

func TestLifecycleStartAfterCrash(t *testing.T) {
	inst, sub := startFake(t, modeCrash, 5*time.Second)
	waitForState(t, sub, StateCrashed)
	drainStates(sub.C)

	// The next launch behaves normally.
	t.Setenv(fakeServerEnv, modeNormal)

	err := inst.Start()
	if err != nil {
		t.Fatalf("Start() after a crash error: %v", err)
	}

	seen := waitForState(t, sub, StateRunning)
	if want := []State{StateStarting, StateRunning}; !slices.Equal(seen, want) {
		t.Errorf("states after restart = %v, want %v", seen, want)
	}
}

func TestLifecyclePlayersAreClearedOnExit(t *testing.T) {
	inst, sub := startFake(t, modePlayers, 5*time.Second)

	joined := waitForEvent(t, sub, EventPlayerJoined)
	if joined.Player != "Steve" {
		t.Fatalf("joined player = %q, want %q", joined.Player, "Steve")
	}
	if want, got := []string{"Steve"}, inst.Players(); !slices.Equal(got, want) {
		t.Errorf("Players() while running = %v, want %v", got, want)
	}

	err := inst.Stop()
	if err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if got := inst.Players(); len(got) != 0 {
		t.Errorf("Players() after exit = %v, want none", got)
	}
}

func TestLifecycleMetrics(t *testing.T) {
	inst, sub := startFake(t, modeNormal, 5*time.Second)
	waitForState(t, sub, StateRunning)

	m, err := inst.Metrics()
	if err != nil {
		t.Fatalf("Metrics() while running error: %v", err)
	}
	if m.State != StateRunning {
		t.Errorf("State = %v, want %v", m.State, StateRunning)
	}
	if m.MemoryBytes == 0 {
		t.Error("MemoryBytes = 0 while a process is running")
	}
	if m.Uptime < 0 {
		t.Errorf("Uptime = %v, want 0 or more", m.Uptime)
	}
	if m.CPUCores <= 0 {
		t.Errorf("CPUCores = %d, want a positive number", m.CPUCores)
	}

	err = inst.Stop()
	if err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	m, err = inst.Metrics()
	if err != nil {
		t.Fatalf("Metrics() after exit error: %v", err)
	}
	if m.State != StateStopped {
		t.Errorf("State after exit = %v, want %v", m.State, StateStopped)
	}
	if m.MemoryBytes != 0 || m.Uptime != 0 {
		t.Errorf("process fields after exit = memory %d, uptime %v, want zero", m.MemoryBytes, m.Uptime)
	}
}

func TestRequestStopNonZeroExitIsStopped(t *testing.T) {
	inst, sub := startFake(t, modeExitOnStop, lifecycleTimeout)
	waitForState(t, sub, StateRunning)

	err := inst.RequestStop()
	if err != nil {
		t.Fatalf("RequestStop() error: %v", err)
	}

	got := waitForState(t, sub, StateStopped)
	want := []State{StateStopping, StateStopped}
	if !slices.Equal(got, want) {
		t.Errorf("states after RequestStop() = %v, want %v", got, want)
	}

	select {
	case <-inst.Done():
	case <-time.After(lifecycleTimeout):
		t.Error("instance did not finish after the server exited")
	}
}

func TestStartupTimeoutKillsStuckServer(t *testing.T) {
	inst := newFakeInstance(t, modeNeverReady, lifecycleTimeout)
	inst.conf.StartupTimeout = 200 * time.Millisecond // adjust if conf isn't a field by this name

	sub := inst.SubscribeEvents()
	t.Cleanup(sub.Cancel)

	err := inst.Start()
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	got := waitForState(t, sub, StateCrashed)
	want := []State{StateStarting, StateCrashed}
	if !slices.Equal(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
}

func TestRestartFromRunning(t *testing.T) {
	inst, sub := startFake(t, modeNormal, lifecycleTimeout)
	waitForState(t, sub, StateRunning)

	err := inst.Restart()
	if err != nil {
		t.Fatalf("Restart() error: %v", err)
	}

	got := waitForState(t, sub, StateRunning)
	want := []State{StateStopping, StateStopped, StateStarting, StateRunning}
	if !slices.Equal(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
}

func TestRestartFromStopped(t *testing.T) {
	inst := newFakeInstance(t, modeNormal, lifecycleTimeout)
	sub := inst.SubscribeEvents()
	t.Cleanup(sub.Cancel)

	err := inst.Restart()
	if err != nil {
		t.Fatalf("Restart() error: %v", err)
	}

	got := waitForState(t, sub, StateRunning)
	want := []State{StateStarting, StateRunning}
	if !slices.Equal(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
}

func TestRestartFromCrashed(t *testing.T) {
	inst, sub := startFake(t, modeCrash, lifecycleTimeout)
	waitForState(t, sub, StateCrashed)

	err := inst.Restart()
	if err != nil {
		t.Fatalf("Restart() error: %v", err)
	}

	// modeCrash crashes again, so only check that the restart launched.
	got := waitForState(t, sub, StateStarting)
	want := []State{StateStarting}
	if !slices.Equal(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
}

func TestRestartWhileStopping(t *testing.T) {
	inst, sub := startFake(t, modeIgnoreStop, lifecycleTimeout)
	waitForState(t, sub, StateRunning)

	err := inst.RequestStop()
	if err != nil {
		t.Fatalf("RequestStop() error: %v", err)
	}

	err = inst.Restart()
	if !errors.Is(err, ErrInvalidState) {
		t.Errorf("Restart() error = %v, want ErrInvalidState", err)
	}
}

func TestPidfileWrittenAndRemoved(t *testing.T) {
	inst, sub := startFake(t, modeNormal, lifecycleTimeout)
	waitForState(t, sub, StateRunning)

	info, err := pidfile.Read(inst.pidfilePath())
	if err != nil {
		t.Fatalf("pidfile.Read() error while running: %v", err)
	}
	if info.PID <= 0 {
		t.Errorf("pid = %d, want a positive pid", info.PID)
	}
	if time.Since(info.StartedAt) > lifecycleTimeout {
		t.Errorf("started_at = %v, want a recent time", info.StartedAt)
	}

	if err := inst.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
	_, err = pidfile.Read(inst.pidfilePath())
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("pidfile after stop: error = %v, want ErrNotExist", err)
	}
}

func TestPidfileRemovedAfterCrash(t *testing.T) {
	inst, sub := startFake(t, modeCrash, lifecycleTimeout)
	waitForState(t, sub, StateCrashed)

	select {
	case <-inst.Done():
	case <-time.After(lifecycleTimeout):
		t.Fatal("instance did not finish after the crash")
	}

	_, err := pidfile.Read(inst.pidfilePath())
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("pidfile after crash: error = %v, want ErrNotExist", err)
	}
}
