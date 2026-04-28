package botd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExternalPluginScaffold(t *testing.T) {
	root := t.TempDir()

	err := runExternalPluginScaffold([]string{
		"--id", "demo_echo",
		"--name", "Demo Echo",
		"--description", "demo external plugin",
		"--output", root,
	})
	if err != nil {
		t.Fatalf("runExternalPluginScaffold() error = %v", err)
	}

	targetDir := filepath.Join(root, "demo_echo")
	assertFileContains(t, filepath.Join(targetDir, "plugin.yaml"), "id: demo_echo")
	assertFileContains(t, filepath.Join(targetDir, "plugin.yaml"), "name: Demo Echo")
	assertFileContains(t, filepath.Join(targetDir, "plugin.yaml"), "description: demo external plugin")
	assertFileContains(t, filepath.Join(targetDir, "config.schema.json"), `"/demo_echo"`)
	assertFileContains(t, filepath.Join(targetDir, "requirements.txt"), `# Add runtime dependencies here.`)
	assertFileContains(t, filepath.Join(targetDir, "requirements-dev.txt"), `../../sdk/python`)
	assertFileContains(t, filepath.Join(targetDir, "main.py"), `from gobot_plugin import`)
	assertFileContains(t, filepath.Join(targetDir, "main.py"), `Demo Echo is alive`)
}

func TestRunExternalPluginScaffold_RejectsExistingDirWithoutForce(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "demo_echo")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	err := runExternalPluginScaffold([]string{
		"--id", "demo_echo",
		"--output", root,
	})
	if err == nil || !strings.Contains(err.Error(), "目标目录已存在") {
		t.Fatalf("runExternalPluginScaffold() error = %v, want existing dir error", err)
	}
}

func TestRunExternalPluginScaffold_ForceOverwrite(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "demo_echo")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "plugin.yaml"), []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := runExternalPluginScaffold([]string{
		"--id", "demo_echo",
		"--name", "Rebuilt Echo",
		"--force",
		"--output", root,
	})
	if err != nil {
		t.Fatalf("runExternalPluginScaffold() error = %v", err)
	}

	assertFileContains(t, filepath.Join(targetDir, "plugin.yaml"), "name: Rebuilt Echo")
}

func TestRunExternalPluginScaffold_InvalidID(t *testing.T) {
	root := t.TempDir()
	err := runExternalPluginScaffold([]string{
		"--id", "Bad Plugin",
		"--output", root,
	})
	if err == nil || !strings.Contains(err.Error(), "插件 ID 格式非法") {
		t.Fatalf("runExternalPluginScaffold() error = %v, want invalid id error", err)
	}
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	if !strings.Contains(string(payload), want) {
		t.Fatalf("%s = %s, want contains %q", path, string(payload), want)
	}
}
