package minecraft

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"

	"github.com/emiHuy/mc-server-manager/config"
)

type Instance struct {
	mu    sync.Mutex // guards state, cmd, and stdin
	state State

	cmd   *exec.Cmd
	stdin io.WriteCloser

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

	go inst.watch(cmd)

	return nil
}

func (inst *Instance) Stop() error {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.state != StateRunning {
		return fmt.Errorf("cannot stop minecraft server when instance is %s", inst.state)
	}

	_, err := inst.stdin.Write([]byte("stop\n"))
	if err != nil {
		return fmt.Errorf("could not send stop command: %w", err)
	}

	inst.state = StateStopping
	return nil
}

func (inst *Instance) watch(cmd *exec.Cmd) {
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
	}
	inst.mu.Unlock()

	if newState == StateStopped {
		slog.Info("minecraft exited", "state", newState.String(), "error", err)
	} else {
		slog.Error("minecraft crashed", "state", newState.String(), "error", err)
	}
}
