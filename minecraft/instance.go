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

const killTimeout = 5 * time.Second

type Instance struct {
	mu    sync.Mutex // guards state, cmd, stdin, and exited
	state State

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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		inst.state = StateStopped
		return fmt.Errorf("cannot get stdin pipe: %w", err)
	}

	err = cmd.Start()
	if err != nil {
		inst.state = StateStopped
		return fmt.Errorf("cannot start minecraft server: %w", err)
	}

	inst.state = StateRunning
	inst.cmd = cmd
	inst.stdin = stdin

	exited := make(chan struct{})
	inst.exited = exited

	go inst.watch(cmd, exited)

	return nil
}

func (inst *Instance) Stop() error {
	cmd, exited, err := inst.beginStop()
	if err != nil {
		return err
	}
	slog.Info("stopping minecraft server", "state", StateStopping.String())

	select {
	case <-exited:
		return nil
	case <-time.After(inst.conf.StopTimeout):
		slog.Warn("minecraft did not stop in time, killing process", "timeout", inst.conf.StopTimeout)
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

func (inst *Instance) watch(cmd *exec.Cmd, exited chan struct{}) {
	defer close(exited)
	err := cmd.Wait()
	inst.mu.Lock()

	if inst.state == StateStopping {
		inst.state = StateStopped
	} else {
		inst.state = StateCrashed
	}
	newState := inst.state

	if inst.cmd == cmd {
		inst.cmd = nil
		inst.stdin = nil
		inst.exited = nil
	}
	inst.mu.Unlock()

	if newState == StateStopped {
		slog.Info("minecraft exited", "state", StateStopped.String(), "error", err)
	} else {
		slog.Error("minecraft crashed", "state", StateCrashed.String(), "error", err)
	}
}

func (inst *Instance) beginStop() (*exec.Cmd, <-chan struct{}, error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.state != StateRunning {
		return nil, nil, fmt.Errorf("cannot stop minecraft server when instance is %s", inst.state)
	}

	_, err := inst.stdin.Write([]byte("stop\n"))
	if err != nil {
		return nil, nil, fmt.Errorf("cannot send stop command: %w", err)
	}

	inst.state = StateStopping
	return inst.cmd, inst.exited, nil
}
