package minecraft

import (
	"fmt"
	"strings"
	"unicode"
)

const maxCommandLength = 1000 // bytes

func normalizeCommand(command string) (string, error) {
	cmd := strings.TrimSpace(command)
	cmd = strings.TrimSpace(strings.TrimPrefix(cmd, "/"))

	switch {
	case cmd == "":
		return "", fmt.Errorf("%w: empty command", ErrInvalidCommand)
	case len(cmd) > maxCommandLength:
		return "", fmt.Errorf("%w: command longer than %d bytes", ErrInvalidCommand, maxCommandLength)
	case strings.ContainsFunc(cmd, unicode.IsControl):
		return "", fmt.Errorf("%w: contains control character", ErrInvalidCommand)
	}
	return cmd, nil
}
