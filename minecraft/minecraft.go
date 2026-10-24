// Package minecraft manages a Minecraft server process.
// This includes starting and stopping the server, sending console commands, tracking players, publishing events, and collecting metrics.
package minecraft

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/internal/fanout"
	"github.com/emiHuy/mc-server-manager/internal/sysmetrics"
)

const (
	killTimeout               = 5 * time.Second        // wait for the exit after killing the process
	consoleDoneWait           = 2 * time.Second        // wait for console output to drain after the process exits
	metricsSampleWindow       = 250 * time.Millisecond // how long Metrics samples CPU
	defaultMetricsInterval    = 3 * time.Second
	maxLines                  = 1000 // console lines kept as history
	consoleSubscriberCapacity = 64
	eventSubscriberCapacity   = 64
	metricsSubscriberCapacity = 4
)

// Instance manages one Minecraft server process.
// Publishing to the hubs while holding mu is safe because they never call back into Instance.
type Instance struct {
	// The hubs and the player set lock themselves.
	conf    config.MinecraftConfig
	console *fanout.Hub[string]
	events  *fanout.Hub[Event]
	players *playerSet

	metrics         *fanout.Hub[Metrics]
	metricsInterval time.Duration

	// writeMu serializes writes to stdin so concurrent commands cannot interleave.
	// Lock order: mu before writeMu, never the reverse.
	writeMu sync.Mutex
	// samplerMu guards samplerRunning.
	// It is held only around subscribing and the subscriber check, never while taking mu.
	samplerMu      sync.Mutex
	samplerRunning bool

	// mu guards every field below it.
	mu        sync.Mutex
	state     State
	startedAt time.Time // zero unless the server is running
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	exited    chan struct{}
}

type Metrics struct {
	State         State
	Uptime        time.Duration
	PlayersOnline int

	CPUPercent        float64 // per core
	CPUMachinePercent float64
	CPUCores          int

	MemoryBytes uint64

	DiskUsedBytes  uint64
	DiskFreeBytes  uint64
	DiskTotalBytes uint64
}

type Status struct {
	State   State
	Uptime  time.Duration
	Players []string
}

func New(conf config.MinecraftConfig) *Instance {
	return &Instance{
		state: StateStopped,

		conf: conf,

		console: fanout.New[string]("console", consoleSubscriberCapacity, maxLines),
		events:  fanout.New[Event]("events", eventSubscriberCapacity, 0),
		players: newPlayerSet(),

		metrics:         fanout.New[Metrics]("metrics", metricsSubscriberCapacity, 1),
		metricsInterval: defaultMetricsInterval,
	}
}

// Start launches the server process and returns without waiting for the world to load.
// The state stays starting until EventServerReady.
func (inst *Instance) Start() error {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.state != StateStopped && inst.state != StateCrashed {
		return fmt.Errorf("cannot start minecraft server when instance is %w: %s", ErrInvalidState, inst.state)
	}

	inst.setStateLocked(StateStarting)
	slog.Info("minecraft server starting", "state", StateStarting)

	args := []string{
		"-Xms" + inst.conf.MinMemory,
		"-Xmx" + inst.conf.MaxMemory,
		"-jar",
		inst.conf.JarFile,
	}
	if !inst.conf.Gui {
		args = append(args, "nogui")
	}

	cmd := exec.Command(inst.conf.Command, args...)
	cmd.Dir = inst.conf.Directory

	stdin, err := cmd.StdinPipe()
	if err != nil {
		inst.setStateLocked(StateStopped)
		return fmt.Errorf("cannot get stdin pipe: %w", err)
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		inst.setStateLocked(StateStopped)
		stdin.Close()
		return fmt.Errorf("cannot create console pipe: %w", err)
	}
	cmd.Stdout = writer
	cmd.Stderr = writer

	detachFromConsole(cmd)

	err = cmd.Start()
	if err != nil {
		inst.setStateLocked(StateStopped)
		writer.Close()
		reader.Close()
		return fmt.Errorf("cannot start minecraft server: %w", err)
	}

	writer.Close()
	consoleDone := make(chan struct{})
	inst.players.clear()
	go inst.readConsole(reader, consoleDone)

	inst.cmd = cmd
	inst.stdin = stdin

	exited := make(chan struct{})
	inst.exited = exited

	go inst.watch(cmd, exited, consoleDone)

	return nil
}

// RequestStop sends the stop command and transitions the state to stopping immediately,
func (inst *Instance) RequestStop() error {
	err := inst.Send("stop")
	if err != nil {
		return fmt.Errorf("cannot stop minecraft server: %w", err)
	}

	inst.mu.Lock()
	// The state flips after the stop command is written, not atomically with it.
	// If the server exits non-zero in this tiny gap, watch records a crash.
	// Revisit before adding crash auto-restart.
	if inst.state.live() {
		inst.setStateLocked(StateStopping)
	}
	inst.mu.Unlock()

	return nil
}

// Stop calls RequestStop and then blocks to wait for the server process to exit
// If it does not exit within StopTimeout, the process is killed.
func (inst *Instance) Stop() error {
	err := inst.RequestStop()
	if err != nil {
		return err
	}

	inst.mu.Lock()
	cmd := inst.cmd
	exited := inst.exited
	inst.mu.Unlock()

	if exited == nil {
		return nil
	}

	slog.Info("minecraft server stopping", "state", StateStopping)

	select {
	case <-exited:
		return nil
	case <-time.After(inst.conf.StopTimeout):
		slog.Warn("minecraft server did not stop in time, killing process", "timeout", inst.conf.StopTimeout.String())
		err = cmd.Process.Kill()
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("cannot kill minecraft server after stop timed out: %w", err)
		}
	}

	select {
	case <-exited:
		return nil
	case <-time.After(killTimeout):
		return fmt.Errorf("cannot confirm minecraft server exited within %s of being killed", killTimeout)
	}
}

// Kill terminates the process immediately, without a world save.
func (inst *Instance) Kill() error {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.cmd == nil {
		return fmt.Errorf("cannot kill minecraft server: %w: %s", ErrInvalidState, inst.state)
	}

	prev := inst.state
	stateChanged := false
	if prev.live() {
		inst.setStateLocked(StateStopping)
		stateChanged = true
	}

	err := inst.cmd.Process.Kill()
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		if stateChanged {
			inst.setStateLocked(prev)
		}
		return fmt.Errorf("cannot kill minecraft server: %w", err)
	}

	return nil
}

// Done returns a channel that closes when the current process exits.
// If nothing is running, it returns one that is already closed.
func (inst *Instance) Done() <-chan struct{} {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.exited == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}

	return inst.exited
}

// Send writes a console command to the server.
func (inst *Instance) Send(command string) error {
	trimmedCommand := strings.TrimSpace(command)
	if trimmedCommand == "" {
		return fmt.Errorf("cannot send command: %w: empty command", ErrInvalidCommand)
	} else if strings.ContainsFunc(trimmedCommand, unicode.IsControl) {
		return fmt.Errorf("cannot send command: %w: contains control character", ErrInvalidCommand)
	}

	inst.mu.Lock()
	if !inst.state.live() {
		state := inst.state
		inst.mu.Unlock()
		return fmt.Errorf("cannot send command: %w: %s", ErrInvalidState, state)
	}
	stdin := inst.stdin
	inst.mu.Unlock()

	inst.writeMu.Lock()
	defer inst.writeMu.Unlock()

	_, err := stdin.Write([]byte(trimmedCommand + "\n"))
	if err != nil {
		return fmt.Errorf("cannot send command to minecraft server: %w", err)
	}
	return nil
}

// RecentConsole return the n most recent console lines.
func (inst *Instance) RecentConsole(n int) []string {
	return inst.console.Recent(n)
}

// Players returns a list of online player names.
func (inst *Instance) Players() []string {
	return inst.players.list()
}

// Metrics reports process and disk statistics.
// The process fields are zero when no process is running.
// It blocks for metricsSampleWindow while sampling CPU.
func (inst *Instance) Metrics() (Metrics, error) {
	return inst.collect(func(pid int32) (float64, uint64, error) {
		sampler, err := sysmetrics.NewSampler(pid)
		if err != nil {
			return 0, 0, err
		}
		return sampler.Sample(metricsSampleWindow)
	})
}

// Status returns the server status. Includes state, uptime, and online players.
func (inst *Instance) Status() Status {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	state := inst.state
	var uptime time.Duration
	if !inst.startedAt.IsZero() {
		uptime = time.Since(inst.startedAt)
	}

	return Status{State: state, Uptime: uptime, Players: inst.Players()}
}

// State reads the instance state under its lock.
func (inst *Instance) State() State {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.state
}

// SubscribeConsole returns up to n recent console lines and a subscription to new ones.
// Callers must call Cancel on the returned subscription when done.
func (inst *Instance) SubscribeConsole(n int) ([]string, *fanout.Subscription[string]) {
	return inst.console.Subscribe(n)
}

// SubscribeEvents returns a subscription to events.
// Callers must call Cancel on it when done.
func (inst *Instance) SubscribeEvents() *fanout.Subscription[Event] {
	_, sub := inst.events.Subscribe(0)
	return sub
}

// SubscribeMetrics returns the latest reading, if one is current, and a subscription to new ones.
// Sampling runs only while a subscription exists.
// Callers must call Cancel on the subscription when done.
func (inst *Instance) SubscribeMetrics() ([]Metrics, *fanout.Subscription[Metrics]) {
	inst.samplerMu.Lock()
	defer inst.samplerMu.Unlock()

	wasRunning := inst.samplerRunning
	latest, sub := inst.metrics.Subscribe(1)

	if !wasRunning {
		// The history is from the last time anyone was watching, so it is stale.
		latest = nil
		inst.samplerRunning = true
		go inst.sampleLoop()
	}

	return latest, sub
}

// watch is the only caller of cmd.Wait.
// It records how the process ended and clears the per-launch fields.
func (inst *Instance) watch(cmd *exec.Cmd, exited chan struct{}, consoleDone chan struct{}) {
	defer close(exited)

	err := cmd.Wait()

	// Let the reader finish, so the last lines are published before exited closes.
	select {
	case <-consoleDone:
	case <-time.After(consoleDoneWait):
		slog.Warn("console output did not close after exit", "timeout", consoleDoneWait.String())
	}

	inst.mu.Lock()

	stopRequested := inst.state == StateStopping
	wasStarting := inst.state == StateStarting

	inst.players.clear()

	// A requested stop is always a stop.
	// Otherwise a clean exit counts as one only after the server was ready
	if stopRequested || (err == nil && !wasStarting) {
		inst.setStateLocked(StateStopped)
	} else {
		inst.setStateLocked(StateCrashed)
	}

	// A newer launch may already own these fields.
	if inst.cmd == cmd {
		inst.cmd = nil
		inst.stdin = nil
		inst.exited = nil
	}
	inst.mu.Unlock()

	switch {
	case stopRequested:
		slog.Info("minecraft server exited", "state", StateStopped, "error", err)
	case wasStarting:
		slog.Error("minecraft server exited before becoming ready", "state", StateCrashed, "error", err)
	case err == nil:
		slog.Info("minecraft server exited on its own", "state", StateStopped)
	default:
		slog.Error("minecraft server crashed", "state", StateCrashed, "error", err)
	}
}

// markReady moves starting to running once.
// It does nothing if a stop or kill got there first.
func (inst *Instance) markReady(e Event) {
	inst.mu.Lock()
	ready := inst.state == StateStarting
	if ready {
		inst.setStateLocked(StateRunning)
	}
	inst.mu.Unlock()

	if ready {
		slog.Info("minecraft server ready", "state", StateRunning)
		inst.events.Publish(e)
	}
}

// setStateLocked changes the state and publishes the change.
// The caller must hold inst.mu.
func (inst *Instance) setStateLocked(newState State) {
	if inst.state == newState {
		return
	}

	switch newState {
	case StateRunning:
		inst.startedAt = time.Now()
	case StateStopped, StateCrashed:
		inst.startedAt = time.Time{}
	}

	inst.state = newState
	slog.Info("publishing event", "type", EventStateChanged, "new_state", newState)
	inst.events.Publish(Event{Type: EventStateChanged, State: newState, Timestamp: time.Now()})
}

// readConsole publishes each console line and acts on the events it finds.
// It handles events inline and not through a subscription, because a subscriber can drop items and these updates must not be missed.
func (inst *Instance) readConsole(r io.ReadCloser, done chan struct{}) {
	defer close(done)
	defer r.Close()

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		inst.console.Publish(line)

		event, ok := parseEvent(line)
		if !ok {
			continue
		}

		slog.Info("publishing event", "type", event.Type, "message", event.Message)

		switch event.Type {
		case EventServerReady:
			inst.markReady(event)
		case EventPlayerJoined:
			inst.players.add(event.Player)
			inst.events.Publish(event)
		case EventPlayerLeft:
			inst.players.remove(event.Player)
			inst.events.Publish(event)
		default:
			inst.events.Publish(event)
		}
	}

	err := scanner.Err()
	if err != nil {
		slog.Warn("console read failed", "error", err)
		io.Copy(io.Discard, r)
	}
}

// collect gathers current runtime metrics for the instance.
func (inst *Instance) collect(sample func(pid int32) (cpu float64, mem uint64, err error)) (Metrics, error) {
	inst.mu.Lock()
	state := inst.state
	startedAt := inst.startedAt

	var pid int32
	hasProcess := false
	if inst.cmd != nil && inst.cmd.Process != nil {
		pid = int32(inst.cmd.Process.Pid)
		hasProcess = true
	}
	inst.mu.Unlock()

	m := Metrics{
		State:         state,
		PlayersOnline: len(inst.Players()),
		CPUCores:      runtime.NumCPU(),
	}

	if !startedAt.IsZero() {
		m.Uptime = time.Since(startedAt)
	}

	if hasProcess {
		cpu, mem, err := sample(pid)
		if err == nil {
			m.CPUPercent = cpu
			m.CPUMachinePercent = min(cpu/float64(m.CPUCores), 100)
			m.MemoryBytes = mem
		}
	}

	usage, err := sysmetrics.DiskUsage(inst.conf.Directory)
	if err != nil {
		return m, err
	}
	m.DiskUsedBytes = usage.Used
	m.DiskFreeBytes = usage.Free
	m.DiskTotalBytes = usage.Total

	return m, nil
}

// sampleLoop publishes a reading every metricsInterval until nobody is subscribed.
func (inst *Instance) sampleLoop() {
	var sampler *sysmetrics.Sampler
	var samplerPID int32

	// A new sampler measures over a short window for its first reading.
	// After that it compares against the previous call, so it need not wait.
	sample := func(pid int32) (float64, uint64, error) {
		var window time.Duration
		if sampler == nil || samplerPID != pid {
			s, err := sysmetrics.NewSampler(pid)
			if err != nil {
				return 0, 0, err
			}
			sampler, samplerPID = s, pid
			window = metricsSampleWindow
		}
		return sampler.Sample(window)
	}

	ticker := time.NewTicker(inst.metricsInterval)
	defer ticker.Stop()

	for {
		m, err := inst.collect(sample)
		if err != nil {
			slog.Debug("metrics sample incomplete", "error", err)
		}
		inst.metrics.Publish(m)

		<-ticker.C

		inst.samplerMu.Lock()
		if inst.metrics.Len() == 0 {
			inst.samplerRunning = false
			inst.samplerMu.Unlock()
			return
		}
		inst.samplerMu.Unlock()
	}
}
