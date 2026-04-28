package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_AllowsMinimalConfigWithDefaults(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	payload := `app:
  owner_qq: "123456789"

connections:
  - id: napcat-main
    enabled: true
    ingress:
      type: ws_server
      listen: ":8080"
    action:
      type: napcat_http
      base_url: http://127.0.0.1:3000
`
	if err := os.WriteFile(configPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.App.Name != "go-bot" {
		t.Fatalf("App.Name = %q, want go-bot", cfg.App.Name)
	}
	if cfg.Storage.Engine != "sqlite" {
		t.Fatalf("Storage.Engine = %q, want sqlite", cfg.Storage.Engine)
	}
	if cfg.Server.WebUI.Theme != WebUIThemePinkLight {
		t.Fatalf("Server.WebUI.Theme = %q, want %q", cfg.Server.WebUI.Theme, WebUIThemePinkLight)
	}
	if len(cfg.Connections) != 1 {
		t.Fatalf("Connections len = %d, want 1", len(cfg.Connections))
	}
	if got := cfg.Connections[0].Action.TimeoutMS; got != 10000 {
		t.Fatalf("Connections[0].Action.TimeoutMS = %d, want 10000", got)
	}
	if got := cfg.Connections[0].Platform; got != "onebot_v11" {
		t.Fatalf("Connections[0].Platform = %q, want onebot_v11", got)
	}
	if len(cfg.Plugins) != 0 {
		t.Fatalf("Plugins len = %d, want 0", len(cfg.Plugins))
	}
}

func TestLoad_IgnoresLegacyAdminAuthUsername(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	payload := `app:
  owner_qq: "123456789"

connections:
  - id: napcat-main
    enabled: true
    ingress:
      type: ws_server
      listen: ":8080"
    action:
      type: napcat_http
      base_url: http://127.0.0.1:3000

security:
  admin_auth:
    enabled: true
    username: admin
    password: secret
`
	if err := os.WriteFile(configPath, []byte(payload), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Security.AdminAuth.Enabled {
		t.Fatalf("AdminAuth.Enabled = false, want true")
	}
	if cfg.Security.AdminAuth.Password != "secret" {
		t.Fatalf("AdminAuth.Password = %q, want secret", cfg.Security.AdminAuth.Password)
	}
}

func TestLoadWithFallback_BootstrapsConfigWhenMissing(t *testing.T) {
	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir(root) error = %v", err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()

	cfg, path, err := LoadWithFallback("")
	if err != nil {
		t.Fatalf("LoadWithFallback() error = %v", err)
	}

	expectedPath := filepath.Join("configs", "config.yml")
	if path != expectedPath {
		t.Fatalf("LoadWithFallback() path = %q, want %q", path, expectedPath)
	}
	writtenPath := filepath.Join(root, expectedPath)
	if _, err := os.Stat(writtenPath); err != nil {
		t.Fatalf("Stat(%q) error = %v", writtenPath, err)
	}
	if cfg.App.Name != "go-bot" {
		t.Fatalf("App.Name = %q, want go-bot", cfg.App.Name)
	}
	if cfg.AI.Prompt.BotName != cfg.App.Name {
		t.Fatalf("AI.Prompt.BotName = %q, want %q", cfg.AI.Prompt.BotName, cfg.App.Name)
	}
	if len(cfg.Connections) != 0 {
		t.Fatalf("Connections len = %d, want 0", len(cfg.Connections))
	}
}

func TestLoadWithFallback_BootstrapsExplicitConfigPathWhenMissing(t *testing.T) {
	root := t.TempDir()
	explicitPath := filepath.Join(root, "custom", "bot.yml")

	cfg, path, err := LoadWithFallback(explicitPath)
	if err != nil {
		t.Fatalf("LoadWithFallback(%q) error = %v", explicitPath, err)
	}
	if path != explicitPath {
		t.Fatalf("LoadWithFallback() path = %q, want %q", path, explicitPath)
	}
	if _, err := os.Stat(explicitPath); err != nil {
		t.Fatalf("Stat(%q) error = %v", explicitPath, err)
	}
	if cfg.Server.Admin.Listen != ":8090" {
		t.Fatalf("Server.Admin.Listen = %q, want :8090", cfg.Server.Admin.Listen)
	}
}
