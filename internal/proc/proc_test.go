package proc

import (
	"errors"
	"os"
	"os/exec"
	"testing"
)

func TestLookupDeadProcess(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}

	_, err = Lookup(cmd.Process.Pid)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Lookup() error = %v, want ErrNotFound", err)
	}
}
