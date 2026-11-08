package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/emiHuy/mc-server-manager/internal/apitypes"
)

const (
	defaultAddr    = "http://127.0.0.1:8080"
	quickTimeout   = 15 * time.Second
	stopTimeout    = 2 * time.Minute
	startTimeout   = 5 * time.Minute
	restartTimeout = stopTimeout + 10*time.Second
)

const usageText = `usage: mcsm [--addr URL] [--token TOKEN] <command>

commands:
	status               state, uptime, and plaers
	metrics              CPU, memory, and disk usage
	players              list online players
	start [--wait]       start the server (--wait blocks until it is running)
	stop                 stop gracefully (blocks until server has exited)
	kill                 kill immediately, without a world save
	command <text...>    send a console command
	logs [-n N] [-f]     recent console lines; -f follows live output

environment:
	MCSM_ADDR            manager address (default ` + defaultAddr + `)
	MCSM_TOKEN           API token (preferred over --token, which shows in ps)
`

type usageError string

func (e usageError) Error() string {
	return string(e)
}

type apiError struct {
	status int
	msg    string
}

func (e *apiError) Error() string {
	return e.msg
}

type client struct {
	base  string
	token string
}

func main() {
	addr := flag.String("addr", envOr("MCSM_ADDR", defaultAddr), "manager address")
	token := flag.String("token", os.Getenv("MCSM_TOKEN"), "API token (prefer MCSM_TOKEN)")

	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	c := &client{base: strings.TrimRight(*addr, "/"), token: *token}
	err := run(ctx, c, args[0], args[1:])
	stop()

	var ue usageError
	switch {
	case err == nil:
	case errors.As(err, &ue):
		fmt.Fprintln(os.Stderr, "mcsm:", err)
		usage()
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "mcsm:", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	v := os.Getenv(key)
	if v != "" {
		return v
	}
	return def
}

func usage() {
	fmt.Fprint(os.Stderr, usageText)
}

func run(ctx context.Context, c *client, cmd string, args []string) error {
	switch cmd {
	case "status":
		return c.status(ctx)
	case "metrics":
		return c.metrics(ctx)
	case "players":
		return c.players(ctx)
	case "start":
		return c.start(ctx, args)
	case "stop":
		return c.action(ctx, "stop", stopTimeout)
	case "kill":
		return c.action(ctx, "kill", quickTimeout)
	case "restart":
		return c.action(ctx, "restart", restartTimeout)
	case "command":
		if len(args) == 0 {
			return usageError("command needs some text")
		}
		return c.command(ctx, strings.Join(args, " "))
	case "logs":
		return c.logs(ctx, args)
	case "help":
		usage()
		return nil
	default:
		return usageError("unknown command " + cmd)
	}
}

func (c *client) setAuth(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", `Bearer `+c.token)
	}
}

// do sends one request and decodes a JSON response into out (if non-nil).
func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setAuth(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", c.base, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return readAPIError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.UnmarshalRead(resp.Body, out)
}

func readAPIError(resp *http.Response) error {
	var e struct {
		Error string `json:"error"`
	}
	json.UnmarshalRead(resp.Body, &e)
	if e.Error == "" {
		e.Error = resp.Status
	}
	return &apiError{status: resp.StatusCode, msg: e.Error}
}

func (c *client) status(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, quickTimeout)
	defer cancel()

	var s apitypes.StatusResponse
	if err := c.do(ctx, http.MethodGet, "/api/server/status", nil, &s); err != nil {
		return err
	}
	fmt.Printf("State:   %s\n", s.State)
	fmt.Printf("Uptime:  %s\n", seconds(s.UptimeSeconds))
	if len(s.Players) == 0 {
		fmt.Println("Players: none")
	} else {
		fmt.Printf("Players: %d (%s)\n", len(s.Players), strings.Join(s.Players, ", "))
	}
	return nil
}

func (c *client) metrics(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, quickTimeout)
	defer cancel()

	var m apitypes.MetricsResponse
	if err := c.do(ctx, http.MethodGet, "/api/server/metrics", nil, &m); err != nil {
		return err
	}
	fmt.Printf("State:   %s\n", m.State)
	fmt.Printf("Uptime:  %s\n", seconds(m.UptimeSeconds))
	fmt.Printf("Players: %d\n", m.PlayersOnline)
	fmt.Printf("CPU:     %.1f%% of machine (%.1f%% of one core, %d cores)\n", m.CPUMachinePercent, m.CPUPercent, m.CPUCores)
	fmt.Printf("Memory:  %s\n", humanBytes(m.MemoryBytes))
	fmt.Printf("Disk:    %s used, %s free, %s total\n", humanBytes(m.DiskUsedBytes), humanBytes(m.DiskFreeBytes), humanBytes(m.DiskTotalBytes))
	return nil
}

func (c *client) players(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, quickTimeout)
	defer cancel()

	var s apitypes.StatusResponse
	if err := c.do(ctx, http.MethodGet, "/api/server/status", nil, &s); err != nil {
		return err
	}
	if len(s.Players) == 0 {
		fmt.Println("no players online")
		return nil
	}
	fmt.Println(strings.Join(s.Players, "\n"))
	return nil
}

func (c *client) start(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	wait := fs.Bool("wait", false, "wait until the server is running")
	fs.Parse(args)

	if err := c.action(ctx, "start", quickTimeout); err != nil {
		return err
	}
	if !*wait {
		return nil
	}
	return c.waitRunning(ctx)
}

// action POSTs to /api/server/<name> and prints the resulting state.
func (c *client) action(ctx context.Context, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var res apitypes.StateResponse
	if err := c.do(ctx, http.MethodPost, "/api/server/"+name, nil, &res); err != nil {
		return err
	}
	fmt.Println(res.State)
	return nil
}

func (c *client) waitRunning(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("gave up waiting for the server to run: %w", ctx.Err())
		case <-ticker.C:
		}

		pollCtx, pollCancel := context.WithTimeout(ctx, quickTimeout)
		var s apitypes.StatusResponse
		err := c.do(pollCtx, http.MethodGet, "/api/server/status", nil, &s)
		pollCancel()
		if err != nil {
			return err
		}

		switch s.State {
		case "running":
			fmt.Println("running")
			return nil
		case "crashed", "stopped":
			return fmt.Errorf("server is %s", s.State)
		}
	}
}

func (c *client) command(ctx context.Context, text string) error {
	ctx, cancel := context.WithTimeout(ctx, quickTimeout)
	defer cancel()

	body := map[string]string{"command": text}
	return c.do(ctx, http.MethodPost, "/api/server/command", body, nil)
}

func (c *client) logs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	n := fs.Int("n", 100, "number of recent lines (1-1000)")
	follow := fs.Bool("f", false, "follow live output")
	fs.Parse(args)

	if !*follow {
		ctx, cancel := context.WithTimeout(ctx, quickTimeout)
		defer cancel()

		var out struct {
			Lines []string `json:"lines"`
		}
		path := fmt.Sprintf("/api/server/console?lines=%d", *n)
		if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
			return err
		}
		for _, line := range out.Lines {
			fmt.Println(line)
		}
		return nil
	}

	// A stream has no overall timeout; Ctrl+C cancels ctx and ends it.
	url := fmt.Sprintf("%s/api/server/console/stream?lines=%d", c.base, *n)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("cannot reach %s: %w", c.base, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return readAPIError(resp)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue // blank separators, comments, other fields
		}
		var msg struct {
			Line string `json:"line"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(data)), &msg) == nil {
			fmt.Println(msg.Line)
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func seconds(s int64) string {
	return (time.Duration(s) * time.Second).String()
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
