package apitypes

import "time"

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
