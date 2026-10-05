package minecraft

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
)

const (
	killTimeout     = 5 * time.Second
	consoleDoneWait = 2 * time.Second
)

type Instance struct {
	mu    sync.Mutex // guards state, cmd, stdin, and exited
	state State

	writeMu sync.Mutex // serializes writes to stdin

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	exited chan struct{}

	conf config.MinecraftConfig
}

func New(conf config.MinecraftConfig) *Instance {
	return &Instance{state: StateStopped, conf: conf}
}

func (inst *Instance) Start() error {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.state != StateStopped && inst.state != StateCrashed {
		return fmt.Errorf("cannot start minecraft server when instance is %s", inst.state)
	}

	inst.state = StateStarting
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
		inst.state = StateStopped
		return fmt.Errorf("cannot get stdin pipe: %w", err)
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		inst.state = StateStopped
		stdin.Close()
		return fmt.Errorf("cannot create pipe: %w", err)
	}
	cmd.Stdout = writer
	cmd.Stderr = writer

	detachFromConsole(cmd)

	err = cmd.Start()
	if err != nil {
		inst.state = StateStopped
		writer.Close()
		reader.Close()
		return fmt.Errorf("cannot start minecraft server: %w", err)
	}

	writer.Close()
	consoleDone := make(chan struct{})
	go inst.readConsole(reader, consoleDone)

	inst.state = StateRunning
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
		return fmt.Errorf("minecraft server stop failed: %w", err)
	}

	inst.mu.Lock()
	if inst.state == StateRunning {
		inst.state = StateStopping
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

	stateChanged := false
	if inst.state == StateRunning {
		inst.state = StateStopping
		stateChanged = true
	}

	err := inst.cmd.Process.Kill()
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		if stateChanged {
			inst.state = StateRunning
		}
		return fmt.Errorf("cannot kill minecraft server: %w", err)
	}

	return nil
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

	if stopRequested || err == nil {
		inst.state = StateStopped
	} else {
		inst.state = StateCrashed
	}

	if inst.cmd == cmd {
		inst.cmd = nil
		inst.stdin = nil
		inst.exited = nil
	}
	inst.mu.Unlock()

	if stopRequested {
		slog.Info("minecraft server exited", "state", StateStopped.String(), "error", err)
	} else if err == nil {
		slog.Info("minecraft server exited on its own", "state", StateStopped.String())
	} else {
		slog.Error("minecraft server crashed", "state", StateCrashed.String(), "error", err)
	}
}
