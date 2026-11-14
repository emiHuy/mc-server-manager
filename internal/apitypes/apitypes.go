// Package apitypes defines the JSON request and response types shared by the HTTP API and its clients.
package apitypes

import "time"

type StateResponse struct {
	State string `json:"state"`
}

type StatusResponse struct {
	State         string              `json:"state"`
	UptimeSeconds int64               `json:"uptime_seconds"`
	Players       []string            `json:"players"`
	AutoRestart   AutoRestartResponse `json:"auto_restart"`
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
	Backup    string    `json:"backup,omitempty"`
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

type AutoRestartRequest struct {
	Enabled *bool `json:"enabled"`
}

type AutoRestartResponse struct {
	Enabled     bool       `json:"enabled"`
	Restarts    int        `json:"restarts"`
	MaxRestarts int        `json:"max_restarts"`
	NextRetryAt *time.Time `json:"next_retry_at"`
	GaveUp      bool       `json:"gave_up"`
}

type BackupResponse struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	SizeBytes int64     `json:"size_bytes"`
}

type BackupListResponse struct {
	Backups []BackupResponse `json:"backups"`
}
