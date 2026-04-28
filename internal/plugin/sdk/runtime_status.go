package sdk

import "time"

type RuntimeLog struct {
	At      time.Time `json:"at"`
	Level   string    `json:"level,omitempty"`
	Source  string    `json:"source,omitempty"`
	Message string    `json:"message"`
}

type RuntimeStatus struct {
	Running             bool         `json:"running"`
	Restarting          bool         `json:"restarting,omitempty"`
	AutoRestart         bool         `json:"auto_restart,omitempty"`
	RestartCount        int          `json:"restart_count,omitempty"`
	ConsecutiveFailures int          `json:"consecutive_failures,omitempty"`
	NextRestartAt       time.Time    `json:"next_restart_at,omitempty"`
	CircuitOpen         bool         `json:"circuit_open,omitempty"`
	CircuitReason       string       `json:"circuit_reason,omitempty"`
	PID                 int          `json:"pid,omitempty"`
	StartedAt           time.Time    `json:"started_at,omitempty"`
	StoppedAt           time.Time    `json:"stopped_at,omitempty"`
	ExitCode            *int         `json:"exit_code,omitempty"`
	LastError           string       `json:"last_error,omitempty"`
	RecentLogs          []RuntimeLog `json:"recent_logs,omitempty"`
}

type RuntimeStatusProvider interface {
	RuntimeStatus() RuntimeStatus
}
