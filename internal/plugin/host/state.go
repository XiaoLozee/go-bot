package host

import "github.com/XiaoLozee/go-bot/internal/plugin/sdk"

type PluginState string

const (
	PluginStopped  PluginState = "stopped"
	PluginStarting PluginState = "starting"
	PluginRunning  PluginState = "running"
	PluginFailed   PluginState = "failed"
	PluginStopping PluginState = "stopping"
)

type Snapshot struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Description string      `json:"description,omitempty"`
	Author      string      `json:"author,omitempty"`
	Kind        string      `json:"kind,omitempty"`
	State       PluginState `json:"state"`
	LastError   string      `json:"last_error,omitempty"`
	Builtin     bool        `json:"builtin"`
	Enabled     bool        `json:"enabled"`
	Configured  bool        `json:"configured"`
}

type Detail struct {
	Snapshot
	Config  map[string]any    `json:"config"`
	Runtime sdk.RuntimeStatus `json:"runtime,omitempty"`
}
