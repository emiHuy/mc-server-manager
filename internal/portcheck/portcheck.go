// Package portcheck reports whether a TCP address can be listened on.
package portcheck

import "net"

// Available returns an error if the port cannot be listened on.
func Available(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return listener.Close()
}
