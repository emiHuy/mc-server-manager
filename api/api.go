// Package api provides the HTTP REST and Server-Sent Events (SSE) interface for managing and monitoring the Minecraft server instance.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/minecraft"
)

const readHeaderTimeout = 5 * time.Second

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
	slog.Info("api server listening", "addr", s.httpServer.Addr)
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
