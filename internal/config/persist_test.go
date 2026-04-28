package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWritablePath(t *testing.T) {
	path := ResolveWritablePath(filepath.Join("configs", "config.example.yml"))
	if path != filepath.Join("configs", "config.yml") {
		t.Fatalf("path = %s, want %s", path, filepath.Join("configs", "config.yml"))
	}
}

func TestSaveCreatesBackupAndWritesYAML(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "configs", "config.yml")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(target, []byte("legacy: true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Save(target, testConfig())
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if result.TargetPath != target {
		t.Fatalf("TargetPath = %s, want %s", result.TargetPath, target)
	}
	if result.BackupPath == "" {
		t.Fatalf("BackupPath is empty")
	}

	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(target) error = %v", err)
	}
	text := string(written)
	if !strings.Contains(text, "app:") || !strings.Contains(text, "connections:") {
		t.Fatalf("written = %s, want yaml content", text)
	}
	if !strings.Contains(text, "password: secret") {
		t.Fatalf("written = %s, want raw password persisted", text)
	}
	if strings.Contains(text, "admin_auth:\n    username:") {
		t.Fatalf("written = %s, admin auth username field should be removed", text)
	}

	backup, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatalf("ReadFile(backup) error = %v", err)
	}
	if string(backup) != "legacy: true\n" {
		t.Fatalf("backup = %s, want legacy content", string(backup))
	}
}

func TestSaveFromExampleWritesConfigYML(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "configs", "config.example.yml")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(source, []byte("app:\n  name: example\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := Save(source, testConfig())
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	wantTarget := filepath.Join(root, "configs", "config.yml")
	if result.TargetPath != wantTarget {
		t.Fatalf("TargetPath = %s, want %s", result.TargetPath, wantTarget)
	}
	if _, err := os.Stat(wantTarget); err != nil {
		t.Fatalf("saved config not found: %v", err)
	}
	if result.BackupPath != "" {
		t.Fatalf("BackupPath = %s, want empty when target did not exist", result.BackupPath)
	}
}

func TestSaveNormalizesConnectionTimeout(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "configs", "config.yml")

	cfg := testConfig()
	cfg.Connections[0].Action.TimeoutMS = 0

	result, err := Save(target, cfg)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if result.TargetPath != target {
		t.Fatalf("TargetPath = %s, want %s", result.TargetPath, target)
	}

	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile(target) error = %v", err)
	}
	if got := string(written); !strings.Contains(got, "timeout_ms: 10000") {
		t.Fatalf("written = %s, want normalized timeout_ms", got)
	}
}

func testConfig() *Config {
	return &Config{
		App: AppConfig{
			Name:     "go-bot",
			Env:      "dev",
			OwnerQQ:  "123456789",
			DataDir:  "./data",
			LogLevel: "info",
		},
		Server: ServerConfig{
			Admin: AdminServerConfig{
				Enabled: true,
				Listen:  ":8090",
			},
			WebUI: WebUIConfig{
				Enabled:  true,
				BasePath: "/",
			},
		},
		Storage: StorageConfig{
			Engine: "sqlite",
			SQLite: SQLiteConfig{Path: "./data/app.db"},
			Logs: LogsConfig{
				Dir:        "./data/logs",
				MaxSizeMB:  50,
				MaxBackups: 7,
				MaxAgeDays: 30,
			},
			Media: MediaConfig{
				Enabled:                true,
				Backend:                "local",
				MaxSizeMB:              64,
				DownloadTimeoutSeconds: 20,
				Local: MediaLocalConfig{
					Dir: "./data/media",
				},
				R2: MediaR2Config{
					KeyPrefix: "media",
				},
			},
		},
		Connections: []ConnectionConfig{
			{
				ID:       "napcat-main",
				Enabled:  true,
				Platform: "onebot_v11",
				Ingress: IngressConfig{
					Type:   "ws_server",
					Listen: ":8080",
				},
				Action: ActionConfig{
					Type:        "napcat_http",
					BaseURL:     "http://127.0.0.1:3000",
					TimeoutMS:   10000,
					AccessToken: "",
				},
			},
		},
		Plugins: []PluginConfig{
			{
				ID:      "menu_hint",
				Kind:    "builtin",
				Enabled: true,
				Config: map[string]any{
					"header_text": "菜单",
				},
			},
		},
		Security: SecurityConfig{
			AdminAuth: AdminAuthConfig{
				Enabled:  false,
				Password: "secret",
			},
		},
	}
}
