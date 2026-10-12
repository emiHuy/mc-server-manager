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

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/internal/fanout"
	"github.com/emiHuy/mc-server-manager/internal/sysmetrics"
)

const (
	killTimeout               = 5 * time.Second
	consoleDoneWait           = 2 * time.Second
	maxLines                  = 1000
	consoleSubscriberCapacity = 64
	eventSubscriberCapacity   = 64
	metricsSampleWindow       = 250 * time.Millisecond
)

type Instance struct {
	mu    sync.Mutex // guards state, cmd, stdin, exited, startAt
	state State

	writeMu sync.Mutex // serializes writes to stdin

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	exited chan struct{}

	console *fanout.Hub[string]
	events  *fanout.Hub[Event]
	players *playerSet

	startedAt time.Time

	conf config.MinecraftConfig
}

type Metrics struct {
	State         State
	Uptime        time.Duration
	PlayersOnline int

	CPUPercent        float64
	CPUMachinePercent float64
	CPUCores          int

	MemoryBytes uint64

	DiskUsedBytes  uint64
	DiskFreeBytes  uint64
	DiskTotalBytes uint64
}

func New(conf config.MinecraftConfig) *Instance {
	return &Instance{
		state:   StateStopped,
		conf:    conf,
		console: fanout.New[string]("console", consoleSubscriberCapacity, maxLines),
		events:  fanout.New[Event]("events", eventSubscriberCapacity, 0),
		players: newPlayerSet(),
	}
}

func (inst *Instance) Start() error {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.state != StateStopped && inst.state != StateCrashed {
		return fmt.Errorf("cannot start minecraft server when instance is %s", inst.state)
	}

	inst.setStateLocked(StateStarting)
	slog.Info("starting minecraft server", "state", StateStarting.String())

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
		return fmt.Errorf("cannot create pipe: %w", err)
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

func (inst *Instance) Stop() error {
	err := inst.Send("stop")
	if err != nil {
		return err
	}

	inst.mu.Lock()
	// The state flips after the stop command is written, not atomically with it.
	// If the server exits non-zero in this tiny gap, watch records a crash.
	// Revisit before adding crash auto-restart.
	if inst.state.live() {
		inst.setStateLocked(StateStopping)
	}
	cmd := inst.cmd
	exited := inst.exited
	inst.mu.Unlock()

	if exited == nil {
		return nil
	}

	slog.Info("stopping minecraft server", "state", StateStopping.String())

	select {
	case <-exited:
		return nil
	case <-time.After(inst.conf.StopTimeout):
		slog.Warn("minecraft did not stop in time, killing process", "timeout", inst.conf.StopTimeout.String())
		err = cmd.Process.Kill()
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("cannot kill minecraft server after stop timed out: %w", err)
		}
	}

	select {
	case <-exited:
		return nil
	case <-time.After(killTimeout):
		return fmt.Errorf("minecraft server did not exit within %s of being killed", killTimeout)
	}
}

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

func (inst *Instance) Kill() error {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.cmd == nil {
		return fmt.Errorf("cannot kill minecraft server when instance is %s", inst.state)
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

func (inst *Instance) Subscribe(n int) ([]string, *fanout.Subscription[string]) {
	return inst.console.Subscribe(n)
}

// Callers must call Cancel on the returned subscription when done
func (inst *Instance) SubscribeEvents() *fanout.Subscription[Event] {
	_, sub := inst.events.Subscribe(0)
	return sub
}

func (inst *Instance) Send(command string) error {
	trimmedCommand := strings.TrimSpace(command)
	if trimmedCommand == "" {
		return errors.New("cannot send empty command")
	} else if strings.Contains(trimmedCommand, "\n") || strings.Contains(trimmedCommand, "\r") {
		return errors.New("command cannot include \\n or \\r")
	}

	inst.mu.Lock()
	if !inst.state.live() {
		state := inst.state
		inst.mu.Unlock()
		return fmt.Errorf("cannot send command when instance is %s", state)
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

func (inst *Instance) RecentConsole(n int) []string {
	return inst.console.Recent(n)
}

func (inst *Instance) Players() []string {
	return inst.players.list()
}

func (inst *Instance) Metrics() (Metrics, error) {
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
		sampler, err := sysmetrics.NewSampler(pid)
		if err == nil {
			cpu, mem, err := sampler.Sample(metricsSampleWindow)
			if err == nil {
				m.CPUPercent = cpu
				m.CPUMachinePercent = min(cpu/float64(m.CPUCores), 100)
				m.MemoryBytes = mem
			}
		}
	}

	usage, err := sysmetrics.DiskUsage(inst.conf.Directory)
	if err != nil {
		return m, fmt.Errorf("cannot read disk usage: %w", err)
	}
	m.DiskUsedBytes = usage.Used
	m.DiskFreeBytes = usage.Free
	m.DiskTotalBytes = usage.Total

	return m, nil
}

func (inst *Instance) watch(cmd *exec.Cmd, exited chan struct{}, consoleDone chan struct{}) {
	defer close(exited)

	err := cmd.Wait()

	select {
	case <-consoleDone:
	case <-time.After(consoleDoneWait):
		slog.Warn("console output did not close after exit, continuing", "timeout", consoleDoneWait.String())
	}

	inst.mu.Lock()

	stopRequested := inst.state == StateStopping
	wasStarting := inst.state == StateStarting

	inst.players.clear()

	if stopRequested || (err == nil && !wasStarting) {
		inst.setStateLocked(StateStopped)
	} else {
		inst.setStateLocked(StateCrashed)
	}

	if inst.cmd == cmd {
		inst.cmd = nil
		inst.stdin = nil
		inst.exited = nil
	}
	inst.mu.Unlock()

	switch {
	case stopRequested:
		slog.Info("minecraft server exited", "state", StateStopped.String(), "error", err)
	case wasStarting:
		slog.Error("minecraft server exited before becoming ready", "state", StateCrashed.String(), "error", err)
	case err == nil:
		slog.Info("minecraft server exited on its own", "state", StateStopped.String())
	default:
		slog.Error("minecraft server crashed", "state", StateCrashed.String(), "error", err)
	}
}

func (inst *Instance) markReady(e Event) {
	inst.mu.Lock()
	ready := inst.state == StateStarting
	if ready {
		inst.setStateLocked(StateRunning)
	}
	inst.mu.Unlock()

	if ready {
		slog.Info("minecraft server ready", "state", StateRunning.String())
		inst.events.Publish(e)
	}
}

// setStateLocked changes the state. The caller must hold inst.mu.
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
	inst.events.Publish(Event{Type: EventStateChanged, State: newState, Timestamp: time.Now()})
}

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

		fmt.Println(inst.Players())
	}

	err := scanner.Err()
	if err != nil {
		slog.Error("console read failed", "error", err)
		io.Copy(io.Discard, r)
	}
}
