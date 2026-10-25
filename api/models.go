package api

import (
	"encoding/json/v2"
	"log/slog"
	"net/http"
	"time"

	"github.com/emiHuy/mc-server-manager/minecraft"
)

type StateResponse struct {
	State string `json:"state"`
}

type StatusResponse struct {
	State         string   `json:"state"`
	UptimeSeconds int64    `json:"uptime_seconds"`
	Players       []string `json:"players"`
}

type MetricsResponse struct {
	State             string  `json:"state"`
	UptimeSeconds     int64   `json:"uptime_seconds"`
	PlayersOnline     int     `json:"players_online"`
	CPUPercent        float64 `json:"cpu_percent"`
	CPUMachinePercent float64 `json:"cpu_machine_percent"`
	CPUCores          int     `json:"cpu_cores"`
	MemoryBytes       uint64  `json:"memory_bytes"`
	DiskUsedBytes     uint64  `json:"disk_used_bytes"`
	DiskFreeBytes     uint64  `json:"disk_free_bytes"`
	DiskTotalBytes    uint64  `json:"disk_total_bytes"`
}

type EventResponse struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Player    string    `json:"player,omitempty"`
	Message   string    `json:"message,omitempty"`
	State     string    `json:"state,omitempty"`
}

type ConsoleResponse struct {
	Lines []string `json:"lines"`
}

type ConsoleLineResponse struct {
	Line string `json:"line"`
}

type CommandRequest struct {
	Command string `json:"command"`
}

func newMetricsResponse(m minecraft.Metrics) MetricsResponse {
	return MetricsResponse{
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

func newEventResponse(e minecraft.Event) EventResponse {
	resp := EventResponse{
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

func newConsoleLineResponse(line string) ConsoleLineResponse {
	return ConsoleLineResponse{Line: line}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	err := json.MarshalWrite(w, v)
	if err != nil {
		slog.Error("cannot write response", "error", err)
	}
}
