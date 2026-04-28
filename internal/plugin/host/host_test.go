package host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

type pluginSpy struct {
	starts int
	stops  int
	modes  []string
	owners []string
}

type spyPlugin struct {
	state *pluginSpy
}

type failingPlugin struct{}

type runtimeStatusController struct {
	mu         sync.Mutex
	starts     int
	crashLimit int
}

type aiToolRegistrarSpy struct {
	mu           sync.Mutex
	registered   map[string]int
	unregistered map[string]int
}

type aiToolPlugin struct{}

type runtimeStatusPlugin struct {
	controller *runtimeStatusController

	mu        sync.Mutex
	running   bool
	startedAt time.Time
	stoppedAt time.Time
	exitCode  *int
	lastError string
}

func (p *spyPlugin) Manifest() sdk.Manifest {
	return sdk.Manifest{
		ID:          "demo_plugin",
		Name:        "Demo Plugin",
		Version:     "0.1.0",
		Description: "test plugin",
		Author:      "test",
		Builtin:     true,
	}
}

func (p *spyPlugin) Start(_ context.Context, env sdk.Env) error {
	p.state.starts++
	p.state.owners = append(p.state.owners, env.OwnerQQ())
	raw := env.Config.Raw()
	if raw == nil {
		p.state.modes = append(p.state.modes, "")
		return nil
	}
	if mode, ok := raw["mode"].(string); ok {
		p.state.modes = append(p.state.modes, mode)
		return nil
	}
	p.state.modes = append(p.state.modes, "")
	return nil
}

func (p *spyPlugin) Stop(context.Context) error {
	p.state.stops++
	return nil
}

func (p *spyPlugin) HandleEvent(context.Context, event.Event) error {
	return nil
}

func (p *failingPlugin) Manifest() sdk.Manifest {
	return sdk.Manifest{
		ID:      "failing_plugin",
		Name:    "Failing Plugin",
		Version: "0.1.0",
		Author:  "test",
		Builtin: true,
	}
}

func (p *failingPlugin) Start(context.Context, sdk.Env) error {
	return fmt.Errorf("boom")
}

func (p *failingPlugin) Stop(context.Context) error {
	return nil
}

func (p *failingPlugin) HandleEvent(context.Context, event.Event) error {
	return nil
}

func (p *aiToolPlugin) Manifest() sdk.Manifest {
	return sdk.Manifest{
		ID:      "ai_tool_plugin",
		Name:    "AI Tool Plugin",
		Version: "0.1.0",
		Builtin: true,
	}
}

func (p *aiToolPlugin) Start(_ context.Context, env sdk.Env) error {
	if env.AITools == nil {
		return fmt.Errorf("missing AI tool registrar")
	}
	return env.AITools.RegisterTools("helper", []sdk.AIToolDefinition{{
		Name:        "plugin_helper_tool",
		Description: "plugin helper tool",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handle: func(context.Context, sdk.AIToolContext, json.RawMessage) (any, error) {
			return map[string]any{"ok": true}, nil
		},
	}})
}

func (p *aiToolPlugin) Stop(context.Context) error {
	return nil
}

func (p *aiToolPlugin) HandleEvent(context.Context, event.Event) error {
	return nil
}

func (s *aiToolRegistrarSpy) RegisterTools(namespace string, tools []sdk.AIToolDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.registered == nil {
		s.registered = make(map[string]int)
	}
	s.registered[namespace] = len(tools)
	return nil
}

func (s *aiToolRegistrarSpy) UnregisterTools(namespace string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unregistered == nil {
		s.unregistered = make(map[string]int)
	}
	s.unregistered[namespace]++
}

func (p *runtimeStatusPlugin) Manifest() sdk.Manifest {
	return sdk.Manifest{
		ID:      "runtime_plugin",
		Name:    "Runtime Plugin",
		Version: "0.1.0",
		Kind:    "external_exec",
	}
}

func (p *runtimeStatusPlugin) Start(context.Context, sdk.Env) error {
	p.mu.Lock()
	p.running = true
	p.startedAt = time.Now()
	p.stoppedAt = time.Time{}
	p.exitCode = nil
	p.lastError = ""
	p.mu.Unlock()

	startNo := p.controller.noteStart()
	if startNo <= p.controller.crashLimit {
		go func() {
			time.Sleep(20 * time.Millisecond)
			p.crash(fmt.Sprintf("crash-%d", startNo), startNo)
		}()
	}
	return nil
}

func (p *runtimeStatusPlugin) Stop(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return nil
	}
	p.running = false
	p.stoppedAt = time.Now()
	p.exitCode = nil
	p.lastError = ""
	return nil
}

func (p *runtimeStatusPlugin) HandleEvent(context.Context, event.Event) error {
	return nil
}

func (p *runtimeStatusPlugin) RuntimeStatus() sdk.RuntimeStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	status := sdk.RuntimeStatus{
		Running:   p.running,
		StartedAt: p.startedAt,
		StoppedAt: p.stoppedAt,
		LastError: p.lastError,
	}
	if p.exitCode != nil {
		code := *p.exitCode
		status.ExitCode = &code
	}
	return status
}

func (p *runtimeStatusPlugin) crash(err string, exitCode int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return
	}
	p.running = false
	p.stoppedAt = time.Now()
	p.lastError = err
	code := exitCode
	p.exitCode = &code
}

func (c *runtimeStatusController) noteStart() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.starts++
	return c.starts
}

func (c *runtimeStatusController) StartCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.starts
}

func TestHostApply_ReconcilePluginChanges(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	host := New(logger, nil, nil)
	host.SetAppInfo(sdk.AppInfo{OwnerQQ: "123456789"})
	spy := &pluginSpy{}
	host.Register(func() sdk.Plugin { return &spyPlugin{state: spy} })

	ctx := context.Background()

	if err := host.Apply(ctx, []config.PluginConfig{{
		ID:      "demo_plugin",
		Kind:    "builtin",
		Enabled: true,
		Config:  map[string]any{"mode": "alpha"},
	}}); err != nil {
		t.Fatalf("Apply(enable) error = %v", err)
	}

	if spy.starts != 1 || spy.stops != 0 {
		t.Fatalf("after enable: starts=%d stops=%d, want 1/0", spy.starts, spy.stops)
	}
	if len(spy.modes) != 1 || spy.modes[0] != "alpha" {
		t.Fatalf("after enable: modes=%v, want [alpha]", spy.modes)
	}
	if len(spy.owners) != 1 || spy.owners[0] != "123456789" {
		t.Fatalf("after enable: owners=%v, want [123456789]", spy.owners)
	}

	detail, ok := host.Detail("demo_plugin")
	if !ok {
		t.Fatalf("Detail(enable) ok = false, want true")
	}
	if detail.Snapshot.State != PluginRunning {
		t.Fatalf("state = %s, want running", detail.Snapshot.State)
	}
	if !detail.Snapshot.Enabled || !detail.Snapshot.Configured {
		t.Fatalf("snapshot = %+v, want enabled and configured", detail.Snapshot)
	}

	if err := host.Apply(ctx, []config.PluginConfig{{
		ID:      "demo_plugin",
		Kind:    "builtin",
		Enabled: true,
		Config:  map[string]any{"mode": "beta"},
	}}); err != nil {
		t.Fatalf("Apply(reload) error = %v", err)
	}

	if spy.starts != 2 || spy.stops != 1 {
		t.Fatalf("after reload: starts=%d stops=%d, want 2/1", spy.starts, spy.stops)
	}
	if len(spy.modes) != 2 || spy.modes[1] != "beta" {
		t.Fatalf("after reload: modes=%v, want second mode beta", spy.modes)
	}
	if len(spy.owners) != 2 || spy.owners[1] != "123456789" {
		t.Fatalf("after reload: owners=%v, want second owner 123456789", spy.owners)
	}

	if err := host.Apply(ctx, nil); err != nil {
		t.Fatalf("Apply(remove) error = %v", err)
	}

	if spy.starts != 2 || spy.stops != 2 {
		t.Fatalf("after remove: starts=%d stops=%d, want 2/2", spy.starts, spy.stops)
	}

	detail, ok = host.Detail("demo_plugin")
	if !ok {
		t.Fatalf("Detail(remove) ok = false, want true because manifest still exists")
	}
	if detail.Snapshot.State != PluginStopped {
		t.Fatalf("state after remove = %s, want stopped", detail.Snapshot.State)
	}
	if detail.Snapshot.Configured {
		t.Fatalf("configured after remove = true, want false")
	}
	if detail.Snapshot.Enabled {
		t.Fatalf("enabled after remove = true, want false")
	}
}

func TestHostPluginAITools_AutoCleanupOnStop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registrar := &aiToolRegistrarSpy{}
	host := New(logger, nil, nil)
	host.SetAITools(registrar)
	host.Register(func() sdk.Plugin { return &aiToolPlugin{} })
	host.SetConfigured([]config.PluginConfig{{
		ID:      "ai_tool_plugin",
		Kind:    "builtin",
		Enabled: true,
	}})

	if err := host.StartPlugin(context.Background(), "ai_tool_plugin"); err != nil {
		t.Fatalf("StartPlugin() error = %v", err)
	}

	registrar.mu.Lock()
	registeredCount := registrar.registered["plugin.ai_tool_plugin.helper"]
	registrar.mu.Unlock()
	if registeredCount != 1 {
		t.Fatalf("registered tool count = %d, want 1", registeredCount)
	}

	if err := host.StopPlugin(context.Background(), "ai_tool_plugin"); err != nil {
		t.Fatalf("StopPlugin() error = %v", err)
	}

	registrar.mu.Lock()
	unregisteredCount := registrar.unregistered["plugin.ai_tool_plugin.helper"]
	registrar.mu.Unlock()
	if unregisteredCount != 1 {
		t.Fatalf("unregistered tool count = %d, want 1", unregisteredCount)
	}
}

func TestHostSetConfigured_DoesNotStartPlugin(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	host := New(logger, nil, nil)
	spy := &pluginSpy{}
	host.Register(func() sdk.Plugin { return &spyPlugin{state: spy} })

	host.SetConfigured([]config.PluginConfig{{
		ID:      "demo_plugin",
		Kind:    "builtin",
		Enabled: true,
		Config:  map[string]any{"mode": "idle"},
	}})

	if spy.starts != 0 || spy.stops != 0 {
		t.Fatalf("SetConfigured should not start/stop plugin, got starts=%d stops=%d", spy.starts, spy.stops)
	}

	detail, ok := host.Detail("demo_plugin")
	if !ok {
		t.Fatalf("Detail() ok = false, want true")
	}
	if detail.Snapshot.State != PluginStopped {
		t.Fatalf("state = %s, want stopped", detail.Snapshot.State)
	}
	if !detail.Snapshot.Configured || !detail.Snapshot.Enabled {
		t.Fatalf("snapshot = %+v, want configured=true enabled=true", detail.Snapshot)
	}
}

func TestHostApply_StartsEnabledPreconfiguredPlugin(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	host := New(logger, nil, nil)
	host.SetAppInfo(sdk.AppInfo{OwnerQQ: "123456789"})
	spy := &pluginSpy{}
	host.Register(func() sdk.Plugin { return &spyPlugin{state: spy} })

	cfg := []config.PluginConfig{{
		ID:      "demo_plugin",
		Kind:    "builtin",
		Enabled: true,
		Config:  map[string]any{"mode": "preconfigured"},
	}}
	host.SetConfigured(cfg)

	if err := host.Apply(context.Background(), cfg); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if spy.starts != 1 || spy.stops != 0 {
		t.Fatalf("after apply: starts=%d stops=%d, want 1/0", spy.starts, spy.stops)
	}
	if len(spy.modes) != 1 || spy.modes[0] != "preconfigured" {
		t.Fatalf("modes=%v, want [preconfigured]", spy.modes)
	}
	detail, ok := host.Detail("demo_plugin")
	if !ok {
		t.Fatalf("Detail() ok = false, want true")
	}
	if detail.Snapshot.State != PluginRunning {
		t.Fatalf("state = %s, want running", detail.Snapshot.State)
	}
}

func TestHostApply_ReturnsStartErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	host := New(logger, nil, nil)
	host.Register(func() sdk.Plugin { return &failingPlugin{} })

	err := host.Apply(context.Background(), []config.PluginConfig{{
		ID:      "failing_plugin",
		Kind:    "builtin",
		Enabled: true,
		Config:  map[string]any{},
	}})
	if err == nil {
		t.Fatalf("Apply() error = nil, want aggregated start error")
	}
	if got := err.Error(); got != "启动插件 failing_plugin 失败: boom" {
		t.Fatalf("Apply() error = %q, want start failure", got)
	}

	detail, ok := host.Detail("failing_plugin")
	if !ok {
		t.Fatalf("Detail() ok = false, want true")
	}
	if detail.Snapshot.State != PluginFailed {
		t.Fatalf("state = %s, want failed", detail.Snapshot.State)
	}
	if detail.Snapshot.LastError != "boom" {
		t.Fatalf("last error = %q, want boom", detail.Snapshot.LastError)
	}
}

func TestHostAutoRestartExternalRuntimePlugin(t *testing.T) {
	oldInterval := runtimeMonitorInterval
	oldBaseDelay := autoRestartBaseDelay
	oldMaxDelay := autoRestartMaxDelay
	oldStableWindow := autoRestartStableWindow
	oldMaxFail := autoRestartMaxConsecutiveFail
	runtimeMonitorInterval = 20 * time.Millisecond
	autoRestartBaseDelay = 30 * time.Millisecond
	autoRestartMaxDelay = 30 * time.Millisecond
	autoRestartStableWindow = 200 * time.Millisecond
	autoRestartMaxConsecutiveFail = 3
	defer func() {
		runtimeMonitorInterval = oldInterval
		autoRestartBaseDelay = oldBaseDelay
		autoRestartMaxDelay = oldMaxDelay
		autoRestartStableWindow = oldStableWindow
		autoRestartMaxConsecutiveFail = oldMaxFail
	}()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	host := New(logger, nil, nil)
	controller := &runtimeStatusController{crashLimit: 1}
	host.Register(func() sdk.Plugin { return &runtimeStatusPlugin{controller: controller} })

	if err := host.Apply(context.Background(), []config.PluginConfig{{
		ID:      "runtime_plugin",
		Kind:    "external_exec",
		Enabled: true,
		Config:  map[string]any{},
	}}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, ok := host.Detail("runtime_plugin")
		if ok && detail.Snapshot.State == PluginRunning && controller.StartCount() >= 2 {
			if !detail.Runtime.AutoRestart {
				t.Fatalf("runtime auto_restart = false, want true")
			}
			if detail.Runtime.RestartCount < 1 {
				t.Fatalf("runtime restart_count = %d, want >= 1", detail.Runtime.RestartCount)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("plugin was not auto restarted, starts=%d detail=%+v", controller.StartCount(), mustPluginDetail(t, host, "runtime_plugin"))
}

func TestHostAutoRestartCircuitOpensAfterRepeatedCrashes(t *testing.T) {
	oldInterval := runtimeMonitorInterval
	oldBaseDelay := autoRestartBaseDelay
	oldMaxDelay := autoRestartMaxDelay
	oldStableWindow := autoRestartStableWindow
	oldMaxFail := autoRestartMaxConsecutiveFail
	runtimeMonitorInterval = 20 * time.Millisecond
	autoRestartBaseDelay = 20 * time.Millisecond
	autoRestartMaxDelay = 20 * time.Millisecond
	autoRestartStableWindow = 200 * time.Millisecond
	autoRestartMaxConsecutiveFail = 2
	defer func() {
		runtimeMonitorInterval = oldInterval
		autoRestartBaseDelay = oldBaseDelay
		autoRestartMaxDelay = oldMaxDelay
		autoRestartStableWindow = oldStableWindow
		autoRestartMaxConsecutiveFail = oldMaxFail
	}()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	host := New(logger, nil, nil)
	controller := &runtimeStatusController{crashLimit: 8}
	host.Register(func() sdk.Plugin { return &runtimeStatusPlugin{controller: controller} })

	if err := host.Apply(context.Background(), []config.PluginConfig{{
		ID:      "runtime_plugin",
		Kind:    "external_exec",
		Enabled: true,
		Config:  map[string]any{},
	}}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, ok := host.Detail("runtime_plugin")
		if ok && detail.Runtime.CircuitOpen {
			if detail.Snapshot.State != PluginFailed {
				t.Fatalf("state = %s, want failed", detail.Snapshot.State)
			}
			if detail.Runtime.CircuitReason == "" {
				t.Fatalf("circuit reason = empty, want non-empty")
			}
			if controller.StartCount() != 2 {
				t.Fatalf("start count = %d, want 2 before circuit opens", controller.StartCount())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("circuit did not open, starts=%d detail=%+v", controller.StartCount(), mustPluginDetail(t, host, "runtime_plugin"))
}

func TestHostRecoverPluginClearsCircuitAndRestarts(t *testing.T) {
	oldInterval := runtimeMonitorInterval
	oldBaseDelay := autoRestartBaseDelay
	oldMaxDelay := autoRestartMaxDelay
	oldStableWindow := autoRestartStableWindow
	oldMaxFail := autoRestartMaxConsecutiveFail
	runtimeMonitorInterval = 20 * time.Millisecond
	autoRestartBaseDelay = 20 * time.Millisecond
	autoRestartMaxDelay = 20 * time.Millisecond
	autoRestartStableWindow = 200 * time.Millisecond
	autoRestartMaxConsecutiveFail = 2
	defer func() {
		runtimeMonitorInterval = oldInterval
		autoRestartBaseDelay = oldBaseDelay
		autoRestartMaxDelay = oldMaxDelay
		autoRestartStableWindow = oldStableWindow
		autoRestartMaxConsecutiveFail = oldMaxFail
	}()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	host := New(logger, nil, nil)
	controller := &runtimeStatusController{crashLimit: 2}
	host.Register(func() sdk.Plugin { return &runtimeStatusPlugin{controller: controller} })

	if err := host.Apply(context.Background(), []config.PluginConfig{{
		ID:      "runtime_plugin",
		Kind:    "external_exec",
		Enabled: true,
		Config:  map[string]any{},
	}}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	var detail Detail
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail = mustPluginDetail(t, host, "runtime_plugin")
		if detail.Runtime.CircuitOpen {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !detail.Runtime.CircuitOpen {
		t.Fatalf("circuit not open before recover, detail=%+v", detail)
	}

	if err := host.RecoverPlugin(context.Background(), "runtime_plugin"); err != nil {
		t.Fatalf("RecoverPlugin() error = %v", err)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail = mustPluginDetail(t, host, "runtime_plugin")
		if detail.Snapshot.State == PluginRunning && !detail.Runtime.CircuitOpen && controller.StartCount() >= 3 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("plugin not recovered, starts=%d detail=%+v", controller.StartCount(), detail)
}

func mustPluginDetail(t *testing.T, host *Host, id string) Detail {
	t.Helper()
	detail, ok := host.Detail(id)
	if !ok {
		t.Fatalf("Detail(%s) ok = false, want true", id)
	}
	return detail
}
