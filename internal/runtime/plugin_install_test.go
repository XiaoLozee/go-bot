package runtime

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/XiaoLozee/go-bot/internal/plugin/host"
)

func TestServiceInstallPluginPackage_ZIP(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(config dir) error = %v", err)
	}

	service, err := New(testRuntimeConfig(), configPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload := buildPluginZip(t, map[string]string{
		"demo-plugin/plugin.yaml":        pluginManifestForUploadTest(t, "uploaded_demo", "1.0.0"),
		"demo-plugin/config.schema.json": `{"type":"object","properties":{"keyword":{"type":"string"}}}`,
	})

	result, err := service.InstallPluginPackage(context.Background(), "uploaded_demo.zip", payload, false)
	if err != nil {
		t.Fatalf("InstallPluginPackage() error = %v", err)
	}
	if result.PluginID != "uploaded_demo" {
		t.Fatalf("PluginID = %q, want uploaded_demo", result.PluginID)
	}
	if result.Format != pluginPackageFormatZIP {
		t.Fatalf("Format = %q, want %q", result.Format, pluginPackageFormatZIP)
	}
	if result.Replaced {
		t.Fatalf("Replaced = true, want false")
	}
	if !result.Reloaded {
		t.Fatalf("Reloaded = false, want true")
	}
	if _, err := os.Stat(result.ManifestPath); err != nil {
		t.Fatalf("Stat(%s) error = %v", result.ManifestPath, err)
	}

	snapshot := findPluginSnapshot(t, service.PluginSnapshots(), "uploaded_demo")
	if snapshot.Kind != "external_exec" {
		t.Fatalf("snapshot kind = %q, want external_exec", snapshot.Kind)
	}
	if snapshot.Configured {
		t.Fatalf("snapshot configured = true, want false before install")
	}

	detail, ok := service.PluginDetail("uploaded_demo")
	if !ok {
		t.Fatalf("PluginDetail() ok = false, want true")
	}
	if detail.ConfigSchemaPath == "" {
		t.Fatalf("ConfigSchemaPath = empty, want schema path")
	}
	if detail.ConfigSchemaError != "" {
		t.Fatalf("ConfigSchemaError = %q, want empty", detail.ConfigSchemaError)
	}
	if got, _ := detail.ConfigSchema["type"].(string); got != "object" {
		t.Fatalf("ConfigSchema.type = %#v, want object", detail.ConfigSchema["type"])
	}
}

func TestServiceInstallPluginPackage_PreparesPythonRequirementsEnv(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(config dir) error = %v", err)
	}

	service, err := New(testRuntimeConfig(), configPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	restore := stubPythonEnvCommands(t)
	defer restore()

	payload := buildPluginZip(t, map[string]string{
		"demo-plugin/plugin.yaml": `id: uploaded_demo
name: Uploaded Demo
version: 1.0.0
description: uploaded python plugin package
author: Go-bot
runtime: python
entry: ./main.py
protocol: stdio_jsonrpc
`,
		"demo-plugin/main.py":          `print("ready")`,
		"demo-plugin/requirements.txt": "requests==2.32.3\n",
	})

	result, err := service.InstallPluginPackage(context.Background(), "uploaded_demo.zip", payload, false)
	if err != nil {
		t.Fatalf("InstallPluginPackage() error = %v", err)
	}
	if !result.DependenciesInstalled {
		t.Fatalf("DependenciesInstalled = false, want true")
	}
	if result.DependencyEnvPath == "" {
		t.Fatalf("DependencyEnvPath = empty, want env path")
	}
	if _, err := os.Stat(filepath.Join(result.DependencyEnvPath, pythonVenvScriptsDir(), pythonVenvExecutableName())); err != nil {
		t.Fatalf("Stat(venv python) error = %v", err)
	}

	manifest, ok := service.host.Manifest("uploaded_demo")
	if !ok {
		t.Fatalf("Manifest(uploaded_demo) ok = false, want true")
	}
	if manifest.PythonEnv != result.DependencyEnvPath {
		t.Fatalf("manifest.PythonEnv = %q, want %q", manifest.PythonEnv, result.DependencyEnvPath)
	}
}

func TestServiceInstallPluginPackage_RepairsIncompletePythonCommon(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(config dir) error = %v", err)
	}
	if err := writeTestPythonCommonRuntime(filepath.Join(root, "plugins", "_common")); err != nil {
		t.Fatalf("writeTestPythonCommonRuntime() error = %v", err)
	}

	service, err := New(testRuntimeConfig(), configPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload := buildPluginZip(t, map[string]string{
		"demo-plugin/plugin.yaml": `id: uploaded_demo
name: Uploaded Demo
version: 1.0.0
description: uploaded python plugin package
author: Go-bot
runtime: python
entry: ./main.py
protocol: stdio_jsonrpc
`,
		"demo-plugin/main.py":                  `print("ready")`,
		"demo-plugin/_common/gobot_runtime.py": "# partial runtime\n",
	})

	result, err := service.InstallPluginPackage(context.Background(), "uploaded_demo.zip", payload, false)
	if err != nil {
		t.Fatalf("InstallPluginPackage() error = %v", err)
	}
	for _, rel := range []string{
		filepath.Join("_common", "gobot_runtime.py"),
		filepath.Join("_common", "gobot_plugin", "__init__.py"),
		filepath.Join("_common", "gobot_plugin", "models.py"),
		filepath.Join("_common", "gobot_plugin", "runtime.py"),
	} {
		if _, err := os.Stat(filepath.Join(result.InstalledTo, rel)); err != nil {
			t.Fatalf("Stat(%s) error = %v", filepath.Join(result.InstalledTo, rel), err)
		}
	}
}

func TestRequirementsFileHasEntries_IgnoresBlankAndCommentLines(t *testing.T) {
	root := t.TempDir()
	requirementsPath := filepath.Join(root, "requirements.txt")
	if err := os.WriteFile(requirementsPath, []byte("\n# Add runtime dependencies here.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(requirements) error = %v", err)
	}

	hasEntries, err := requirementsFileHasEntries(requirementsPath)
	if err != nil {
		t.Fatalf("requirementsFileHasEntries() error = %v", err)
	}
	if hasEntries {
		t.Fatalf("hasEntries = true, want false")
	}

	if err := os.WriteFile(requirementsPath, []byte("\nrequests==2.32.3\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(requirements) error = %v", err)
	}
	hasEntries, err = requirementsFileHasEntries(requirementsPath)
	if err != nil {
		t.Fatalf("requirementsFileHasEntries() error = %v", err)
	}
	if !hasEntries {
		t.Fatalf("hasEntries = false, want true")
	}
}

func TestServiceInstallPluginPackage_CommentOnlyRequirementsDoesNotPrepareEnv(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(config dir) error = %v", err)
	}

	service, err := New(testRuntimeConfig(), configPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	restore := stubPythonEnvCommands(t)
	defer restore()

	payload := buildPluginZip(t, map[string]string{
		"demo-plugin/plugin.yaml": `id: uploaded_demo
name: Uploaded Demo
version: 1.0.0
description: uploaded python plugin package
author: Go-bot
runtime: python
entry: ./main.py
protocol: stdio_jsonrpc
`,
		"demo-plugin/main.py":          `print("ready")`,
		"demo-plugin/requirements.txt": "# Add runtime dependencies here.\n",
	})

	result, err := service.InstallPluginPackage(context.Background(), "uploaded_demo.zip", payload, false)
	if err != nil {
		t.Fatalf("InstallPluginPackage() error = %v", err)
	}
	if result.DependenciesInstalled {
		t.Fatalf("DependenciesInstalled = true, want false")
	}
	if result.DependencyEnvPath != "" {
		t.Fatalf("DependencyEnvPath = %q, want empty", result.DependencyEnvPath)
	}

	manifest, ok := service.host.Manifest("uploaded_demo")
	if !ok {
		t.Fatalf("Manifest(uploaded_demo) ok = false, want true")
	}
	if manifest.PythonEnv != "" {
		t.Fatalf("manifest.PythonEnv = %q, want empty", manifest.PythonEnv)
	}
}

func TestServiceInstallPluginPackage_TarGZOverwrite_RestartsRunningPlugin(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(config dir) error = %v", err)
	}

	service, err := New(testRuntimeConfig(), configPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	initialPayload := buildPluginZip(t, map[string]string{
		"uploaded-demo/plugin.yaml":        pluginManifestForUploadTest(t, "uploaded_demo", "1.0.0"),
		"uploaded-demo/config.schema.json": `{"type":"object","properties":{"keyword":{"type":"string"}}}`,
	})
	if _, err := service.InstallPluginPackage(context.Background(), "uploaded_demo.zip", initialPayload, false); err != nil {
		t.Fatalf("InstallPluginPackage(initial) error = %v", err)
	}
	if err := service.InstallPlugin(context.Background(), "uploaded_demo"); err != nil {
		t.Fatalf("InstallPlugin() error = %v", err)
	}

	t.Setenv(runtimeExternalHelperEnv, "1")
	service.state = StateRunning
	if err := service.StartPlugin(context.Background(), "uploaded_demo"); err != nil {
		t.Fatalf("StartPlugin() error = %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_ = service.StopPlugin(stopCtx, "uploaded_demo")
	}()

	before, ok := service.PluginDetail("uploaded_demo")
	if !ok {
		t.Fatalf("PluginDetail(before overwrite) ok = false, want true")
	}
	if before.Snapshot.State != host.PluginRunning {
		t.Fatalf("state before overwrite = %s, want running", before.Snapshot.State)
	}
	if before.Snapshot.Version != "1.0.0" {
		t.Fatalf("version before overwrite = %q, want 1.0.0", before.Snapshot.Version)
	}

	overwritePayload := buildPluginTarGZ(t, map[string]string{
		"uploaded-demo/plugin.yaml":        pluginManifestForUploadTest(t, "uploaded_demo", "2.0.0"),
		"uploaded-demo/config.schema.json": `{"type":"object","properties":{"mode":{"type":"string","enum":["lite","full"]}}}`,
	})

	if _, err := service.InstallPluginPackage(context.Background(), "uploaded_demo.tar.gz", overwritePayload, false); err == nil || !strings.Contains(err.Error(), "覆盖安装") {
		t.Fatalf("InstallPluginPackage(no overwrite) error = %v, want overwrite hint", err)
	}

	result, err := service.InstallPluginPackage(context.Background(), "uploaded_demo.tar.gz", overwritePayload, true)
	if err != nil {
		t.Fatalf("InstallPluginPackage(overwrite) error = %v", err)
	}
	if result.Format != pluginPackageFormatTarGZ {
		t.Fatalf("Format = %q, want %q", result.Format, pluginPackageFormatTarGZ)
	}
	if !result.Replaced {
		t.Fatalf("Replaced = false, want true")
	}
	if result.BackupPath == "" {
		t.Fatalf("BackupPath = empty, want backup path")
	}
	if _, err := os.Stat(result.BackupPath); err != nil {
		t.Fatalf("Stat(%s) error = %v", result.BackupPath, err)
	}

	backupManifest, err := os.ReadFile(filepath.Join(result.BackupPath, "plugin.yaml"))
	if err != nil {
		t.Fatalf("ReadFile(backup manifest) error = %v", err)
	}
	if !strings.Contains(string(backupManifest), "version: 1.0.0") {
		t.Fatalf("backup manifest = %s, want version 1.0.0", string(backupManifest))
	}

	currentManifest, err := os.ReadFile(result.ManifestPath)
	if err != nil {
		t.Fatalf("ReadFile(current manifest) error = %v", err)
	}
	if !strings.Contains(string(currentManifest), "version: 2.0.0") {
		t.Fatalf("current manifest = %s, want version 2.0.0", string(currentManifest))
	}

	after, ok := service.PluginDetail("uploaded_demo")
	if !ok {
		t.Fatalf("PluginDetail(after overwrite) ok = false, want true")
	}
	if after.Snapshot.State != host.PluginRunning {
		t.Fatalf("state after overwrite = %s, want running", after.Snapshot.State)
	}
	if after.Snapshot.Version != "2.0.0" {
		t.Fatalf("version after overwrite = %q, want 2.0.0", after.Snapshot.Version)
	}
	if after.ConfigSchemaError != "" {
		t.Fatalf("ConfigSchemaError = %q, want empty", after.ConfigSchemaError)
	}
	properties, ok := after.ConfigSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %#v, want object", after.ConfigSchema["properties"])
	}
	if _, ok := properties["mode"]; !ok {
		t.Fatalf("schema properties = %#v, want mode property", after.ConfigSchema["properties"])
	}
}

func pluginManifestForUploadTest(t *testing.T, pluginID, version string) string {
	t.Helper()
	return fmt.Sprintf(`id: %s
name: Uploaded Demo
version: %s
description: uploaded plugin package
author: Go-bot
entry: %q
args:
  - "-test.run=^TestRuntimeExternalExecHelperProcess$"
protocol: stdio_jsonrpc
config_schema: ./config.schema.json
`, pluginID, version, os.Args[0])
}

func stubPythonEnvCommands(t *testing.T) func() {
	t.Helper()
	oldLookPath := pythonEnvLookPath
	oldRunCommand := pythonEnvRunCommand

	pythonEnvLookPath = func(name string) (string, error) {
		if name == "uv" {
			return "uv", nil
		}
		return "", os.ErrNotExist
	}
	pythonEnvRunCommand = func(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
		if name != "uv" {
			return nil, fmt.Errorf("unexpected command: %s", name)
		}
		if len(args) >= 2 && args[0] == "venv" {
			envDir := args[1]
			binDir := filepath.Join(envDir, pythonVenvScriptsDir())
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(binDir, pythonVenvExecutableName()), []byte(""), 0o755); err != nil {
				return nil, err
			}
			return []byte("created"), nil
		}
		if len(args) >= 5 && args[0] == "pip" && args[1] == "install" {
			return []byte("installed"), nil
		}
		return nil, fmt.Errorf("unexpected uv args: %v", args)
	}

	return func() {
		pythonEnvLookPath = oldLookPath
		pythonEnvRunCommand = oldRunCommand
	}
}

func pythonVenvScriptsDir() string {
	if os.PathSeparator == '\\' {
		return "Scripts"
	}
	return "bin"
}

func pythonVenvExecutableName() string {
	if os.PathSeparator == '\\' {
		return "python.exe"
	}
	return "python"
}

func buildPluginZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("writer.Create(%s) error = %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("entry.Write(%s) error = %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}
	return buf.Bytes()
}

func buildPluginTarGZ(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, content := range files {
		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("tarWriter.WriteHeader(%s) error = %v", name, err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatalf("tarWriter.Write(%s) error = %v", name, err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("tarWriter.Close() error = %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("gzipWriter.Close() error = %v", err)
	}
	return buf.Bytes()
}

func writeTestPythonCommonRuntime(commonDir string) error {
	if err := os.MkdirAll(filepath.Join(commonDir, "gobot_plugin"), 0o755); err != nil {
		return err
	}
	files := map[string]string{
		filepath.Join(commonDir, "gobot_runtime.py"):            "# runtime\n",
		filepath.Join(commonDir, "gobot_plugin", "__init__.py"): "# package init\n",
		filepath.Join(commonDir, "gobot_plugin", "models.py"):   "# models\n",
		filepath.Join(commonDir, "gobot_plugin", "runtime.py"):  "# package runtime\n",
		filepath.Join(commonDir, "gobot_plugin", "py.typed"):    "",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
