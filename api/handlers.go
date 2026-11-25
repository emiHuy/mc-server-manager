package api

import (
	"encoding/json/v2"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/emiHuy/mc-server-manager/internal/apitypes"
	"github.com/emiHuy/mc-server-manager/minecraft"
)

const (
	defaultConsoleLines = 100
	maxConsoleLines     = 1000
	maxCommandBody      = 4 << 10
	maxAutoRestartBody  = 1 << 10
)

func (s *Server) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	status := s.instance.Status()
	resp := apitypes.StatusResponse{
		State:         status.State.String(),
		UptimeSeconds: int64(status.Uptime.Seconds()),
		Players:       status.Players,
		AutoRestart:   newAutoRestartResponse(s.restarter.Status()),
	}

	if s.nextBackup != nil {
		if t, ok := s.nextBackup.NextRun(); ok {
			resp.NextBackup = &t
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleServerStart(w http.ResponseWriter, r *http.Request) {
	if err := s.instance.Start(); err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, apitypes.StateResponse{
		State: minecraft.StateStarting.String(),
	})
}

func (s *Server) handleServerStop(w http.ResponseWriter, r *http.Request) {
	if err := s.instance.Stop(); err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, apitypes.StateResponse{
		State: minecraft.StateStopped.String(),
	})
}

func (s *Server) handleServerKill(w http.ResponseWriter, r *http.Request) {
	if err := s.instance.Kill(); err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, apitypes.StateResponse{
		State: minecraft.StateStopped.String(),
	})
}

func (s *Server) handleServerRestart(w http.ResponseWriter, r *http.Request) {
	if err := s.instance.Restart(); err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, apitypes.StateResponse{
		State: minecraft.StateStarting.String(),
	})
}

func (s *Server) handleServerCommand(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, r, errUnsupportedMediaType)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCommandBody)

	var req apitypes.CommandRequest

	if err = json.UnmarshalRead(r.Body, &req); err != nil {
		writeError(w, r, errBadRequest)
		return
	}

	if err = s.instance.Send(req.Command); err != nil {
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
	writeJSON(w, http.StatusOK, apitypes.ConsoleResponse{Lines: lines})
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
	sub := s.instance.SubscribeEvents()
	defer sub.Cancel()

	state := s.instance.State()

	sse, err := newSSEWriter(w)
	if err != nil {
		slog.Warn("cannot start event stream", "error", err)
		return
	}

	err = sse.send(apitypes.EventResponse{
		Type:      minecraft.EventStateChanged.String(),
		Timestamp: time.Now(),
		State:     state.String(),
	})
	if err != nil {
		return
	}

	serveStream(r.Context(), sse, sub.C, newEventResponse)
}

func (s *Server) handleServerAutoRestart(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, r, errUnsupportedMediaType)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAutoRestartBody)

	var req apitypes.AutoRestartRequest

	err = json.UnmarshalRead(r.Body, &req)
	if err != nil || req.Enabled == nil {
		writeError(w, r, errBadRequest)
		return
	}

	s.restarter.SetEnabled(*req.Enabled)
	slog.Info("auto-restart toggled", "enabled", *req.Enabled, "remote", r.RemoteAddr)

	writeJSON(w, http.StatusOK, newAutoRestartResponse(s.restarter.Status()))
}

func (s *Server) handleServerConsoleStream(w http.ResponseWriter, r *http.Request) {
	n, err := linesParam(r, 1)
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

func (s *Server) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	info, err := s.backups.Create(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}

	slog.Info("backup created", "id", info.ID, "remote", r.RemoteAddr)
	writeJSON(w, http.StatusCreated, newBackupResponse(info))
}

func (s *Server) handleBackupList(w http.ResponseWriter, r *http.Request) {
	infos, err := s.backups.List()
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newBackupListResponse(infos))
}

func (s *Server) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.backups.Delete(id); err != nil {
		writeError(w, r, err)
		return
	}

	slog.Info("backup deleted", "id", id, "remote", r.RemoteAddr)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	path, err := s.backups.BackupExists(id)
	if err != nil {
		writeError(w, r, err)
		return
	}

	w.Header().Set("Content-Disposition", `attachment; filename="`+id+`.zip"`)
	http.ServeFile(w, r, path)
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
