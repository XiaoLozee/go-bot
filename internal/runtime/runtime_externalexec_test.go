package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/plugin/externalexec"
	"github.com/XiaoLozee/go-bot/internal/plugin/host"
)

const runtimeExternalHelperEnv = "GO_BOT_RUNTIME_EXTERNAL_HELPER"
const runtimeExternalHelperModeEnv = "GO_BOT_RUNTIME_EXTERNAL_HELPER_MODE"

type runtimeHelperHostMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func TestServiceDiscoverInstallStartExternalExecPlugin(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(config dir) error = %v", err)
	}

	pluginDir := filepath.Join(root, "plugins", "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(plugin dir) error = %v", err)
	}
	schemaPath := filepath.Join(pluginDir, "config.schema.json")
	if err := os.WriteFile(schemaPath, []byte(`{"type":"object","properties":{"enabled":{"type":"boolean"}}}`), 0o644); err != nil {
		t.Fatalf("WriteFile(config.schema.json) error = %v", err)
	}

	manifest := fmt.Sprintf(`
id: ext_demo
name: External Demo
version: 0.1.0
description: runtime external plugin test
entry: %q
config_schema: ./config.schema.json
args:
  - "-test.run=^TestRuntimeExternalExecHelperProcess$"
`, os.Args[0])
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.yaml) error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := New(testRuntimeConfig(), configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	snapshot := findPluginSnapshot(t, service.PluginSnapshots(), "ext_demo")
	if snapshot.Kind != externalexec.KindExternalExec {
		t.Fatalf("snapshot kind = %q, want %q", snapshot.Kind, externalexec.KindExternalExec)
	}
	if snapshot.Configured {
		t.Fatalf("snapshot configured = true, want false before install")
	}

	if err := service.InstallPlugin(context.Background(), "ext_demo"); err != nil {
		t.Fatalf("InstallPlugin() error = %v", err)
	}

	detail, ok := service.PluginDetail("ext_demo")
	if !ok {
		t.Fatalf("PluginDetail(install) ok = false, want true")
	}
	if detail.Snapshot.Kind != externalexec.KindExternalExec {
		t.Fatalf("detail kind = %q, want %q", detail.Snapshot.Kind, externalexec.KindExternalExec)
	}
	if !detail.Snapshot.Configured || detail.Snapshot.Enabled {
		t.Fatalf("detail after install = %+v, want configured=true enabled=false", detail.Snapshot)
	}
	if detail.ConfigSchemaPath != schemaPath {
		t.Fatalf("config schema path = %q, want %q", detail.ConfigSchemaPath, schemaPath)
	}
	if got, _ := detail.ConfigSchema["type"].(string); got != "object" {
		t.Fatalf("config schema type = %#v, want object", detail.ConfigSchema["type"])
	}
	if detail.ConfigSchemaError != "" {
		t.Fatalf("config schema error = %q, want empty", detail.ConfigSchemaError)
	}

	savedPath := config.ResolveWritablePath(configPath)
	payload, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("ReadFile(saved config) error = %v", err)
	}
	if !strings.Contains(string(payload), "kind: external_exec") {
		t.Fatalf("saved config = %s, want kind: external_exec", string(payload))
	}

	t.Setenv(runtimeExternalHelperEnv, "1")
	service.state = StateRunning
	if err := service.StartPlugin(context.Background(), "ext_demo"); err != nil {
		t.Fatalf("StartPlugin() error = %v", err)
	}
	if err := service.ReloadPlugin(context.Background(), "ext_demo"); err != nil {
		t.Fatalf("ReloadPlugin() error = %v", err)
	}

	detail, ok = service.PluginDetail("ext_demo")
	if !ok {
		t.Fatalf("PluginDetail(start) ok = false, want true")
	}
	if detail.Snapshot.State != host.PluginRunning {
		t.Fatalf("state after start = %s, want running", detail.Snapshot.State)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.StopPlugin(stopCtx, "ext_demo"); err != nil {
		t.Fatalf("StopPlugin() error = %v", err)
	}
	if err := service.UninstallPlugin(context.Background(), "ext_demo"); err != nil {
		t.Fatalf("UninstallPlugin() error = %v", err)
	}

	detail, ok = service.PluginDetail("ext_demo")
	if ok {
		t.Fatalf("PluginDetail(uninstall) ok = true, want false after physical delete; detail=%+v", detail)
	}
	if exists, err := pathExists(pluginDir); err != nil {
		t.Fatalf("pathExists(pluginDir) error = %v", err)
	} else if exists {
		t.Fatalf("plugin dir still exists after uninstall: %s", pluginDir)
	}
}

func TestServicePluginDetailReflectsExternalExecCrash(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(config dir) error = %v", err)
	}

	pluginDir := filepath.Join(root, "plugins", "crashy")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(plugin dir) error = %v", err)
	}

	manifest := fmt.Sprintf(`
id: crashy
name: Crashy
version: 0.1.0
description: runtime external crash test
entry: %q
args:
  - "-test.run=^TestRuntimeExternalExecHelperProcess$"
`, os.Args[0])
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.yaml) error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := New(testRuntimeConfig(), configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := service.InstallPlugin(context.Background(), "crashy"); err != nil {
		t.Fatalf("InstallPlugin() error = %v", err)
	}

	t.Setenv(runtimeExternalHelperEnv, "1")
	t.Setenv(runtimeExternalHelperModeEnv, "exit_after_ready")
	service.state = StateRunning
	if err := service.StartPlugin(context.Background(), "crashy"); err != nil {
		t.Fatalf("StartPlugin() error = %v", err)
	}

	detail, ok := waitForPluginDetail(t, service, "crashy", 2*time.Second, func(detail PluginDetail) bool {
		return detail.Snapshot.State == host.PluginFailed
	})
	if !ok {
		t.Fatalf("waitForPluginDetail() timed out")
	}
	if detail.Snapshot.LastError == "" {
		t.Fatalf("snapshot last error = empty, want crash message")
	}
	if detail.Runtime.Running {
		t.Fatalf("runtime running = true, want false")
	}
	if detail.Runtime.ExitCode == nil || *detail.Runtime.ExitCode == 0 {
		t.Fatalf("runtime exit code = %#v, want non-zero", detail.Runtime.ExitCode)
	}
	if len(detail.Runtime.RecentLogs) == 0 {
		t.Fatalf("runtime recent logs = empty, want lifecycle logs")
	}
}

func TestServiceNewEnsuresPythonCommonRuntime(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(config dir) error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := New(testRuntimeConfig(), configPath, logger); err != nil {
		t.Fatalf("New() error = %v", err)
	}

	assertPythonCommonRuntimeExists(t, filepath.Join(root, "plugins", "_common"))
}

func TestServiceNewRepairsIncompletePythonCommonRuntime(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(config dir) error = %v", err)
	}

	commonDir := filepath.Join(root, "plugins", "_common")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(common) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(commonDir, "gobot_runtime.py"), []byte("# partial runtime\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(partial runtime) error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := New(testRuntimeConfig(), configPath, logger); err != nil {
		t.Fatalf("New() error = %v", err)
	}

	assertPythonCommonRuntimeExists(t, commonDir)
}

func TestServiceSyncExternalPlugins_RepairsIncompletePythonCommon(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(config dir) error = %v", err)
	}
	if err := writeTestPythonCommonRuntime(filepath.Join(root, "plugins", "_common")); err != nil {
		t.Fatalf("writeTestPythonCommonRuntime() error = %v", err)
	}

	pluginDir := filepath.Join(root, "plugins", "menu_hint")
	if err := os.MkdirAll(filepath.Join(pluginDir, "_common"), 0o755); err != nil {
		t.Fatalf("MkdirAll(plugin common) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte(`id: menu_hint
name: Menu Hint
version: 0.1.0
runtime: python
entry: ./main.py
protocol: stdio_jsonrpc
`), 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.yaml) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "main.py"), []byte(`print("ready")`), 0o644); err != nil {
		t.Fatalf("WriteFile(main.py) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "_common", "gobot_runtime.py"), []byte("# partial runtime\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(partial gobot_runtime.py) error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := New(testRuntimeConfig(), configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for _, rel := range []string{
		filepath.Join("_common", "gobot_runtime.py"),
		filepath.Join("_common", "gobot_plugin", "__init__.py"),
		filepath.Join("_common", "gobot_plugin", "models.py"),
		filepath.Join("_common", "gobot_plugin", "runtime.py"),
	} {
		if _, err := os.Stat(filepath.Join(pluginDir, rel)); err != nil {
			t.Fatalf("Stat(%s) error = %v", filepath.Join(pluginDir, rel), err)
		}
	}
	if _, ok := service.PluginDetail("menu_hint"); !ok {
		t.Fatalf("PluginDetail(menu_hint) ok = false, want true")
	}
}

func assertPythonCommonRuntimeExists(t *testing.T, commonDir string) {
	t.Helper()
	for _, rel := range []string{
		"gobot_runtime.py",
		filepath.Join("gobot_plugin", "__init__.py"),
		filepath.Join("gobot_plugin", "models.py"),
		filepath.Join("gobot_plugin", "runtime.py"),
	} {
		if _, err := os.Stat(filepath.Join(commonDir, rel)); err != nil {
			t.Fatalf("Stat(%s) error = %v", filepath.Join(commonDir, rel), err)
		}
	}
}

func TestRuntimeExternalExecHelperProcess(t *testing.T) {
	if os.Getenv(runtimeExternalHelperEnv) != "1" {
		return
	}

	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	var startMsg runtimeHelperHostMessage
	if err := decoder.Decode(&startMsg); err != nil {
		t.Fatalf("decode start message error = %v", err)
	}
	if startMsg.Type != "start" {
		t.Fatalf("start message type = %q, want start", startMsg.Type)
	}
	if err := encoder.Encode(map[string]any{
		"type":    "ready",
		"payload": map[string]any{"message": "runtime-helper-ready"},
	}); err != nil {
		t.Fatalf("encode ready message error = %v", err)
	}
	if os.Getenv(runtimeExternalHelperModeEnv) == "exit_after_ready" {
		os.Exit(9)
	}

	for {
		var msg runtimeHelperHostMessage
		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				return
			}
			t.Fatalf("decode host message error = %v", err)
		}
		if msg.Type == "stop" {
			return
		}
	}
}

func findPluginSnapshot(t *testing.T, items []host.Snapshot, id string) host.Snapshot {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("plugin snapshot not found: %s", id)
	return host.Snapshot{}
}

func waitForPluginDetail(t *testing.T, service *Service, id string, timeout time.Duration, match func(PluginDetail) bool) (PluginDetail, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		detail, ok := service.PluginDetail(id)
		if ok && match(detail) {
			return detail, true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return PluginDetail{}, false
}
