package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/minecraft"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// baseURL is a placeholder host. httptest.NewTestServer uses an in-memory
// network, and its client sends every request to the server whatever the host.
const baseURL = "http://example.com"

// newTestServer serves the API's real handler (s.httpServer.Handler), so any
// middleware wrapped around the mux is covered too. The zero APIConfig is fine
// because Addr is never used. The server is shut down automatically.
func newTestServer(t *testing.T, inst *minecraft.Instance) *httptest.Server {
	t.Helper()

	s := New(inst, config.APIConfig{})
	return httptest.NewTestServer(t, s.httpServer.Handler)
}

// newStoppedInstance returns an instance that has never been started.
func newStoppedInstance() *minecraft.Instance {
	return minecraft.New(config.MinecraftConfig{})
}

// newStoppedInstanceWithDir is a stopped instance whose directory exists, which
// metrics need because they report disk usage for it.
func newStoppedInstanceWithDir(t *testing.T) *minecraft.Instance {
	t.Helper()
	return minecraft.New(config.MinecraftConfig{Directory: t.TempDir()})
}

// newFailingLaunchInstance returns an instance whose Start fails because the
// java command does not exist. A failed start still publishes real state events
// (starting, then stopped), so it exercises the API without needing Java.
func newFailingLaunchInstance(t *testing.T) *minecraft.Instance {
	t.Helper()

	return minecraft.New(config.MinecraftConfig{
		Directory: t.TempDir(),
		Command:   filepath.Join(t.TempDir(), "no-such-java"),
		JarFile:   "server.jar",
		MinMemory: "1M",
		MaxMemory: "1M",
	})
}

func get(t *testing.T, server *httptest.Server, path string) *http.Response {
	t.Helper()

	resp, err := server.Client().Get(baseURL + path)
	if err != nil {
		t.Fatalf("GET %s error: %v", path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func post(t *testing.T, server *httptest.Server, path, contentType, body string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("cannot build POST %s: %v", path, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("POST %s error: %v", path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// readJSON reads the whole body and decodes it into T. json.Unmarshal rejects
// trailing data, so a handler that writes two JSON documents (for example a
// missing return after writeError) fails here. It also returns the raw body
// for tests that check exact text such as "players":[].
func readJSON[T any](t *testing.T, resp *http.Response) (T, string) {
	t.Helper()

	var v T
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("cannot read body: %v", err)
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("cannot unmarshal body %q: %v", raw, err)
	}
	return v, string(raw)
}

// Wire formats the tests expect. They are declared here, not taken from the
// package's response types, so the tests check the JSON a client actually sees.

type statusBody struct {
	State         string   `json:"state"`
	UptimeSeconds int64    `json:"uptime_seconds"`
	Players       []string `json:"players"`
}

type consoleBody struct {
	Lines []string `json:"lines"`
}

type metricsBody struct {
	State          string  `json:"state"`
	UptimeSeconds  int64   `json:"uptime_seconds"`
	PlayersOnline  int     `json:"players_online"`
	CPUPercent     float64 `json:"cpu_percent"`
	CPUCores       int     `json:"cpu_cores"`
	MemoryBytes    uint64  `json:"memory_bytes"`
	DiskTotalBytes uint64  `json:"disk_total_bytes"`
}

type eventBody struct {
	Type    string `json:"type"`
	Player  string `json:"player"`
	Message string `json:"message"`
	State   string `json:"state"`
}

// ---------------------------------------------------------------------------
// Server construction
// ---------------------------------------------------------------------------

func TestNewSetsReadHeaderTimeout(t *testing.T) {
	s := New(newStoppedInstance(), config.APIConfig{})

	if s.httpServer.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout is not set; slow clients can hold connections open")
	}
	if s.httpServer.WriteTimeout != 0 {
		t.Error("WriteTimeout is set; it would cut off every SSE stream")
	}
}

func TestNewBuildsAddress(t *testing.T) {
	tests := []struct {
		name string
		conf config.APIConfig
		want string
	}{
		{"ipv4", config.APIConfig{Host: "127.0.0.1", Port: 8080}, "127.0.0.1:8080"},
		{"ipv6 is bracketed", config.APIConfig{Host: "::1", Port: 8080}, "[::1]:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(newStoppedInstance(), tt.conf)
			if s.httpServer.Addr != tt.want {
				t.Errorf("Addr = %q, want %q", s.httpServer.Addr, tt.want)
			}
		})
	}
}

// TestShutdownEndsStreams needs the BaseContext and RegisterOnShutdown change
// in New. Without it, Shutdown waits on the open stream and hits its deadline.
// It uses a real listener because httptest cannot set BaseContext.
func TestShutdownEndsStreams(t *testing.T) {
	s := New(newStoppedInstance(), config.APIConfig{})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go s.httpServer.Serve(ln)

	resp, err := http.Get("http://" + ln.Addr().String() + "/api/server/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	newSSEReader(resp.Body).next(t) // the stream is live

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error: %v, want open streams to end so it can finish", err)
	}
}

// ---------------------------------------------------------------------------
// Routing
// ---------------------------------------------------------------------------

func TestWrongMethod(t *testing.T) {
	server := newTestServer(t, newStoppedInstance())

	tests := []struct {
		name      string
		method    string
		path      string
		wantAllow string
	}{
		{"post to status", http.MethodPost, "/api/server/status", "GET"},
		{"delete on status", http.MethodDelete, "/api/server/status", "GET"},
		{"get on start", http.MethodGet, "/api/server/start", "POST"},
		{"get on stop", http.MethodGet, "/api/server/stop", "POST"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, baseURL+tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
			}
			if got := resp.Header.Get("Allow"); !strings.Contains(got, tt.wantAllow) {
				t.Errorf("Allow = %q, want it to include %q", got, tt.wantAllow)
			}

			body, _ := readJSON[errorResponse](t, resp)
			if body.Error != "method not allowed" {
				t.Errorf("error = %q, want %q", body.Error, "method not allowed")
			}
		})
	}
}

func TestUnknownRouteIsNotFound(t *testing.T) {
	server := newTestServer(t, newStoppedInstance())

	for _, path := range []string{"/api/nope", "/", "/api/server/status/extra"} {
		t.Run(path, func(t *testing.T) {
			resp := get(t, server, path)

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusNotFound)
			}
			if got := resp.Header.Get("Allow"); got != "" {
				t.Errorf("Allow = %q, want none on a 404", got)
			}

			body, _ := readJSON[errorResponse](t, resp)
			if body.Error != "not found" {
				t.Errorf("error = %q, want %q", body.Error, "not found")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

func TestStatusFor(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"invalid state", minecraft.ErrInvalidState, http.StatusConflict},
		{"wrapped invalid state", fmt.Errorf("cannot start: %w: running", minecraft.ErrInvalidState), http.StatusConflict},
		{"invalid command", minecraft.ErrInvalidCommand, http.StatusBadRequest},
		{"wrapped invalid command", fmt.Errorf("cannot send: %w: empty", minecraft.ErrInvalidCommand), http.StatusBadRequest},
		{"method not allowed", errMethodNotAllowed, http.StatusMethodNotAllowed},
		{"not found", errNotFound, http.StatusNotFound},
		{"wrapped api error", fmt.Errorf("x: %w", errNotFound), http.StatusNotFound},
		{"unknown error", errors.New("boom"), http.StatusInternalServerError},
		{"wrapped unknown error", fmt.Errorf("wrapped: %w", errors.New("boom")), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statusFor(tt.err); got != tt.want {
				t.Errorf("statusFor(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestWriteError(t *testing.T) {
	t.Run("client errors show their message", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/server/start", nil)

		writeError(rec, req, fmt.Errorf("cannot start: %w: running", minecraft.ErrInvalidState))

		if rec.Code != http.StatusConflict {
			t.Errorf("status code = %d, want %d", rec.Code, http.StatusConflict)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		var body errorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("cannot unmarshal %q: %v", rec.Body, err)
		}
		if !strings.Contains(body.Error, "running") {
			t.Errorf("error = %q, want it to mention the state", body.Error)
		}
	})

	t.Run("internal errors are hidden from the client", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/server/status", nil)

		writeError(rec, req, errors.New(`open C:\secret\path: access denied`))

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status code = %d, want %d", rec.Code, http.StatusInternalServerError)
		}

		var body errorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("cannot unmarshal %q: %v", rec.Body, err)
		}
		if body.Error != "internal error" {
			t.Errorf("error = %q, want %q", body.Error, "internal error")
		}
		if strings.Contains(rec.Body.String(), "secret") {
			t.Error("response leaked the underlying error text")
		}
	})
}

// ---------------------------------------------------------------------------
// Status, start and stop
// ---------------------------------------------------------------------------

func TestServerStatusStopped(t *testing.T) {
	server := newTestServer(t, newStoppedInstance())

	resp := get(t, server, "/api/server/status")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	got, raw := readJSON[statusBody](t, resp)

	if got.State != "stopped" {
		t.Errorf("state = %q, want %q", got.State, "stopped")
	}
	if got.UptimeSeconds != 0 {
		t.Errorf("uptime_seconds = %d, want 0", got.UptimeSeconds)
	}
	if len(got.Players) != 0 {
		t.Errorf("players = %v, want none", got.Players)
	}
	// Clients iterate over players, so it must be [] and never null.
	if !strings.Contains(raw, `"players":[]`) {
		t.Errorf("body = %s, want players as an empty array, not null", raw)
	}
}

func TestServerStartLaunchFailure(t *testing.T) {
	inst := newFailingLaunchInstance(t)
	server := newTestServer(t, inst)

	resp := post(t, server, "/api/server/start", "", "")

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}

	// readJSON also fails if the body holds more than one JSON value, which
	// catches a missing return after writeError.
	body, _ := readJSON[errorResponse](t, resp)
	if body.Error != "internal error" {
		t.Errorf("error = %q, want %q (launch details belong in the log)", body.Error, "internal error")
	}

	if got := inst.State(); got != minecraft.StateStopped {
		t.Errorf("state after failed start = %v, want %v", got, minecraft.StateStopped)
	}
}

func TestServerStopWhenStopped(t *testing.T) {
	server := newTestServer(t, newStoppedInstance())

	resp := post(t, server, "/api/server/stop", "", "")

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusConflict)
	}

	body, _ := readJSON[errorResponse](t, resp)
	if !strings.Contains(body.Error, "stopped") {
		t.Errorf("error = %q, want it to mention the stopped state", body.Error)
	}
}

// ---------------------------------------------------------------------------
// Console history
// ---------------------------------------------------------------------------

// Assumes GET /api/server/console?lines=N (query parameter, default 100,
// valid range 1 to 1000).
func TestServerConsole(t *testing.T) {
	server := newTestServer(t, newStoppedInstance())

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{"default", "", http.StatusOK},
		{"explicit lines", "?lines=10", http.StatusOK},
		{"maximum", "?lines=1000", http.StatusOK},
		{"empty value is the default", "?lines=", http.StatusOK},
		{"zero", "?lines=0", http.StatusBadRequest},
		{"negative", "?lines=-5", http.StatusBadRequest},
		{"above maximum", "?lines=1001", http.StatusBadRequest},
		{"not a number", "?lines=abc", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := get(t, server, "/api/server/console"+tt.query)

			if resp.StatusCode != tt.want {
				t.Fatalf("status code = %d, want %d", resp.StatusCode, tt.want)
			}

			if tt.want != http.StatusOK {
				body, _ := readJSON[errorResponse](t, resp)
				if body.Error == "" {
					t.Error("error response has no message")
				}
				return
			}

			got, raw := readJSON[consoleBody](t, resp)
			if len(got.Lines) != 0 {
				t.Errorf("lines = %v, want none for a server that never ran", got.Lines)
			}
			if !strings.Contains(raw, `"lines":[]`) {
				t.Errorf("body = %s, want lines as an empty array, not null", raw)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Command
// ---------------------------------------------------------------------------

// Assumes POST /api/server/command with a JSON body of {"command": "..."}, a
// required application/json content type, and a body size limit. Validation
// runs before the state check, so a stopped instance is enough.
func TestServerCommandValidation(t *testing.T) {
	server := newTestServer(t, newStoppedInstance())

	tests := []struct {
		name        string
		contentType string
		body        string
		want        int
	}{
		{"wrong content type", "text/plain", `{"command":"list"}`, http.StatusUnsupportedMediaType},
		{"no content type", "", `{"command":"list"}`, http.StatusUnsupportedMediaType},
		{"malformed json", "application/json", `{`, http.StatusBadRequest},
		{"empty body", "application/json", ``, http.StatusBadRequest},
		{"empty command", "application/json", `{"command":""}`, http.StatusBadRequest},
		{"whitespace only", "application/json", `{"command":"   "}`, http.StatusBadRequest},
		// In backticks, \n is a backslash and an n: the JSON escape. The decoder
		// turns it into a real newline, which is the smuggling case to catch.
		{"embedded newline", "application/json", `{"command":"say hi\nstop"}`, http.StatusBadRequest},
		{"embedded carriage return", "application/json", `{"command":"say hi\rstop"}`, http.StatusBadRequest},
		{"oversized body", "application/json", `{"command":"` + strings.Repeat("a", 1<<20) + `"}`, http.StatusBadRequest},
		// Passes validation, then hits the state check.
		{"valid command, server stopped", "application/json", `{"command":"list"}`, http.StatusConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := post(t, server, "/api/server/command", tt.contentType, tt.body)

			if resp.StatusCode != tt.want {
				t.Fatalf("status code = %d, want %d", resp.StatusCode, tt.want)
			}

			body, _ := readJSON[errorResponse](t, resp)
			if body.Error == "" {
				t.Error("error response has no message")
			}
		})
	}
}

// TestServerCommandControlCharacter only passes once Instance.Send rejects
// control characters other than line breaks. Remove it if you decide not to.
func TestServerCommandControlCharacter(t *testing.T) {
	server := newTestServer(t, newStoppedInstance())

	resp := post(t, server, "/api/server/command", "application/json", `{"command":"say \u0007"}`)

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

func TestNewMetricsResponse(t *testing.T) {
	got := newMetricsResponse(minecraft.Metrics{
		State:             minecraft.StateRunning,
		Uptime:            90*time.Second + 900*time.Millisecond,
		PlayersOnline:     2,
		CPUPercent:        150,
		CPUMachinePercent: 37.5,
		CPUCores:          4,
		MemoryBytes:       1 << 30,
		DiskUsedBytes:     10,
		DiskFreeBytes:     20,
		DiskTotalBytes:    30,
	})

	// Every field has a distinct value on purpose, so a field copied from the
	// wrong source (say used and free swapped) fails the comparison.
	want := MetricsResponse{
		State:             "running",
		UptimeSeconds:     90, // truncated, not rounded
		PlayersOnline:     2,
		CPUPercent:        150,
		CPUMachinePercent: 37.5,
		CPUCores:          4,
		MemoryBytes:       1 << 30,
		DiskUsedBytes:     10,
		DiskFreeBytes:     20,
		DiskTotalBytes:    30,
	}
	if got != want {
		t.Errorf("newMetricsResponse() = %+v, want %+v", got, want)
	}
}

func TestServerMetrics(t *testing.T) {
	server := newTestServer(t, newStoppedInstanceWithDir(t))

	resp := get(t, server, "/api/server/metrics")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	got, _ := readJSON[metricsBody](t, resp)
	if got.State != "stopped" {
		t.Errorf("state = %q, want %q", got.State, "stopped")
	}
	if got.MemoryBytes != 0 || got.CPUPercent != 0 || got.UptimeSeconds != 0 {
		t.Errorf("process fields = %+v, want all zero for a stopped server", got)
	}
	if got.DiskTotalBytes == 0 {
		t.Error("disk_total_bytes = 0, want disk usage reported even when stopped")
	}
	if got.CPUCores <= 0 {
		t.Errorf("cpu_cores = %d, want a positive number", got.CPUCores)
	}
}

// ---------------------------------------------------------------------------
// Event streams (SSE)
// ---------------------------------------------------------------------------

// sseReader turns an event stream into a channel of data payloads.
// Comment lines (": keepalive") and blank separator lines are skipped.
type sseReader struct {
	data chan string
	err  error // written before data is closed, so read it only after data is closed
}

func newSSEReader(body io.Reader) *sseReader {
	r := &sseReader{data: make(chan string, 16)}
	go func() {
		defer close(r.data)
		sc := bufio.NewScanner(body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				r.data <- data
			}
		}
		r.err = sc.Err()
	}()
	return r
}

func (r *sseReader) next(t *testing.T) string {
	t.Helper()

	select {
	case data, ok := <-r.data:
		if !ok {
			t.Fatal("stream ended, expected a message")
		}
		return data
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a message")
	}
	return ""
}

// openStream opens an event stream. The cleanup cancels the request before
// closing the body, and it runs before the test server's own cleanup, which
// waits for open requests to finish.
func openStream(t *testing.T, server *httptest.Server, path string) (*http.Response, *sseReader) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		resp.Body.Close()
	})

	return resp, newSSEReader(resp.Body)
}

func checkStreamHeaders(t *testing.T, resp *http.Response) {
	t.Helper()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
}

func TestServerEventsStream(t *testing.T) {
	server := newTestServer(t, newFailingLaunchInstance(t))

	resp, events := openStream(t, server, "/api/server/events")
	checkStreamHeaders(t, resp)

	decode := func() eventBody {
		t.Helper()
		var e eventBody
		if err := json.Unmarshal([]byte(events.next(t)), &e); err != nil {
			t.Fatal(err)
		}
		return e
	}

	// A new client is told the current state first, because events have no history.
	if e := decode(); e.Type != "state_changed" || e.State != "stopped" {
		t.Fatalf("first event = %+v, want state_changed to stopped", e)
	}

	// The subscription exists before the headers go out, so this start cannot
	// be missed. A failed launch publishes starting and then stopped.
	post(t, server, "/api/server/start", "", "")

	if e := decode(); e.Type != "state_changed" || e.State != "starting" {
		t.Errorf("event = %+v, want state_changed to starting", e)
	}
	if e := decode(); e.Type != "state_changed" || e.State != "stopped" {
		t.Errorf("event = %+v, want state_changed to stopped", e)
	}
}

func TestServerMetricsStream(t *testing.T) {
	server := newTestServer(t, newStoppedInstanceWithDir(t))

	resp, metrics := openStream(t, server, "/api/server/metrics/stream")
	checkStreamHeaders(t, resp)

	var m metricsBody
	if err := json.Unmarshal([]byte(metrics.next(t)), &m); err != nil {
		t.Fatal(err)
	}
	if m.State != "stopped" {
		t.Errorf("state = %q, want %q", m.State, "stopped")
	}
	if m.DiskTotalBytes == 0 {
		t.Error("disk_total_bytes = 0, want disk usage reported even when stopped")
	}
}

func TestServerConsoleStream(t *testing.T) {
	server := newTestServer(t, newStoppedInstance())

	t.Run("opens with lines=0", func(t *testing.T) {
		resp, _ := openStream(t, server, "/api/server/console/stream?lines=0")
		checkStreamHeaders(t, resp)
	})

	for _, query := range []string{"?lines=-1", "?lines=1001", "?lines=abc"} {
		t.Run("rejects "+query, func(t *testing.T) {
			resp := get(t, server, "/api/server/console/stream"+query)

			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusBadRequest)
			}
			if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/event-stream") {
				t.Error("a rejected request must not start a stream")
			}
		})
	}
}

func TestStreamsRejectWrongMethod(t *testing.T) {
	server := newTestServer(t, newStoppedInstance())

	for _, path := range []string{
		"/api/server/events",
		"/api/server/console/stream",
		"/api/server/metrics/stream",
	} {
		t.Run(path, func(t *testing.T) {
			resp := post(t, server, path, "", "")
			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("status code = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
			}
		})
	}
}
