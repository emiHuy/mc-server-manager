package minecraft

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultServerIP   = ""
	defaultServerPort = 25565
)

func serverAddrFromProperties(dir string) (string, int, error) {
	host := defaultServerIP
	port := defaultServerPort

	data, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if errors.Is(err, os.ErrNotExist) {
		return defaultServerIP, defaultServerPort, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("cannot read server.properties: %w", err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch key {
		case "server-ip":
			if value == "" {
				continue
			}
			_, err := netip.ParseAddr(value)
			if err != nil {
				return "", 0, fmt.Errorf("invalid server-ip %q in server.properties", value)
			}
			host = value
		case "server-port":
			if value == "" {
				continue
			}
			p, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || p < 1 || p > 65535 {
				return "", 0, fmt.Errorf("invalid server-port %d in server.properties", p)
			}
			port = p
		}
	}

	return host, port, nil
}
