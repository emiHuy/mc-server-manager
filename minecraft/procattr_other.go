//go:build !windows

package minecraft

import (
	"os/exec"
	"syscall"
)

func detachFromConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
