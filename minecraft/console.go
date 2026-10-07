package minecraft

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

func (inst *Instance) Send(command string) error {
	trimmedCommand := strings.TrimSpace(command)
	if trimmedCommand == "" {
		return errors.New("cannot send empty command")
	} else if strings.Contains(trimmedCommand, "\n") || strings.Contains(trimmedCommand, "\r") {
		return errors.New("command cannot include \\n or \\r")
	}

	inst.mu.Lock()
	if !inst.state.live() {
		state := inst.state
		inst.mu.Unlock()
		return fmt.Errorf("cannot send command when instance is %s", state)
	}
	stdin := inst.stdin
	inst.mu.Unlock()

	inst.writeMu.Lock()
	defer inst.writeMu.Unlock()

	_, err := stdin.Write([]byte(trimmedCommand + "\n"))
	if err != nil {
		return fmt.Errorf("cannot send command to minecraft server: %w", err)
	}
	return nil
}

func (inst *Instance) RecentConsole(n int) []string {
	return inst.console.recent(n)
}

func (inst *Instance) Subscribe(n int) ([]string, *Subscription) {
	return inst.console.subscribe(n)
}

func (inst *Instance) readConsole(r io.ReadCloser, done chan struct{}) {
	defer close(done)
	defer r.Close()

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		inst.console.add(line)

		event, ok := parseEvent(line)
		if ok && event.Type == EventServerReady {
			inst.markReady()
		}
	}

	err := scanner.Err()
	if err != nil {
		slog.Error("console read failed", "error", err)
		io.Copy(io.Discard, r)
	}
}
