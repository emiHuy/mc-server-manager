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

	timestamp := time.Now()

	if level == "WARN" {
		return Event{Type: EventServerWarning, Timestamp: timestamp, Message: message}, true
	} else if level == "ERROR" {
		return Event{Type: EventServerError, Timestamp: timestamp, Message: message}, true
	} else if serverReady(level, message) {
		return Event{Type: EventServerReady, Timestamp: timestamp, Message: message}, true
	} else if player, ok := playerJoined(level, message); ok {
		return Event{Type: EventPlayerJoined, Timestamp: timestamp, Player: player, Message: message}, true
	} else if player, ok := playerLeft(level, message); ok {
		return Event{Type: EventPlayerLeft, Timestamp: timestamp, Player: player, Message: message}, true
	} else if player, chat, ok := playerChat(level, message); ok {
		return Event{Type: EventPlayerChat, Timestamp: timestamp, Player: player, Message: chat}, true
	}
	return Event{}, false
}

func serverReady(level, message string) bool {
	return level == "INFO" && strings.HasPrefix(message, "Done (") && strings.HasSuffix(message, `! For help, type "help"`)
}

func playerJoined(level, message string) (string, bool) {
	if level != "INFO" {
		return "", false
	}
	return playerName(message, " joined the game")
}

func playerLeft(level, message string) (string, bool) {
	if level != "INFO" {
		return "", false
	}
	return playerName(message, " left the game")
}

func playerChat(level, message string) (string, string, bool) {
	if level != "INFO" {
		return "", "", false
	}

	rest, ok := strings.CutPrefix(message, "<")
	if !ok {
		return "", "", false
	}

	player, chat, ok := strings.Cut(rest, "> ")
	if !ok || player == "" || strings.ContainsAny(player, " <>") {
		return "", "", false
	}

	return player, chat, true
}

func playerName(message, suffix string) (string, bool) {
	name, ok := strings.CutSuffix(message, suffix)
	if !ok || name == "" || strings.ContainsAny(name, " <>") {
		return "", false
	}
	return name, true
}
