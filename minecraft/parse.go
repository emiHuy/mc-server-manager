package minecraft

import (
	"strings"
	"time"
)

func parseLine(line string) (level, message string, ok bool) {
	if !strings.HasPrefix(line, "[") {
		return "", "", false
	}

	slash := strings.Index(line, "/")
	if slash == -1 {
		return "", "", false
	}

	end := strings.Index(line[slash:], "]")
	if end == -1 {
		return "", "", false
	}

	level = line[slash+1 : slash+end]
	message, ok = strings.CutPrefix(line[slash+end+1:], ": ")
	if !ok {
		return "", "", false
	}

	return level, message, true
}

func parseEvent(line string) (Event, bool) {
	level, message, ok := parseLine(line)
	if !ok {
		return Event{}, false
	}

	if serverReady(level, message) {
		return Event{Type: EventServerReady, Timestamp: time.Now(), Message: message}, true
	}
	return Event{}, false
}

func serverReady(level, message string) bool {
	return level == "INFO" && strings.HasPrefix(message, "Done (") && strings.HasSuffix(message, `! For help, type "help"`)
}
