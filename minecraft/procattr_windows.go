package minecraft

import (
	"os/exec"
	"syscall"
)

func detachFromConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
