package runtime

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/plugin/host"
)

func TestBuildGroupDisplayHint(t *testing.T) {
	item, ok := buildGroupDisplayHint("napcat-main", adapter.GroupInfo{
		GroupID:   "10001",
		GroupName: "春日研究会",
	})
	if !ok {
		t.Fatalf("buildGroupDisplayHint() ok = false, want true")
	}
	if item.ConnectionID != "napcat-main" || item.ChatType != "group" || item.GroupID != "10001" || item.GroupName != "春日研究会" {
		t.Fatalf("buildGroupDisplayHint() = %+v, want populated group display hint", item)
	}

	if _, ok := buildGroupDisplayHint("napcat-main", adapter.GroupInfo{GroupID: "10001"}); ok {
		t.Fatalf("buildGroupDisplayHint() ok = true, want false when group name is missing")
	}
}

func TestBuildPrivateDisplayHint(t *testing.T) {
	item, ok := buildPrivateDisplayHint("napcat-main", adapter.UserInfo{
		UserID:   "20002",
		Nickname: "Sakura",
	})
	if !ok {
		t.Fatalf("buildPrivateDisplayHint() ok = false, want true")
	}
	if item.ConnectionID != "napcat-main" || item.ChatType != "private" || item.UserID != "20002" {
		t.Fatalf("buildPrivateDisplayHint() = %+v, want private display hint scope", item)
	}
	if item.SenderRole != "user" || item.SenderName != "Sakura" || item.SenderNickname != "Sakura" {
		t.Fatalf("buildPrivateDisplayHint() = %+v, want nickname persisted", item)
	}

	if _, ok := buildPrivateDisplayHint("napcat-main", adapter.UserInfo{UserID: "20002"}); ok {
		t.Fatalf("buildPrivateDisplayHint() ok = true, want false when nickname is missing")
	}
}

func TestServiceAuditLogs_ReturnsNewestFirst(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	service.RecordAuditLog(AuditLogEntry{
		At:       time.Unix(1710000000, 0),
		Category: "plugin",
		Action:   "start",
		Target:   "test",
		Result:   "success",
		Summary:  "启动插件成功",
	})
	service.RecordAuditLog(AuditLogEntry{
		At:       time.Unix(1710000005, 0),
		Category: "plugin",
		Action:   "recover",
		Target:   "test",
		Result:   "success",
		Summary:  "恢复插件成功",
	})

	items := service.AuditLogs(1)
	if len(items) != 1 {
		t.Fatalf("AuditLogs(1) len = %d, want 1", len(items))
	}
	if items[0].Action != "recover" {
		t.Fatalf("AuditLogs(1)[0].Action = %q, want recover", items[0].Action)
	}

	allItems := service.AuditLogs(10)
	if len(allItems) != 2 {
		t.Fatalf("AuditLogs(10) len = %d, want 2", len(allItems))
	}
	if allItems[0].Action != "recover" || allItems[1].Action != "start" {
		t.Fatalf("AuditLogs order = %+v, want newest first", allItems)
	}
}

func TestServiceMetadata_DisablesAIMessageLogWhenStoreUnavailable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()
	cfg.Storage.Engine = "sqlite"
	cfg.Storage.SQLite.Path = ""

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := service.Metadata().Capabilities["ai_message_log"]; got {
		t.Fatalf("ai_message_log capability = %v, want false when store is unavailable", got)
	}
	if snapshot := service.AIView().Snapshot; snapshot.StoreReady {
		t.Fatalf("AI snapshot = %+v, want store_ready=false", snapshot)
	}
}

func TestServiceNew_PreparesRuntimeDirectories(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	root := t.TempDir()
	configPath := filepath.Join(root, "configs", "config.example.yml")

	service, err := New(testRuntimeConfig(), configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	configDir := filepath.Dir(config.ResolveWritablePath(configPath))
	configDirAbs, err := filepath.Abs(configDir)
	if err != nil {
		t.Fatalf("Abs(config dir) error = %v", err)
	}
	if info, err := os.Stat(configDirAbs); err != nil {
		t.Fatalf("Stat(config dir) error = %v", err)
	} else if !info.IsDir() {
		t.Fatalf("config dir = %q, want directory", configDirAbs)
	}

	expectedPluginDir := filepath.Join(root, "plugins")
	expectedPluginDirAbs, err := filepath.Abs(expectedPluginDir)
	if err != nil {
		t.Fatalf("Abs(plugin dir) error = %v", err)
	}
	if info, err := os.Stat(expectedPluginDirAbs); err != nil {
		t.Fatalf("Stat(plugin dir) error = %v", err)
	} else if !info.IsDir() {
		t.Fatalf("plugin dir = %q, want directory", expectedPluginDirAbs)
	}

	servicePluginDirAbs, err := filepath.Abs(service.externalRoot)
	if err != nil {
		t.Fatalf("Abs(service.externalRoot) error = %v", err)
	}
	if servicePluginDirAbs != expectedPluginDirAbs {
		t.Fatalf("service.externalRoot = %q, want %q", servicePluginDirAbs, expectedPluginDirAbs)
	}

	expectedSkillDir := filepath.Join(root, "data", "skills")
	expectedSkillDirAbs, err := filepath.Abs(expectedSkillDir)
	if err != nil {
		t.Fatalf("Abs(skill dir) error = %v", err)
	}
	if info, err := os.Stat(expectedSkillDirAbs); err != nil {
		t.Fatalf("Stat(skill dir) error = %v", err)
	} else if !info.IsDir() {
		t.Fatalf("skill dir = %q, want directory", expectedSkillDirAbs)
	}
	if service.skillStore == nil {
		t.Fatalf("service.skillStore = nil, want initialized store")
	}
}

func TestServiceSavePluginConfig_HotAppliesPluginChanges(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()
	cfg.Plugins = []config.PluginConfig{{
		ID:      "test",
		Kind:    "builtin",
		Enabled: false,
		Config:  map[string]any{},
	}}

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.state = StateRunning

	result, err := service.SavePluginConfig(context.Background(), "test", true, map[string]any{})
	if err != nil {
		t.Fatalf("SavePluginConfig() error = %v", err)
	}

	if result.RestartRequired {
		t.Fatalf("RestartRequired = true, want false for plugin-only hot apply")
	}
	if !result.HotApplied || result.HotApplyError != "" {
		t.Fatalf("hot apply flags = applied:%v err:%q, want true/empty", result.HotApplied, result.HotApplyError)
	}
	if !strings.Contains(result.Message, "热应用") {
		t.Fatalf("Message = %q, want mention hot apply", result.Message)
	}

	detail, ok := service.PluginDetail("test")
	if !ok {
		t.Fatalf("PluginDetail() ok = false, want true")
	}
	if detail.Snapshot.State != host.PluginRunning {
		t.Fatalf("plugin state = %s, want running", detail.Snapshot.State)
	}
	if !detail.Snapshot.Enabled {
		t.Fatalf("plugin enabled = false, want true")
	}

	if len(service.cfg.Plugins) != 1 || !service.cfg.Plugins[0].Enabled {
		t.Fatalf("service cfg plugins = %+v, want enabled test plugin", service.cfg.Plugins)
	}

	targetPath := config.ResolveWritablePath(configPath)
	payload, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", targetPath, err)
	}
	if !strings.Contains(string(payload), "enabled: true") {
		t.Fatalf("saved config = %s, want enabled: true", string(payload))
	}
}

func TestServiceStartStopPlugin_PersistsConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.state = StateRunning

	if err := service.StartPlugin(context.Background(), "test"); err != nil {
		t.Fatalf("StartPlugin() error = %v", err)
	}

	startDetail, ok := service.PluginDetail("test")
	if !ok {
		t.Fatalf("PluginDetail(start) ok = false, want true")
	}
	if startDetail.Snapshot.State != host.PluginRunning {
		t.Fatalf("plugin state after start = %s, want running", startDetail.Snapshot.State)
	}
	if !startDetail.Snapshot.Enabled || !startDetail.Snapshot.Configured {
		t.Fatalf("snapshot after start = %+v, want enabled and configured", startDetail.Snapshot)
	}

	if err := service.StopPlugin(context.Background(), "test"); err != nil {
		t.Fatalf("StopPlugin() error = %v", err)
	}

	stopDetail, ok := service.PluginDetail("test")
	if !ok {
		t.Fatalf("PluginDetail(stop) ok = false, want true")
	}
	if stopDetail.Snapshot.State != host.PluginStopped {
		t.Fatalf("plugin state after stop = %s, want stopped", stopDetail.Snapshot.State)
	}
	if stopDetail.Snapshot.Enabled {
		t.Fatalf("plugin enabled after stop = true, want false")
	}

	targetPath := config.ResolveWritablePath(configPath)
	payload, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", targetPath, err)
	}
	text := string(payload)
	if !strings.Contains(text, "id: test") || !strings.Contains(text, "enabled: false") {
		t.Fatalf("saved config = %s, want persisted stopped plugin", text)
	}
}

func TestServiceBuiltinPluginInventory_IsNormalized(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()
	cfg.Plugins = []config.PluginConfig{{
		ID:      "menu_hint",
		Kind:    "builtin",
		Enabled: true,
		Config:  map[string]any{"header_text": "旧菜单"},
	}}

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	detail, ok := service.PluginDetail("test")
	if !ok {
		t.Fatalf("PluginDetail(test) ok = false, want true")
	}
	if detail.Snapshot.Configured || detail.Snapshot.Enabled {
		t.Fatalf("builtin snapshot = %+v, want configured=false enabled=false", detail.Snapshot)
	}
	if detail.Snapshot.Kind != "builtin" {
		t.Fatalf("builtin kind = %q, want builtin", detail.Snapshot.Kind)
	}

	if _, ok := service.PluginDetail("menu_hint"); ok {
		t.Fatalf("PluginDetail(menu_hint) ok = true, want false after dropping unsupported builtin")
	}

	if len(service.cfg.Plugins) != 0 {
		t.Fatalf("normalized plugins = %+v, want unsupported builtin removed without auto-append", service.cfg.Plugins)
	}
}

func TestServiceSavePluginConfig_AppliesSchemaDefaults(t *testing.T) {
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
		"uploaded-demo/plugin.yaml": pluginManifestForUploadTest(t, "uploaded_demo", "1.0.0"),
		"uploaded-demo/config.schema.json": `{
  "type": "object",
  "properties": {
    "request_timeout_ms": { "type": "integer", "minimum": 1, "default": 15000 },
    "mode": { "type": "string", "default": "lite" }
  }
}`,
	})
	if _, err := service.InstallPluginPackage(context.Background(), "uploaded_demo.zip", payload, false); err != nil {
		t.Fatalf("InstallPluginPackage() error = %v", err)
	}

	result, err := service.SavePluginConfig(context.Background(), "uploaded_demo", true, map[string]any{})
	if err != nil {
		t.Fatalf("SavePluginConfig() error = %v", err)
	}
	if !result.Persisted {
		t.Fatalf("Persisted = false, want true")
	}

	detail, ok := service.PluginDetail("uploaded_demo")
	if !ok {
		t.Fatalf("PluginDetail() ok = false, want true")
	}
	if got := detail.Config["mode"]; got != "lite" {
		t.Fatalf("detail config mode = %#v, want lite", got)
	}
	switch got := detail.Config["request_timeout_ms"].(type) {
	case int:
		if got != 15000 {
			t.Fatalf("detail config request_timeout_ms = %d, want 15000", got)
		}
	case int64:
		if got != 15000 {
			t.Fatalf("detail config request_timeout_ms = %d, want 15000", got)
		}
	case float64:
		if got != 15000 {
			t.Fatalf("detail config request_timeout_ms = %v, want 15000", got)
		}
	default:
		t.Fatalf("detail config request_timeout_ms type = %T, want integer", got)
	}

	savedPath := config.ResolveWritablePath(configPath)
	written, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("ReadFile(saved config) error = %v", err)
	}
	text := string(written)
	if !strings.Contains(text, "mode: lite") || !strings.Contains(text, "request_timeout_ms: 15000") {
		t.Fatalf("saved config = %s, want expanded schema defaults", text)
	}
}

func TestServiceSavePluginConfig_RejectsMissingRequiredSchemaWhenEnabling(t *testing.T) {
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
		"uploaded-demo/plugin.yaml": pluginManifestForUploadTest(t, "uploaded_demo", "1.0.0"),
		"uploaded-demo/config.schema.json": `{
  "type": "object",
  "properties": {
    "api_key": { "type": "string", "title": "接口密钥" }
  },
  "required": ["api_key"]
}`,
	})
	if _, err := service.InstallPluginPackage(context.Background(), "uploaded_demo.zip", payload, false); err != nil {
		t.Fatalf("InstallPluginPackage() error = %v", err)
	}

	if _, err := service.SavePluginConfig(context.Background(), "uploaded_demo", true, map[string]any{}); err == nil || !strings.Contains(err.Error(), "接口密钥 不能为空") {
		t.Fatalf("SavePluginConfig() error = %v, want required schema validation", err)
	}
}

func TestServiceBuiltinPlugin_CannotInstallOrUninstall(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := service.InstallPlugin(context.Background(), "test"); err == nil || !strings.Contains(err.Error(), "内置插件不支持安装") {
		t.Fatalf("InstallPlugin() error = %v, want builtin install rejected", err)
	}

	if err := service.UninstallPlugin(context.Background(), "test"); err == nil || !strings.Contains(err.Error(), "内置插件不支持卸载") {
		t.Fatalf("UninstallPlugin() error = %v, want builtin uninstall rejected", err)
	}

	detail, ok := service.PluginDetail("test")
	if !ok {
		t.Fatalf("PluginDetail() ok = false, want true")
	}
	if detail.Snapshot.Configured || detail.Snapshot.Kind != "builtin" {
		t.Fatalf("snapshot after rejected actions = %+v, want unconfigured builtin preserved", detail.Snapshot)
	}
}

func TestServiceSavePluginConfig_RejectsUnknownPlugin(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.state = StateRunning

	_, err = service.SavePluginConfig(context.Background(), "missing_plugin", true, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "插件未注册: missing_plugin") {
		t.Fatalf("SavePluginConfig() error = %v, want plugin not registered", err)
	}
}

func TestServiceSaveConfig_PreservesPluginConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()
	cfg.Plugins = []config.PluginConfig{{
		ID:      "test",
		Kind:    "builtin",
		Enabled: true,
		Config:  map[string]any{"header_text": "旧菜单"},
	}}

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	nextCfg, err := config.Clone(cfg)
	if err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	nextCfg.App.Name = "go-bot-next"
	nextCfg.Plugins[0].Enabled = false
	nextCfg.Plugins[0].Config["header_text"] = "新菜单"

	result, err := service.SaveConfig(context.Background(), nextCfg)
	if err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if result.PluginChanged {
		t.Fatalf("PluginChanged = true, want false when system config save should preserve plugin config")
	}
	if !result.NonPluginChanged {
		t.Fatalf("NonPluginChanged = false, want true")
	}
	if got := service.cfg.App.Name; got != "go-bot-next" {
		t.Fatalf("service cfg app name = %q, want go-bot-next", got)
	}

	detail, ok := service.PluginDetail("test")
	if !ok {
		t.Fatalf("PluginDetail() ok = false, want true")
	}
	if !detail.Snapshot.Enabled {
		t.Fatalf("plugin enabled = false, want preserved true")
	}
	if got := detail.Config["header_text"]; got != "旧菜单" {
		t.Fatalf("plugin config header_text = %v, want 旧菜单", got)
	}
}

func TestServiceSaveConfig_PreservesConnections(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	nextCfg := testRuntimeConfig()
	nextCfg.App.Name = "go-bot-next"
	nextCfg.Connections[0].Action.BaseURL = "http://127.0.0.1:3999"
	nextCfg.Connections[0].Ingress.Listen = ":18888"
	nextCfg.Connections[0].Ingress.Path = "/changed"

	result, err := service.SaveConfig(context.Background(), nextCfg)
	if err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if !result.NonPluginChanged {
		t.Fatalf("NonPluginChanged = false, want true")
	}
	if len(service.cfg.Connections) != 1 {
		t.Fatalf("connections len = %d, want 1", len(service.cfg.Connections))
	}
	if !service.cfg.Connections[0].Enabled {
		t.Fatalf("connection enabled = false, want preserved true")
	}
	if got := service.cfg.Connections[0].Action.BaseURL; got != "http://127.0.0.1:3000" {
		t.Fatalf("action base url = %q, want preserved original", got)
	}
	if got := service.cfg.Connections[0].Ingress.Listen; got != ":0" {
		t.Fatalf("ingress listen = %q, want preserved original", got)
	}
}

func TestServiceSaveConfig_HotAppliesAIStorageConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	root := t.TempDir()
	cfg := testRuntimeConfig()
	cfg.Storage.Engine = "sqlite"
	cfg.Storage.SQLite.Path = filepath.Join(root, "initial.db")
	cfg.Storage.Logs.Dir = filepath.Join(root, "logs")
	configPath := filepath.Join(root, "config.yml")

	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() {
		if service.aiService != nil {
			_ = service.aiService.Close()
		}
		if service.mediaService != nil {
			_ = service.mediaService.Close()
		}
	}()

	nextCfg := *cfg
	nextCfg.Storage.SQLite.Path = filepath.Join(root, "next.db")
	result, err := service.SaveConfig(context.Background(), &nextCfg)
	if err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if !result.HotApplyAttempted {
		t.Fatalf("HotApplyAttempted = false, want storage hot apply attempted")
	}
	if result.HotApplyError != "" {
		t.Fatalf("HotApplyError = %q, want empty", result.HotApplyError)
	}
	if got := service.aiService.Snapshot().LastDecisionReason; got != "AI 存储配置已热更新" {
		t.Fatalf("LastDecisionReason = %q, want AI storage hot update", got)
	}
}

func TestServiceSaveConfig_PreservesRedactedSensitiveFields(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()
	cfg.Storage.Engine = "mysql"
	cfg.Storage.MySQL.Host = "127.0.0.1"
	cfg.Storage.MySQL.Port = 3306
	cfg.Storage.MySQL.Username = "root"
	cfg.Storage.MySQL.Password = "mysql-secret"
	cfg.Storage.MySQL.Database = "go_bot"
	cfg.Storage.Media.Backend = "r2"
	cfg.Storage.Media.R2.SecretAccessKey = "r2-secret"
	passwordHash, err := config.HashAdminPassword("secret123")
	if err != nil {
		t.Fatalf("HashAdminPassword() error = %v", err)
	}
	cfg.Security.AdminAuth.Enabled = true
	cfg.Security.AdminAuth.Password = passwordHash

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	raw := config.SanitizedMap(cfg)
	appRaw, _ := raw["app"].(map[string]any)
	if appRaw == nil {
		t.Fatalf("sanitized app config missing")
	}
	appRaw["name"] = "go-bot-next"
	storageRaw, _ := raw["storage"].(map[string]any)
	mysqlRaw, _ := storageRaw["mysql"].(map[string]any)
	mediaRaw, _ := storageRaw["media"].(map[string]any)
	r2Raw, _ := mediaRaw["r2"].(map[string]any)
	securityRaw, _ := raw["security"].(map[string]any)
	adminAuthRaw, _ := securityRaw["admin_auth"].(map[string]any)
	mysqlRaw["password"] = "******"
	r2Raw["secret_access_key"] = "******"
	adminAuthRaw["password"] = "******"

	draft, err := config.DecodeDraftMap(raw)
	if err != nil {
		t.Fatalf("DecodeDraftMap() error = %v", err)
	}

	if _, err := service.SaveConfig(context.Background(), draft); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if got := service.cfg.App.Name; got != "go-bot-next" {
		t.Fatalf("service cfg app name = %q, want go-bot-next", got)
	}
	if got := service.cfg.AI.Prompt.BotName; got != "go-bot-next" {
		t.Fatalf("service cfg ai bot name = %q, want synced app name", got)
	}
	if got := service.cfg.Storage.MySQL.Password; got != "mysql-secret" {
		t.Fatalf("service cfg mysql password = %q, want preserved secret", got)
	}
	if got := service.cfg.Storage.Media.R2.SecretAccessKey; got != "r2-secret" {
		t.Fatalf("service cfg r2 secret = %q, want preserved secret", got)
	}
	if !config.VerifyAdminPassword(service.cfg.Security.AdminAuth.Password, "secret123") {
		t.Fatalf("admin password hash should be preserved after saving sanitized draft")
	}

	targetPath := config.ResolveWritablePath(configPath)
	savedPayload, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", targetPath, err)
	}
	text := string(savedPayload)
	if !strings.Contains(text, "password: mysql-secret") {
		t.Fatalf("saved config = %s, want mysql secret persisted", text)
	}
	if !strings.Contains(text, "secret_access_key: r2-secret") {
		t.Fatalf("saved config = %s, want r2 secret persisted", text)
	}
	if !strings.Contains(text, passwordHash) {
		t.Fatalf("saved config = %s, want hashed admin password persisted", text)
	}
	if !strings.Contains(text, "bot_name: go-bot-next") {
		t.Fatalf("saved config = %s, want ai bot name synced with app name", text)
	}
}

func TestServiceSaveWebUITheme_PersistsWithoutRestart(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.state = StateRunning

	result, err := service.SaveWebUITheme(context.Background(), config.WebUIThemeBlueLight)
	if err != nil {
		t.Fatalf("SaveWebUITheme() error = %v", err)
	}
	if result.RestartRequired {
		t.Fatalf("RestartRequired = true, want false for webui theme save")
	}
	if result.Message == "" || !strings.Contains(result.Message, "WebUI 主题已保存") {
		t.Fatalf("Message = %q, want webui theme saved message", result.Message)
	}
	if got := service.cfg.Server.WebUI.Theme; got != config.WebUIThemeBlueLight {
		t.Fatalf("theme = %q, want %q", got, config.WebUIThemeBlueLight)
	}
	if got := service.Metadata().WebUITheme; got != config.WebUIThemeBlueLight {
		t.Fatalf("metadata theme = %q, want %q", got, config.WebUIThemeBlueLight)
	}

	targetPath := config.ResolveWritablePath(configPath)
	payload, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", targetPath, err)
	}
	if !strings.Contains(string(payload), "theme: blue-light") {
		t.Fatalf("saved config = %s, want theme persisted", string(payload))
	}
}

func TestServiceHotRestart_ReloadsSavedConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	if _, err := config.Save(configPath, cfg); err != nil {
		t.Fatalf("Save(initial) error = %v", err)
	}

	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		_ = service.Stop(context.Background())
	}()

	service.RecordAuditLog(AuditLogEntry{
		Category: "config",
		Action:   "save",
		Target:   "system",
		Result:   "success",
		Summary:  "保存成功",
	})

	nextCfg, err := config.Clone(cfg)
	if err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	nextCfg.App.Name = "go-bot-restarted"
	nextCfg.Storage.SQLite.Path = "./data/restarted.db"
	if _, err := config.Save(configPath, nextCfg); err != nil {
		t.Fatalf("Save(updated) error = %v", err)
	}

	result, err := service.HotRestart(context.Background())
	if err != nil {
		t.Fatalf("HotRestart() error = %v", err)
	}
	if !result.Accepted || !result.Restarted {
		t.Fatalf("result = %+v, want accepted and restarted", result)
	}
	if result.State != StateRunning {
		t.Fatalf("state = %s, want running", result.State)
	}
	if got := service.Metadata().AppName; got != "go-bot-restarted" {
		t.Fatalf("metadata app name = %q, want go-bot-restarted", got)
	}
	if got := service.cfg.Storage.SQLite.Path; got != "./data/restarted.db" {
		t.Fatalf("sqlite path = %q, want ./data/restarted.db", got)
	}
	if logs := service.AuditLogs(10); len(logs) != 1 {
		t.Fatalf("audit logs len = %d, want 1", len(logs))
	}
}

func TestServiceSaveConnectionConfig_AddsConnection(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	conn := config.ConnectionConfig{
		ID:       "napcat-secondary",
		Enabled:  true,
		Platform: "onebot_v11",
		Ingress: config.IngressConfig{
			Type:   "http_callback",
			Listen: ":18081",
			Path:   "/callback",
		},
		Action: config.ActionConfig{
			Type:      "napcat_http",
			BaseURL:   "http://127.0.0.1:3001",
			TimeoutMS: 5000,
		},
	}

	result, err := service.SaveConnectionConfig(context.Background(), conn)
	if err != nil {
		t.Fatalf("SaveConnectionConfig() error = %v", err)
	}
	if result.ConnectionID != "napcat-secondary" {
		t.Fatalf("ConnectionID = %q, want napcat-secondary", result.ConnectionID)
	}
	if result.RestartRequired {
		t.Fatalf("RestartRequired = true, want false when runtime is stopped")
	}
	if len(service.cfg.Connections) != 2 {
		t.Fatalf("connections len = %d, want 2", len(service.cfg.Connections))
	}

	detail, ok := service.ConnectionDetail("napcat-secondary")
	if !ok {
		t.Fatalf("ConnectionDetail() ok = false, want true")
	}
	if detail.Snapshot.IngressType != "http_callback" {
		t.Fatalf("ingress type = %q, want http_callback", detail.Snapshot.IngressType)
	}

	targetPath := config.ResolveWritablePath(configPath)
	payload, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", targetPath, err)
	}
	if !strings.Contains(string(payload), "id: napcat-secondary") {
		t.Fatalf("saved config = %s, want new connection entry", string(payload))
	}
}

func TestServiceSaveConnectionConfig_UpdatesExistingConnection(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	conn := config.ConnectionConfig{
		ID:       "napcat-main",
		Enabled:  true,
		Platform: "onebot_v11",
		Ingress: config.IngressConfig{
			Type:   "ws_server",
			Listen: ":19090",
			Path:   "/gateway",
		},
		Action: config.ActionConfig{
			Type:        "napcat_http",
			BaseURL:     "http://127.0.0.1:3900",
			TimeoutMS:   8000,
			AccessToken: "secret-token",
		},
	}

	result, err := service.SaveConnectionConfig(context.Background(), conn)
	if err != nil {
		t.Fatalf("SaveConnectionConfig() error = %v", err)
	}
	if result.ConnectionID != "napcat-main" {
		t.Fatalf("ConnectionID = %q, want napcat-main", result.ConnectionID)
	}
	if len(service.cfg.Connections) != 1 {
		t.Fatalf("connections len = %d, want 1", len(service.cfg.Connections))
	}
	if !service.cfg.Connections[0].Enabled {
		t.Fatalf("enabled = false, want true")
	}
	if got := service.cfg.Connections[0].Ingress.Path; got != "/gateway" {
		t.Fatalf("ingress path = %q, want /gateway", got)
	}
	if got := service.cfg.Connections[0].Action.AccessToken; got != "secret-token" {
		t.Fatalf("access token = %q, want secret-token", got)
	}

	detail, ok := service.ConnectionDetail("napcat-main")
	if !ok {
		t.Fatalf("ConnectionDetail() ok = false, want true")
	}
	if !detail.Snapshot.Enabled {
		t.Fatalf("snapshot enabled = false, want true")
	}
	if detail.Snapshot.IngressType != "ws_server" {
		t.Fatalf("snapshot ingress type = %q, want ws_server", detail.Snapshot.IngressType)
	}
}

func TestServiceSaveConnectionConfig_AllowsWSActionWithoutBaseURL(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	conn := config.ConnectionConfig{
		ID:       "napcat-main",
		Enabled:  true,
		Platform: "onebot_v11",
		Ingress: config.IngressConfig{
			Type:   "ws_server",
			Listen: ":19090",
			Path:   "/gateway",
		},
		Action: config.ActionConfig{
			Type:      config.ActionTypeOneBotWS,
			TimeoutMS: 8000,
		},
	}

	result, err := service.SaveConnectionConfig(context.Background(), conn)
	if err != nil {
		t.Fatalf("SaveConnectionConfig() error = %v", err)
	}
	if result.ConnectionID != "napcat-main" {
		t.Fatalf("ConnectionID = %q, want napcat-main", result.ConnectionID)
	}
	if got := service.cfg.Connections[0].Action.Type; got != config.ActionTypeOneBotWS {
		t.Fatalf("action type = %q, want %q", got, config.ActionTypeOneBotWS)
	}
	if got := service.cfg.Connections[0].Action.BaseURL; got != "" {
		t.Fatalf("action base url = %q, want empty", got)
	}
}

func TestServiceSaveConnectionConfig_PreservesRedactedAccessToken(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()
	cfg.Connections[0].Action.AccessToken = "secret-token"

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	conn := config.ConnectionConfig{
		ID:       "napcat-main",
		Enabled:  true,
		Platform: "onebot_v11",
		Ingress: config.IngressConfig{
			Type:   "ws_server",
			Listen: ":19091",
			Path:   "/gateway",
		},
		Action: config.ActionConfig{
			Type:        config.ActionTypeOneBotWS,
			TimeoutMS:   6000,
			AccessToken: "******",
		},
	}

	if _, err := service.SaveConnectionConfig(context.Background(), conn); err != nil {
		t.Fatalf("SaveConnectionConfig() error = %v", err)
	}
	if got := service.cfg.Connections[0].Action.AccessToken; got != "secret-token" {
		t.Fatalf("access token = %q, want preserved secret-token", got)
	}
}

func TestServiceSaveConnectionConfig_RunningWSActionDoesNotBlockWhenSessionNotReady(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()
	cfg.Connections[0].Ingress.Listen = ":0"

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	service.state = StateRunning
	service.runCtx = runCtx
	service.cancel = cancel
	defer func() {
		_ = service.Stop(context.Background())
	}()

	conn := config.ConnectionConfig{
		ID:       "napcat-main",
		Enabled:  true,
		Platform: "onebot_v11",
		Ingress: config.IngressConfig{
			Type:   "ws_server",
			Listen: ":0",
			Path:   "/gateway",
		},
		Action: config.ActionConfig{
			Type:      config.ActionTypeOneBotWS,
			TimeoutMS: 10000,
		},
	}

	startedAt := time.Now()
	result, err := service.SaveConnectionConfig(context.Background(), conn)
	elapsed := time.Since(startedAt)
	if err != nil {
		t.Fatalf("SaveConnectionConfig() error = %v", err)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("SaveConnectionConfig() elapsed = %s, want < 2s", elapsed)
	}
	if result.ConnectionID != "napcat-main" {
		t.Fatalf("ConnectionID = %q, want napcat-main", result.ConnectionID)
	}
	if result.RestartRequired {
		t.Fatalf("RestartRequired = true, want false")
	}
	if !result.HotApplied || result.HotApplyError != "" {
		t.Fatalf("hot apply flags = applied:%v err:%q, want true/empty", result.HotApplied, result.HotApplyError)
	}
	if result.Detail.Snapshot.State != adapter.ConnectionRunning {
		t.Fatalf("snapshot state = %s, want running", result.Detail.Snapshot.State)
	}
	if result.Detail.Snapshot.IngressState != adapter.ConnectionRunning {
		t.Fatalf("snapshot ingress state = %s, want running", result.Detail.Snapshot.IngressState)
	}
	if result.Detail.Snapshot.LastError != "" {
		t.Fatalf("snapshot last error = %q, want empty", result.Detail.Snapshot.LastError)
	}
	if result.Detail.Snapshot.Online {
		t.Fatalf("snapshot online = true, want false before websocket session is ready")
	}
}

func TestServiceSetConnectionEnabled_TogglesExistingConnection(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()
	cfg.Connections[0].Enabled = true

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := service.SetConnectionEnabled(context.Background(), "napcat-main", false)
	if err != nil {
		t.Fatalf("SetConnectionEnabled() error = %v", err)
	}
	if result.ConnectionID != "napcat-main" {
		t.Fatalf("ConnectionID = %q, want napcat-main", result.ConnectionID)
	}
	if result.RestartRequired {
		t.Fatalf("RestartRequired = true, want false when runtime is stopped")
	}
	if service.cfg.Connections[0].Enabled {
		t.Fatalf("connection enabled = true, want false")
	}
	if result.Detail.Snapshot.Enabled {
		t.Fatalf("snapshot enabled = true, want false")
	}

	targetPath := config.ResolveWritablePath(configPath)
	payload, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", targetPath, err)
	}
	if !strings.Contains(string(payload), "enabled: false") {
		t.Fatalf("saved config = %s, want enabled: false", string(payload))
	}
}

func TestServiceDeleteConnection_RemovesConnection(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()
	cfg.Connections = append(cfg.Connections, config.ConnectionConfig{
		ID:       "napcat-secondary",
		Enabled:  true,
		Platform: "onebot_v11",
		Ingress: config.IngressConfig{
			Type:   "http_callback",
			Listen: ":18082",
			Path:   "/callback",
		},
		Action: config.ActionConfig{
			Type:      "napcat_http",
			BaseURL:   "http://127.0.0.1:3002",
			TimeoutMS: 5000,
		},
	})

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := service.DeleteConnection(context.Background(), "napcat-main")
	if err != nil {
		t.Fatalf("DeleteConnection() error = %v", err)
	}
	if result.ConnectionID != "napcat-main" {
		t.Fatalf("ConnectionID = %q, want napcat-main", result.ConnectionID)
	}
	if len(service.cfg.Connections) != 1 {
		t.Fatalf("connections len = %d, want 1", len(service.cfg.Connections))
	}
	if _, ok := service.ConnectionDetail("napcat-main"); ok {
		t.Fatalf("ConnectionDetail() ok = true, want false after delete")
	}
	if _, ok := service.ConnectionDetail("napcat-secondary"); !ok {
		t.Fatalf("ConnectionDetail(napcat-secondary) ok = false, want preserved secondary connection")
	}

	targetPath := config.ResolveWritablePath(configPath)
	payload, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", targetPath, err)
	}
	text := string(payload)
	if strings.Contains(text, "id: napcat-main") {
		t.Fatalf("saved config = %s, want deleted connection removed", text)
	}
	if !strings.Contains(text, "id: napcat-secondary") {
		t.Fatalf("saved config = %s, want secondary connection kept", text)
	}
}

func TestServiceSaveAIConfig_HotAppliesCoreAIConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := testRuntimeConfig()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.example.yml")
	service, err := New(cfg, configPath, logger)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.state = StateRunning

	result, err := service.SaveAIConfig(context.Background(), config.AIConfig{
		Enabled: true,
		Provider: config.AIProviderConfig{
			Kind:        "openai_compatible",
			Vendor:      "openai",
			BaseURL:     "https://api.openai.com/v1",
			APIKey:      "secret-key",
			Model:       "gpt-4.1-mini",
			TimeoutMS:   30000,
			Temperature: 0.8,
		},
		Reply: config.AIReplyConfig{
			EnabledInGroup:   true,
			EnabledInPrivate: true,
			ReplyOnAt:        true,
			ReplyOnBotName:   false,
			ReplyOnQuote:     false,
			CooldownSeconds:  20,
			MaxContextMsgs:   16,
			MaxOutputTokens:  160,
		},
		Memory: config.AIMemoryConfig{
			Enabled:                 true,
			SessionWindow:           24,
			CandidateEnabled:        true,
			PromoteThreshold:        2,
			MaxPromptLongTerm:       4,
			MaxPromptCandidates:     3,
			ReflectionRawLimit:      768,
			ReflectionPerGroupLimit: 36,
		},
		Prompt: config.AIPromptConfig{
			BotName:      "罗纸酱",
			SystemPrompt: "你是一个测试 AI。",
		},
	})
	if err != nil {
		t.Fatalf("SaveAIConfig() error = %v", err)
	}
	if result.RestartRequired {
		t.Fatalf("RestartRequired = true, want false")
	}
	if !result.HotApplied || result.HotApplyError != "" {
		t.Fatalf("hot apply flags = applied:%v err:%q, want true/empty", result.HotApplied, result.HotApplyError)
	}
	if !result.View.Snapshot.Enabled || result.View.Snapshot.Model != "gpt-4.1-mini" {
		t.Fatalf("AI snapshot = %+v, want enabled gpt-4.1-mini", result.View.Snapshot)
	}
	if !service.cfg.AI.Enabled || service.cfg.AI.Provider.Model != "gpt-4.1-mini" {
		t.Fatalf("service cfg ai = %+v, want persisted enabled ai config", service.cfg.AI)
	}
	if got := service.cfg.AI.Prompt.BotName; got != cfg.App.Name {
		t.Fatalf("service cfg ai bot name = %q, want synced app name %q", got, cfg.App.Name)
	}

	targetPath := config.ResolveWritablePath(configPath)
	payload, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", targetPath, err)
	}
	text := string(payload)
	if !strings.Contains(text, "ai:") || !strings.Contains(text, "model: gpt-4.1-mini") {
		t.Fatalf("saved config = %s, want ai section with model", text)
	}
	if !strings.Contains(text, "bot_name: go-bot") {
		t.Fatalf("saved config = %s, want ai bot name synced with app name", text)
	}
}

func testRuntimeConfig() *config.Config {
	return &config.Config{
		App: config.AppConfig{
			Name:     "go-bot",
			Env:      "test",
			OwnerQQ:  "123456789",
			DataDir:  "./data",
			LogLevel: "info",
		},
		Server: config.ServerConfig{
			Admin: config.AdminServerConfig{
				Enabled: true,
				Listen:  ":8090",
			},
			WebUI: config.WebUIConfig{
				Enabled:  true,
				BasePath: "/",
				Theme:    config.WebUIThemePinkLight,
			},
		},
		Storage: config.StorageConfig{
			SQLite: config.SQLiteConfig{Path: "./data/app.db"},
			Logs: config.LogsConfig{
				Dir:        "./data/logs",
				MaxSizeMB:  10,
				MaxBackups: 3,
				MaxAgeDays: 7,
			},
		},
		Connections: []config.ConnectionConfig{{
			ID:       "napcat-main",
			Enabled:  true,
			Platform: "onebot_v11",
			Ingress: config.IngressConfig{
				Type:   "ws_server",
				Listen: ":0",
			},
			Action: config.ActionConfig{
				Type:      "napcat_http",
				BaseURL:   "http://127.0.0.1:3000",
				TimeoutMS: 1000,
			},
		}},
		Plugins: []config.PluginConfig{},
		Security: config.SecurityConfig{
			AdminAuth: config.AdminAuthConfig{
				Enabled:  false,
				Password: "",
			},
		},
	}
}
