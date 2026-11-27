package minecraft

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/stretchr/testify/require"
)

// These tests launch a real process, so they are skipped with go test -short.
// The process is the test binary itself acting as a Minecraft server, see TestMain.

const (
	fakeServerEnv  = "MCSM_FAKE_SERVER"
	fakeReadyLine  = `[20:26:33] [Server thread/INFO]: Done (0.439s)! For help, type "help"`
	fakeServerWait = 10 * time.Second
)

// TestMain lets the test binary double as a fake Minecraft server.
func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeServerEnv); mode != "" {
		runFakeServer(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runFakeServer behaves like a server, depending on mode:
//
//	normal              becomes ready, exits 0 when it reads "stop"
//	crash-after-ready   becomes ready, then exits 1
//	exit0-before-ready  exits 0 before it is ready
//	hang                never becomes ready
func runFakeServer(mode string) {
	switch mode {
	case "exit0-before-ready":
		os.Exit(0)
	case "hang":
		time.Sleep(time.Hour)
	}

	fmt.Println(fakeReadyLine)

	if mode == "crash-after-ready" {
		time.Sleep(100 * time.Millisecond)
		os.Exit(1)
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if scanner.Text() == "stop" {
			os.Exit(0)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "fake server stdin scan error: %v\n", err)
		os.Exit(1)
	}
}

// newFakeServer returns an instance whose server is the test binary acting out the given mode.
// Any process it launched is killed when the test ends.
func newFakeServer(t *testing.T, mode string, tweak ...func(*config.MinecraftConfig)) *Instance {
	t.Helper()
	if testing.Short() {
		t.Skip("starts a real process")
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeServerEnv, mode)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	dir := t.TempDir()
	props := fmt.Sprintf("server-ip=127.0.0.1\nserver-port=%d\n", port)
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(props), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.MinecraftConfig{
		Command:        exe,
		Directory:      dir,
		JarFile:        "server.jar",
		MinMemory:      "1M",
		MaxMemory:      "1M",
		StartupTimeout: fakeServerWait,
		StopTimeout:    fakeServerWait,
	}
	for _, f := range tweak {
		f(&cfg)
	}

	inst := New(cfg)
	t.Cleanup(func() {
		inst.Kill()
		select {
		case <-inst.Done():
		case <-time.After(fakeServerWait):
		}
	})
	return inst
}

func waitForState(t *testing.T, inst *Instance, want State) {
	t.Helper()
	deadline := time.Now().Add(fakeServerWait)
	for time.Now().Before(deadline) {
		if inst.State() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for state %v, still %v", want, inst.State())
}

func waitDone(t *testing.T, inst *Instance) {
	t.Helper()
	select {
	case <-inst.Done():
	case <-time.After(fakeServerWait):
		t.Fatal("timed out waiting for the server process to exit")
	}
}

// crashHook returns a hook for SetOnCrash and a function that reports whether it ran.
// The hook runs before Done closes, so it can be checked as soon as waitDone returns.
func crashHook() (hook func(time.Duration), ranFor func() (time.Duration, bool)) {
	ch := make(chan time.Duration, 1)
	hook = func(d time.Duration) { ch <- d }
	ranFor = func() (time.Duration, bool) {
		select {
		case d := <-ch:
			return d, true
		default:
			return 0, false
		}
	}
	return hook, ranFor
}

func TestStartStopRoundTrip(t *testing.T) {
	inst := newFakeServer(t, "normal")
	hook, crashed := crashHook()
	inst.SetOnCrash(hook)

	require.NoError(t, inst.Start())
	waitForState(t, inst, StateRunning)

	_, err := os.Stat(pidfilePath(inst.cfg.Directory))
	require.NoError(t, err)

	inst.players.add("Steve")
	require.NoError(t, inst.Stop())

	require.Equal(t, StateStopped, inst.State())
	require.Len(t, inst.Players(), 0)

	_, err = os.Stat(pidfilePath(inst.cfg.Directory))
	require.ErrorIs(t, err, os.ErrNotExist)

	_, ran := crashed()
	require.False(t, ran)
}

func TestCrashAfterReadyRunsHook(t *testing.T) {
	inst := newFakeServer(t, "crash-after-ready")
	hook, crashed := crashHook()
	inst.SetOnCrash(hook)

	require.NoError(t, inst.Start())
	waitDone(t, inst)

	require.Equal(t, StateCrashed, inst.State())

	d, ran := crashed()
	require.True(t, ran)
	require.Greater(t, d, time.Duration(0))
}

func TestExitBeforeReadyIsACrash(t *testing.T) {
	inst := newFakeServer(t, "exit0-before-ready")
	hook, crashed := crashHook()
	inst.SetOnCrash(hook)

	require.NoError(t, inst.Start())
	waitDone(t, inst)

	require.Equal(t, StateCrashed, inst.State())

	d, ran := crashed()
	require.True(t, ran)
	require.Equal(t, time.Duration(0), d)
}

func TestKillDuringStartupIsNotACrash(t *testing.T) {
	inst := newFakeServer(t, "hang")
	hook, crashed := crashHook()
	inst.SetOnCrash(hook)

	require.NoError(t, inst.Start())
	require.NoError(t, inst.Kill())
	waitDone(t, inst)

	require.Equal(t, StateStopped, inst.State())

	_, ran := crashed()
	require.False(t, ran)
}

func TestStartupTimeoutKillsServer(t *testing.T) {
	inst := newFakeServer(t, "hang", func(c *config.MinecraftConfig) {
		c.StartupTimeout = 300 * time.Millisecond
	})
	hook, crashed := crashHook()
	inst.SetOnCrash(hook)

	require.NoError(t, inst.Start())
	waitDone(t, inst)

	require.Equal(t, StateCrashed, inst.State())

	_, ran := crashed()
	require.True(t, ran)
}
