package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"time"
)

var tailscaleRange = netip.MustParsePrefix("100.64.0.0/10")

// tailscaleAddr returns the first IPv4 address inside Tailscale's range.
func tailscaleAddr(addrs []net.Addr) (netip.Addr, bool) {
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip, ok := netip.AddrFromSlice(ipnet.IP)
		if !ok {
			continue
		}
		ip = ip.Unmap() // net.ParseIP returns the 16-byte form for IPv4
		if ip.Is4() && tailscaleRange.Contains(ip) {
			return ip, true
		}
	}
	return netip.Addr{}, false
}

func waitForTailscale(ctx context.Context, addrs func() ([]net.Addr, error), every, limit time.Duration) (netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	ticker := time.NewTicker(every)
	defer ticker.Stop()

	logged := false
	for {
		if list, err := addrs(); err == nil {
			if ip, ok := tailscaleAddr(list); ok {
				return ip, nil
			}
		}
		if !logged { // once, not on every poll
			slog.Warn("waiting for tailscale interface", "range", tailscaleRange.String(), "timeout", limit.String())
			logged = true
		}
		select {
		case <-ctx.Done():
			return netip.Addr{}, fmt.Errorf("no address in %s: %w", tailscaleRange, ctx.Err())
		case <-ticker.C:
		}
	}
}
