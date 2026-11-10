package portcheck

import (
	"net"
	"testing"
)

func TestAvailable(t *testing.T) {
	listener, err := net.Listen("tcp", "")
	if err != nil {
		t.Fatal(err)
	}

	addr := listener.Addr().String()

	if err := Available(addr); err == nil {
		t.Errorf("Available(%s) = nil while port is held, want error", addr)
	}

	listener.Close()
	if err := Available(addr); err != nil {
		t.Errorf("Available(%s) after close = %v, want nil", addr, err)
	}
}
