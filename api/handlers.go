package api

import (
	"encoding/json/v2"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/emiHuy/mc-server-manager/minecraft"
)

const (
	defaultConsoleLines = 100
	maxConsoleLines     = 1000 // the size of the console history ring
	maxCommandBody      = 4 << 10
)

func (s *Server) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	status := s.instance.Status()
	writeJSON(w, http.StatusOK, StatusResponse{
		State:         status.State.String(),
		UptimeSeconds: int64(status.Uptime.Seconds()),
		Players:       status.Players,
	})
}

func (s *Server) handleServerStart(w http.ResponseWriter, r *http.Request) {
	err := s.instance.Start()
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, StateResponse{
		State: minecraft.StateStarting.String(),
	})
}

func (s *Server) handleServerStop(w http.ResponseWriter, r *http.Request) {
	err := s.instance.Stop()
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, StateResponse{
		State: minecraft.StateStopped.String(),
	})
}

func (s *Server) handleServerKill(w http.ResponseWriter, r *http.Request) {
	err := s.instance.Kill()
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, StateResponse{
		State: minecraft.StateStopped.String(),
	})
}

func (s *Server) handleServerCommand(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, r, errUnsupportedMediaType)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCommandBody)

	var req CommandRequest
	err = json.UnmarshalRead(r.Body, &req)
	if err != nil {
		writeError(w, r, errBadRequest)
		return
	}

	err = s.instance.Send(req.Command)
	if err != nil {
		writeError(w, r, err)
		return
	}

	slog.Info("console command sent", "command", req.Command, "remote", r.RemoteAddr)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleServerConsole(w http.ResponseWriter, r *http.Request) {
	n := defaultConsoleLines

	n, err := linesParam(r, 1)
	if err != nil {
		writeError(w, r, err)
		return
	}

	lines := s.instance.RecentConsole(n)
	writeJSON(w, http.StatusOK, ConsoleResponse{Lines: lines})
}

func (s *Server) handleServerMetrics(w http.ResponseWriter, r *http.Request) {
	metrics, err := s.instance.Metrics()
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newMetricsResponse(metrics))
}

func (s *Server) handleServerEvents(w http.ResponseWriter, r *http.Request) {
	// Subscribe before reading the state, so no transition can fall between the two.
	sub := s.instance.SubscribeEvents()
	defer sub.Cancel()
	state := s.instance.State()

	sse, err := newSSEWriter(w)
	if err != nil {
		slog.Warn("cannot start event stream", "error", err)
		return
	}

	err = sse.send(EventResponse{
		Type:      minecraft.EventStateChanged.String(),
		Timestamp: time.Now(),
		State:     state.String(),
	})
	if err != nil {
		return
	}

	serveStream(r.Context(), sse, sub.C, newEventResponse)
}

func (s *Server) handleServerConsoleStream(w http.ResponseWriter, r *http.Request) {
	// Validate before the headers go out, so a bad request still gets a JSON 400.
	n, err := linesParam(r, 0)
	if err != nil {
		writeError(w, r, err)
		return
	}

	snapshot, sub := s.instance.SubscribeConsole(n)
	defer sub.Cancel()

	sse, err := newSSEWriter(w)
	if err != nil {
		slog.Warn("cannot start console stream", "error", err)
		return
	}

	for _, line := range snapshot {
		if err := sse.send(newConsoleLineResponse(line)); err != nil {
			return
		}
	}

	serveStream(r.Context(), sse, sub.C, newConsoleLineResponse)
}

func (s *Server) handleServerMetricsStream(w http.ResponseWriter, r *http.Request) {
	latest, sub := s.instance.SubscribeMetrics()
	defer sub.Cancel()

	sse, err := newSSEWriter(w)
	if err != nil {
		slog.Warn("cannot start metrics stream", "error", err)
		return
	}

	for _, m := range latest {
		if err := sse.send(newMetricsResponse(m)); err != nil {
			return
		}
	}

	serveStream(r.Context(), sse, sub.C, newMetricsResponse)
}

func linesParam(r *http.Request, min int) (int, error) {
	v := r.URL.Query().Get("lines")
	if v == "" {
		return defaultConsoleLines, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > maxConsoleLines {
		return 0, errInvalidLines
	}
	return n, nil
}
