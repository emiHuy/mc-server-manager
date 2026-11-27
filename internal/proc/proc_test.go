package proc

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLookupDeadProcess(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)

	cmd := exec.Command(exe, "-test.run=^$")
	require.NoError(t, cmd.Run())

	_, err = Lookup(cmd.Process.Pid)
	require.ErrorIs(t, err, ErrNotFound)
}
