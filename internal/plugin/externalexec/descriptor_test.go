package externalexec

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultPluginRoot_UsesProjectRoot(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")

	got := DefaultPluginRoot(configPath)
	want := filepath.Join(root, "plugins")
	if got != want {
		t.Fatalf("DefaultPluginRoot() = %q, want %q", got, want)
	}
}

func TestDiscover_LoadsRelativeEntryAndDefaultProtocol(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	entryPath := filepath.Join(pluginDir, "run-helper")
	if err := os.WriteFile(entryPath, []byte("helper"), 0o755); err != nil {
		t.Fatalf("WriteFile(entry) error = %v", err)
	}
	schemaPath := filepath.Join(pluginDir, "schema.json")
	if err := os.WriteFile(schemaPath, []byte(`{"type":"object"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(schema) error = %v", err)
	}

	manifestPath := filepath.Join(pluginDir, "plugin.yaml")
	payload := []byte(`
id: demo_external
name: Demo External
version: 1.2.3
description: test plugin
author: tester
entry: ./run-helper
args:
  - --serve
config_schema: ./schema.json
`)
	if err := os.WriteFile(manifestPath, payload, 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.yaml) error = %v", err)
	}

	items, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Discover() count = %d, want 1", len(items))
	}

	got := items[0]
	if got.Manifest.ID != "demo_external" {
		t.Fatalf("manifest id = %q, want demo_external", got.Manifest.ID)
	}
	if got.Manifest.Kind != KindExternalExec {
		t.Fatalf("manifest kind = %q, want %q", got.Manifest.Kind, KindExternalExec)
	}
	if got.Manifest.Protocol != ProtocolStdioJSONRP {
		t.Fatalf("manifest protocol = %q, want %q", got.Manifest.Protocol, ProtocolStdioJSONRP)
	}
	if got.Manifest.Entry != entryPath {
		t.Fatalf("manifest entry = %q, want %q", got.Manifest.Entry, entryPath)
	}
	if got.Manifest.ConfigSchema != schemaPath {
		t.Fatalf("manifest config schema = %q, want %q", got.Manifest.ConfigSchema, schemaPath)
	}
	if got.Manifest.Source != pluginDir {
		t.Fatalf("manifest source = %q, want %q", got.Manifest.Source, pluginDir)
	}
	if got.WorkDir != pluginDir {
		t.Fatalf("work dir = %q, want %q", got.WorkDir, pluginDir)
	}
}

func TestDiscover_ExpandsExecutablePlaceholder(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	manifestPath := filepath.Join(pluginDir, "plugin.yaml")
	payload := []byte(`
id: demo_external
entry: ${GOBOT_EXECUTABLE}
args:
  - external-plugin
  - --plugin
  - menu_hint
`)
	if err := os.WriteFile(manifestPath, payload, 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.yaml) error = %v", err)
	}

	items, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Discover() count = %d, want 1", len(items))
	}

	executablePath, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable() error = %v", err)
	}
	if items[0].Manifest.Entry != executablePath {
		t.Fatalf("manifest entry = %q, want executable %q", items[0].Manifest.Entry, executablePath)
	}
	if len(items[0].Manifest.Args) != 3 || items[0].Manifest.Args[2] != "menu_hint" {
		t.Fatalf("manifest args = %#v, want external runner args", items[0].Manifest.Args)
	}
}

func TestDiscover_ResolvesPlatformLauncherVariant(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	entryBase := filepath.Join(pluginDir, "run")
	resolvedEntry := entryBase + ".sh"
	if runtime.GOOS == "windows" {
		resolvedEntry = entryBase + ".cmd"
	}
	if err := os.WriteFile(resolvedEntry, []byte("launcher"), 0o644); err != nil {
		t.Fatalf("WriteFile(entry) error = %v", err)
	}

	manifestPath := filepath.Join(pluginDir, "plugin.yaml")
	payload := []byte("\nid: demo_external\nentry: ./run\n")
	if err := os.WriteFile(manifestPath, payload, 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.yaml) error = %v", err)
	}

	items, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Discover() count = %d, want 1", len(items))
	}
	if items[0].Manifest.Entry != resolvedEntry {
		t.Fatalf("manifest entry = %q, want %q", items[0].Manifest.Entry, resolvedEntry)
	}
}

func TestDiscover_LoadsPythonRuntimeEntry(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	entryPath := filepath.Join(pluginDir, "main.py")
	if err := os.WriteFile(entryPath, []byte("print('hello')"), 0o644); err != nil {
		t.Fatalf("WriteFile(entry) error = %v", err)
	}

	manifestPath := filepath.Join(pluginDir, "plugin.yaml")
	payload := []byte("\nid: python_demo\nruntime: python\nentry: ./main.py\n")
	if err := os.WriteFile(manifestPath, payload, 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.yaml) error = %v", err)
	}

	items, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Discover() count = %d, want 1", len(items))
	}
	if items[0].Manifest.Runtime != RuntimePython {
		t.Fatalf("manifest runtime = %q, want %q", items[0].Manifest.Runtime, RuntimePython)
	}
	if items[0].Manifest.Entry != entryPath {
		t.Fatalf("manifest entry = %q, want %q", items[0].Manifest.Entry, entryPath)
	}
}

func TestDiscover_RejectsUnknownRuntime(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	entryPath := filepath.Join(pluginDir, "main.py")
	if err := os.WriteFile(entryPath, []byte("print('hello')"), 0o644); err != nil {
		t.Fatalf("WriteFile(entry) error = %v", err)
	}

	manifestPath := filepath.Join(pluginDir, "plugin.yaml")
	payload := []byte("\nid: bad_runtime\nruntime: ruby\nentry: ./main.py\n")
	if err := os.WriteFile(manifestPath, payload, 0o644); err != nil {
		t.Fatalf("WriteFile(plugin.yaml) error = %v", err)
	}

	_, err := Discover(root)
	if err == nil || !strings.Contains(err.Error(), "当前不支持 runtime") {
		t.Fatalf("Discover() error = %v, want unsupported runtime", err)
	}
}
