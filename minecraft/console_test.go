package minecraft

import (
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
)

func TestReadConsoleFillsBuffer(t *testing.T) {
	inst := New(config.MinecraftConfig{})

	reader := io.NopCloser(strings.NewReader("a\nb\nc\n"))
	done := make(chan struct{})

	go inst.readConsole(reader, done)

	select {
	case <-done:
		// readConsole finished
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for readConsole to complete")
	}

	got := inst.RecentConsole(10)
	want := []string{"a", "b", "c"}

	if !slices.Equal(got, want) {
		t.Errorf("RecentConsole(10) mismatch:\n got: %v\nwant: %v", got, want)
	}
}
