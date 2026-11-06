// Package api provides the HTTP REST and Server-Sent Events (SSE) interface for managing and monitoring the Minecraft server instance.
package api

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/minecraft"
)

const readHeaderTimeout = 5 * time.Second

//go:embed static
var webFiles embed.FS

type Server struct {
	httpServer *http.Server
	instance   *minecraft.Instance
	token      string
}

func New(instance *minecraft.Instance, conf config.APIConfig) *Server {
	s := Server{
		instance: instance,
		token:    conf.Token,
	}

	ctx, cancel := context.WithCancel(context.Background())

	s.httpServer = &http.Server{
		Addr:              net.JoinHostPort(conf.Host, strconv.Itoa(conf.Port)),
		Handler:           s.auth(s.routes()),
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	s.httpServer.RegisterOnShutdown(cancel)

	return &s
}

func (s *Server) Run() error {
	slog.Info("api server listening", "addr", s.httpServer.Addr, "dashboard", dashboardURL(s.httpServer.Addr))
	err := s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	handle(mux, http.MethodGet, "/api/server/status", s.handleServerStatus)
	handle(mux, http.MethodPost, "/api/server/start", s.handleServerStart)
	handle(mux, http.MethodPost, "/api/server/stop", s.handleServerStop)
	handle(mux, http.MethodPost, "/api/server/kill", s.handleServerKill)
	handle(mux, http.MethodPost, "/api/server/command", s.handleServerCommand)
	handle(mux, http.MethodGet, "/api/server/console", s.handleServerConsole)
	handle(mux, http.MethodGet, "/api/server/metrics", s.handleServerMetrics)
	handle(mux, http.MethodGet, "/api/server/events", s.handleServerEvents)
	handle(mux, http.MethodGet, "/api/server/console/stream", s.handleServerConsoleStream)
	handle(mux, http.MethodGet, "/api/server/metrics/stream", s.handleServerMetricsStream)

	site, err := fs.Sub(webFiles, "static")
	if err != nil {
		panic(err) // only fails on an invalid path; the embed itself is checked at compile time
	}
	mux.Handle("GET /dashboard/", http.StripPrefix("/dashboard/", http.FileServerFS(site)))
	mux.Handle("GET /dashboard", http.RedirectHandler("/dashboard/", http.StatusMovedPermanently))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, errNotFound)
	})

	return mux
}

// handle registers handler for method and path. Any other method on the same
// path gets a JSON 405 with an Allow header.
func handle(mux *http.ServeMux, method, path string, handler http.HandlerFunc) {
	mux.HandleFunc(method+" "+path, handler)
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", method)
		writeError(w, r, errMethodNotAllowed)
	})
}

// dashboardURL turns a listen address like ":8080" or "0.0.0.0:8080" into a URL you can open in a browser.
func dashboardURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}

	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port) + "/dashboard"
}
