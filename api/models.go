package api

import (
	"encoding/json/v2"
	"log/slog"
	"net/http"

	"github.com/emiHuy/mc-server-manager/internal/apitypes"
	"github.com/emiHuy/mc-server-manager/minecraft"
)

func newMetricsResponse(m minecraft.Metrics) apitypes.MetricsResponse {
	return apitypes.MetricsResponse{
		State:             m.State.String(),
		UptimeSeconds:     int64(m.Uptime.Seconds()),
		PlayersOnline:     m.PlayersOnline,
		CPUPercent:        m.CPUPercent,
		CPUMachinePercent: m.CPUMachinePercent,
		CPUCores:          m.CPUCores,
		MemoryBytes:       m.MemoryBytes,
		DiskUsedBytes:     m.DiskUsedBytes,
		DiskFreeBytes:     m.DiskFreeBytes,
		DiskTotalBytes:    m.DiskTotalBytes,
	}
}

func newEventResponse(e minecraft.Event) apitypes.EventResponse {
	resp := apitypes.EventResponse{
		Type:      e.Type.String(),
		Timestamp: e.Timestamp,
		Player:    e.Player,
		Message:   e.Message,
	}
	if e.Type == minecraft.EventStateChanged {
		resp.State = e.State.String()
	}
	return resp
}

func newConsoleLineResponse(line string) apitypes.ConsoleLineResponse {
	return apitypes.ConsoleLineResponse{Line: line}
}

func newAutoRestartResponse(s minecraft.RestarterSnapshot) apitypes.AutoRestartResponse {
	resp := apitypes.AutoRestartResponse{
		Enabled:     s.Enabled,
		Restarts:    s.Restarts,
		MaxRestarts: s.MaxRestarts,
		GaveUp:      s.GaveUp,
	}
	if !s.NextRetryAt.IsZero() {
		t := s.NextRetryAt
		resp.NextRetryAt = &t
	}
	return resp
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	err := json.MarshalWrite(w, v)
	if err != nil {
		slog.Error("cannot write response", "error", err)
	}
}
