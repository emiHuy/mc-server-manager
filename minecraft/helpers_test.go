package minecraft

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
)

const testTimeout = time.Second

// readyLine is the console line Minecraft prints once the world has loaded.
const readyLine = `[20:26:34] [Server thread/INFO]: Done (0.439s)! For help, type "help"`

// infoLine builds a console line the way Minecraft prints INFO messages.
func infoLine(message string) string {
	return "[20:30:01] [Server thread/INFO]: " + message
}

// recvEvent reads one event from ch, failing the test if none arrives in time.
func recvEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case e, ok := <-ch:
		if !ok {
			t.Fatal("channel closed, expected an event")
		}
		return e
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for an event")
	}
	return Event{}
}

// recvLine reads one console line from ch, failing the test if none arrives in time.
func recvLine(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case line, ok := <-ch:
		if !ok {
			t.Fatal("channel closed, expected a line")
		}
		return line
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for a line")
	}
	return ""
}

// drainEvents returns the Message of every event currently queued in ch, without blocking.
func drainEvents(ch <-chan Event) []string {
	var messages []string
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return messages
			}
			messages = append(messages, e.Message)
		default:
			return messages
		}
	}
}

// runReadConsole feeds lines through readConsole and waits for it to finish.
func runReadConsole(t *testing.T, inst *Instance, lines ...string) {
	t.Helper()

	reader := io.NopCloser(strings.NewReader(strings.Join(lines, "\n") + "\n"))
	done := make(chan struct{})

	go inst.readConsole(reader, done)

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("timeout waiting for readConsole to complete")
	}
}

// newInstanceInState returns an instance with its state set directly, without publishing an event, so a test starts from a known point.
func newInstanceInState(state State) *Instance {
	inst := New(config.MinecraftConfig{})
	inst.state = state
	return inst
}
