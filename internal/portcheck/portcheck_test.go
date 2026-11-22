package portcheck

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAvailable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := listener.Addr().String()
	require.Error(t, Available(addr))

	listener.Close()
	require.NoError(t, Available(addr))
}
