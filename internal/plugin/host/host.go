package host

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
	"github.com/spf13/viper"
)

var (
	runtimeMonitorInterval        = 500 * time.Millisecond
	autoRestartBaseDelay          = 1 * time.Second
	autoRestartMaxDelay           = 10 * time.Second
	autoRestartStableWindow       = 30 * time.Second
	autoRestartMaxConsecutiveFail = 3
)

type Factory func() sdk.Plugin

type Registration struct {
	Manifest sdk.Manifest
	Factory  Factory
}

type Host struct {
	logger     *slog.Logger
	messenger  sdk.Messenger
	botAPI     sdk.BotAPI
	aiTools    sdk.AIToolRegistrar
	appInfo    sdk.AppInfo
	mu         sync.RWMutex
	registry   map[string]Factory
	manifests  map[string]sdk.Manifest
	dynamicIDs map[string]struct{}
	managed    map[string]*managedPlugin
	configured map[string]config.PluginConfig
}

type managedPlugin struct {
	instance  sdk.Plugin
	manifest  sdk.Manifest
	state     PluginState
	lastError string
	enabled   bool
	runtime   sdk.RuntimeStatus

	monitorCancel        context.CancelFunc
	aiTools              *pluginAIToolRegistrar
	autoRestart          bool
	restartCount         int
	consecutiveFailures  int
	nextRestartAt        time.Time
	circuitOpen          bool
	circuitReason        string
	lastUnexpectedStopAt time.Time
}

type pluginAIToolRegistrar struct {
	base     sdk.AIToolRegistrar
	pluginID string

	mu         sync.Mutex
	closed     bool
	namespaces map[string]string
}

func newPluginAIToolRegistrar(pluginID string, base sdk.AIToolRegistrar) *pluginAIToolRegistrar {
	if base == nil {
		return nil
	}
	return &pluginAIToolRegistrar{
		base:       base,
		pluginID:   strings.TrimSpace(pluginID),
		namespaces: make(map[string]string),
	}
}

func (r *pluginAIToolRegistrar) RegisterTools(namespace string, tools []sdk.AIToolDefinition) error {
	if r == nil || r.base == nil {
		return nil
	}
	providerID := r.providerID(namespace)
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("AI tool registrar 已关闭")
	}
	r.mu.Unlock()
	if err := r.base.RegisterTools(providerID, tools); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		r.base.UnregisterTools(providerID)
		return fmt.Errorf("AI tool registrar 已关闭")
	}
	r.namespaces[normalizeAIToolNamespace(namespace)] = providerID
	return nil
}

func (r *pluginAIToolRegistrar) UnregisterTools(namespace string) {
	if r == nil || r.base == nil {
		return
	}
	providerID := r.providerID(namespace)
	r.base.UnregisterTools(providerID)
	r.mu.Lock()
	delete(r.namespaces, normalizeAIToolNamespace(namespace))
	r.mu.Unlock()
}

func (r *pluginAIToolRegistrar) Close() {
	if r == nil || r.base == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	providerIDs := make([]string, 0, len(r.namespaces))
	for _, providerID := range r.namespaces {
		providerIDs = append(providerIDs, providerID)
	}
	r.namespaces = make(map[string]string)
	r.mu.Unlock()
	for _, providerID := range providerIDs {
		r.base.UnregisterTools(providerID)
	}
}

func (r *pluginAIToolRegistrar) providerID(namespace string) string {
	return "plugin." + r.pluginID + "." + normalizeAIToolNamespace(namespace)
}

func normalizeAIToolNamespace(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return "default"
	}
	return namespace
}

func releaseManagedPluginAITools(entry *managedPlugin) {
	if entry == nil || entry.aiTools == nil {
		return
	}
	entry.aiTools.Close()
	entry.aiTools = nil
}

func New(logger *slog.Logger, messenger sdk.Messenger, botAPI sdk.BotAPI) *Host {
	return &Host{
		logger:     logger,
		messenger:  messenger,
		botAPI:     botAPI,
		registry:   make(map[string]Factory),
		manifests:  make(map[string]sdk.Manifest),
		dynamicIDs: make(map[string]struct{}),
		managed:    make(map[string]*managedPlugin),
		configured: make(map[string]config.PluginConfig),
	}
}

func (h *Host) SetAppInfo(info sdk.AppInfo) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.appInfo = info
}

func (h *Host) SetAITools(registrar sdk.AIToolRegistrar) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.aiTools = registrar
}

func (h *Host) Register(factory Factory) {
	plugin := factory()
	manifest := plugin.Manifest()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.registry[manifest.ID] = factory
	h.manifests[manifest.ID] = manifest
}

func (h *Host) SyncDynamic(registrations []Registration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for id := range h.dynamicIDs {
		delete(h.registry, id)
		delete(h.manifests, id)
	}
	clear(h.dynamicIDs)

	for _, item := range registrations {
		if item.Factory == nil || item.Manifest.ID == "" {
			continue
		}
		h.registry[item.Manifest.ID] = item.Factory
		h.manifests[item.Manifest.ID] = item.Manifest
		h.dynamicIDs[item.Manifest.ID] = struct{}{}
	}
}

func (h *Host) Manifest(id string) (sdk.Manifest, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	manifest, ok := h.manifests[id]
	return manifest, ok
}

func (h *Host) SetConfigured(plugins []config.PluginConfig) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.configured = toConfiguredMap(plugins)
}

func (h *Host) Apply(ctx context.Context, plugins []config.PluginConfig) error {
	h.mu.RLock()
	prevConfigured := copyConfiguredMap(h.configured)
	h.mu.RUnlock()

	nextConfigured := toConfiguredMap(plugins)
	h.SetConfigured(plugins)

	stopSet := make(map[string]struct{})
	startSet := make(map[string]struct{})
	var errs []error

	for id, prevCfg := range prevConfigured {
		nextCfg, exists := nextConfigured[id]
		if !exists || !nextCfg.Enabled || !reflect.DeepEqual(prevCfg, nextCfg) {
			stopSet[id] = struct{}{}
		}
	}

	for id, nextCfg := range nextConfigured {
		prevCfg, existed := prevConfigured[id]
		if !nextCfg.Enabled {
			stopSet[id] = struct{}{}
			continue
		}
		if !existed || !prevCfg.Enabled || !reflect.DeepEqual(prevCfg, nextCfg) || h.pluginNeedsStart(id) {
			startSet[id] = struct{}{}
		}
	}

	for _, id := range sortedPluginIDs(stopSet) {
		if err := h.StopPlugin(ctx, id); err != nil {
			h.logger.Error("停止插件失败", "plugin", id, "error", err)
			errs = append(errs, err)
		}
	}
	for _, id := range sortedPluginIDs(startSet) {
		if err := h.StartPlugin(ctx, id); err != nil {
			h.logger.Error("启动插件失败", "plugin", id, "error", err)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (h *Host) pluginNeedsStart(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry, ok := h.managed[id]
	if !ok {
		return true
	}
	h.refreshManagedStateLocked(id, entry)
	return entry.state != PluginRunning && entry.state != PluginStarting
}

func (h *Host) StartPlugin(ctx context.Context, id string) error {
	h.mu.Lock()

	pluginCfg, ok := h.configured[id]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("插件未配置: %s", id)
	}

	factory, ok := h.registry[id]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("插件未注册: %s", id)
	}

	if existing, ok := h.managed[id]; ok {
		h.refreshManagedStateLocked(id, existing)
		switch existing.state {
		case PluginRunning:
			existing.enabled = true
			h.mu.Unlock()
			return nil
		case PluginStarting, PluginStopping:
			h.mu.Unlock()
			return fmt.Errorf("插件当前处于%s状态: %s", existing.state, id)
		default:
			if existing.monitorCancel != nil {
				existing.monitorCancel()
			}
			releaseManagedPluginAITools(existing)
			delete(h.managed, id)
		}
	}

	instance := factory()
	manifest := instance.Manifest()
	entry := &managedPlugin{
		instance:    instance,
		manifest:    manifest,
		state:       PluginStarting,
		enabled:     true,
		autoRestart: !manifest.Builtin,
	}
	h.managed[id] = entry
	h.mu.Unlock()

	if err := h.startManagedPlugin(ctx, id, entry, pluginCfg); err != nil {
		return fmt.Errorf("启动插件 %s 失败: %w", id, err)
	}
	return nil
}

func (h *Host) StopPlugin(ctx context.Context, id string) error {
	h.mu.Lock()

	entry, ok := h.managed[id]
	if !ok {
		h.mu.Unlock()
		return nil
	}
	h.refreshManagedStateLocked(id, entry)
	if entry.state == PluginFailed || entry.state == PluginStopped {
		if entry.monitorCancel != nil {
			entry.monitorCancel()
		}
		releaseManagedPluginAITools(entry)
		delete(h.managed, id)
		h.mu.Unlock()
		return nil
	}

	entry.state = PluginStopping
	entry.enabled = false
	entry.nextRestartAt = time.Time{}
	instance := entry.instance
	cancel := entry.monitorCancel
	entry.monitorCancel = nil
	h.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if err := instance.Stop(ctx); err != nil {
		h.mu.Lock()
		if current, exists := h.managed[id]; exists && current == entry {
			entry.state = PluginFailed
			entry.lastError = err.Error()
			h.captureRuntimeStatusLocked(entry)
		}
		h.mu.Unlock()
		return fmt.Errorf("停止插件 %s 失败: %w", id, err)
	}

	h.mu.Lock()
	releaseManagedPluginAITools(entry)
	delete(h.managed, id)
	h.mu.Unlock()
	return nil
}

func (h *Host) ReloadPlugin(ctx context.Context, id string) error {
	h.mu.RLock()
	pluginCfg, ok := h.configured[id]
	h.mu.RUnlock()
	if !ok {
		return fmt.Errorf("插件未配置: %s", id)
	}
	if !pluginCfg.Enabled {
		return fmt.Errorf("插件未启用: %s", id)
	}
	if err := h.StopPlugin(ctx, id); err != nil {
		return err
	}
	return h.StartPlugin(ctx, id)
}

func (h *Host) RecoverPlugin(ctx context.Context, id string) error {
	h.mu.Lock()
	pluginCfg, ok := h.configured[id]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("插件未配置: %s", id)
	}
	factory, ok := h.registry[id]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("插件未注册: %s", id)
	}
	manifest, hasManifest := h.manifests[id]
	if hasManifest && manifest.Builtin {
		h.mu.Unlock()
		return fmt.Errorf("内置插件不支持恢复: %s", id)
	}
	if !pluginCfg.Enabled {
		h.mu.Unlock()
		return fmt.Errorf("插件未启用: %s", id)
	}

	entry, exists := h.managed[id]
	if exists {
		h.refreshManagedStateLocked(id, entry)
		switch entry.state {
		case PluginRunning:
			h.mu.Unlock()
			return nil
		case PluginStarting, PluginStopping:
			h.mu.Unlock()
			return fmt.Errorf("插件当前处于%s状态: %s", entry.state, id)
		}
		if entry.monitorCancel != nil {
			entry.monitorCancel()
			entry.monitorCancel = nil
		}
		entry.circuitOpen = false
		entry.circuitReason = ""
		entry.consecutiveFailures = 0
		entry.nextRestartAt = time.Time{}
		entry.enabled = true
		entry.instance = factory()
		entry.manifest = entry.instance.Manifest()
		entry.state = PluginStarting
		entry.lastError = ""
		entry.runtime = mergeRuntimeStatus(entry, entry.runtime)
		h.mu.Unlock()
		return h.startManagedPlugin(ctx, id, entry, pluginCfg)
	}

	instance := factory()
	entry = &managedPlugin{
		instance:    instance,
		manifest:    instance.Manifest(),
		state:       PluginStarting,
		enabled:     true,
		autoRestart: !instance.Manifest().Builtin,
	}
	h.managed[id] = entry
	h.mu.Unlock()
	return h.startManagedPlugin(ctx, id, entry, pluginCfg)
}

func (h *Host) StopAll(ctx context.Context) error {
	h.mu.RLock()
	ids := make([]string, 0, len(h.managed))
	for id := range h.managed {
		ids = append(ids, id)
	}
	h.mu.RUnlock()

	var firstErr error
	for _, id := range ids {
		if err := h.StopPlugin(ctx, id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h *Host) Dispatch(ctx context.Context, evt event.Event) {
	type dispatchItem struct {
		id       string
		instance sdk.Plugin
	}

	h.mu.Lock()
	items := make([]dispatchItem, 0, len(h.managed))
	for id, entry := range h.managed {
		h.refreshManagedStateLocked(id, entry)
		if entry.state != PluginRunning || !entry.enabled {
			continue
		}
		items = append(items, dispatchItem{
			id:       id,
			instance: entry.instance,
		})
	}
	h.mu.Unlock()

	for _, item := range items {
		if err := item.instance.HandleEvent(ctx, evt); err != nil {
			h.logger.Error("插件处理事件失败", "plugin", item.id, "error", err, "event_kind", evt.Kind)
			h.mu.Lock()
			if entry, ok := h.managed[item.id]; ok {
				h.refreshManagedStateLocked(item.id, entry)
			}
			h.mu.Unlock()
		}
	}
}

func (h *Host) Snapshots() []Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()

	ids := make(map[string]struct{}, len(h.manifests)+len(h.configured))
	for id := range h.manifests {
		ids[id] = struct{}{}
	}
	for id := range h.configured {
		ids[id] = struct{}{}
	}

	out := make([]Snapshot, 0, len(ids))
	for id := range ids {
		if entry, ok := h.managed[id]; ok {
			h.refreshManagedStateLocked(id, entry)
		}
		out = append(out, h.buildSnapshotLocked(id))
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (h *Host) Detail(id string) (Detail, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	_, hasManifest := h.manifests[id]
	pluginCfg, configured := h.configured[id]
	if !hasManifest && !configured {
		return Detail{}, false
	}
	if entry, ok := h.managed[id]; ok {
		h.refreshManagedStateLocked(id, entry)
	}

	runtime := sdk.RuntimeStatus{}
	if entry, ok := h.managed[id]; ok {
		runtime = cloneRuntimeStatus(entry.runtime)
	}

	return Detail{
		Snapshot: h.buildSnapshotLocked(id),
		Config:   cloneConfig(pluginCfg.Config),
		Runtime:  runtime,
	}, true
}

type hostPluginCatalog struct {
	host *Host
}

func (c hostPluginCatalog) ListPlugins() []sdk.PluginInfo {
	c.host.mu.RLock()
	defer c.host.mu.RUnlock()

	infos := make([]sdk.PluginInfo, 0, len(c.host.manifests))
	for id, manifest := range c.host.manifests {
		cfg, configured := c.host.configured[id]
		infos = append(infos, sdk.PluginInfo{
			ID:          manifest.ID,
			Name:        manifest.Name,
			Version:     manifest.Version,
			Description: manifest.Description,
			Kind:        manifest.Kind,
			Enabled:     configured && cfg.Enabled,
			Builtin:     manifest.Builtin,
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos
}

func (h *Host) buildSnapshotLocked(id string) Snapshot {
	manifest, hasManifest := h.manifests[id]
	pluginCfg, configured := h.configured[id]

	snapshot := Snapshot{
		ID:          id,
		Name:        id,
		Version:     "",
		Description: "",
		Author:      "",
		Kind:        pluginCfg.Kind,
		State:       PluginStopped,
		Builtin:     pluginCfg.Kind == "builtin",
		Enabled:     configured && pluginCfg.Enabled,
		Configured:  configured,
	}

	if hasManifest {
		snapshot.Name = manifest.Name
		snapshot.Version = manifest.Version
		snapshot.Description = manifest.Description
		snapshot.Author = manifest.Author
		snapshot.Builtin = manifest.Builtin
		if manifest.Kind != "" {
			snapshot.Kind = manifest.Kind
		} else if snapshot.Kind == "" {
			if manifest.Builtin {
				snapshot.Kind = "builtin"
			}
		}
	}

	if entry, ok := h.managed[id]; ok {
		snapshot.State = entry.state
		snapshot.LastError = entry.lastError
		snapshot.Name = entry.manifest.Name
		snapshot.Version = entry.manifest.Version
		snapshot.Description = entry.manifest.Description
		snapshot.Author = entry.manifest.Author
		snapshot.Builtin = entry.manifest.Builtin
		if entry.manifest.Kind != "" {
			snapshot.Kind = entry.manifest.Kind
		} else if snapshot.Kind == "" && entry.manifest.Builtin {
			snapshot.Kind = "builtin"
		}
	}

	return snapshot
}

func (h *Host) refreshManagedStateLocked(id string, entry *managedPlugin) {
	if entry == nil {
		return
	}
	status, ok := entry.instance.(sdk.RuntimeStatusProvider)
	if !ok {
		return
	}

	current := status.RuntimeStatus()
	entry.runtime = mergeRuntimeStatus(entry, current)
	if current.Running {
		if !current.StartedAt.IsZero() && time.Since(current.StartedAt) >= autoRestartStableWindow {
			entry.consecutiveFailures = 0
			entry.runtime.ConsecutiveFailures = 0
		}
		return
	}
	if current.LastError != "" {
		entry.lastError = current.LastError
	} else if entry.lastError == "" && (entry.state == PluginRunning || entry.state == PluginStarting) {
		entry.lastError = fmt.Sprintf("插件进程已退出: %s", id)
	}

	switch entry.state {
	case PluginRunning, PluginStarting:
		entry.state = PluginFailed
	case PluginStopping:
		if current.LastError != "" {
			entry.lastError = current.LastError
		}
	}
}

func (h *Host) captureRuntimeStatusLocked(entry *managedPlugin) {
	if entry == nil {
		return
	}
	status, ok := entry.instance.(sdk.RuntimeStatusProvider)
	if !ok {
		return
	}
	entry.runtime = cloneRuntimeStatus(status.RuntimeStatus())
}

func cloneConfig(raw map[string]any) map[string]any {
	if raw == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		out[k] = v
	}
	return out
}

func cloneRuntimeStatus(status sdk.RuntimeStatus) sdk.RuntimeStatus {
	out := status
	if status.ExitCode != nil {
		exitCode := *status.ExitCode
		out.ExitCode = &exitCode
	}
	if len(status.RecentLogs) > 0 {
		out.RecentLogs = append([]sdk.RuntimeLog(nil), status.RecentLogs...)
	}
	return out
}

func mergeRuntimeStatus(entry *managedPlugin, status sdk.RuntimeStatus) sdk.RuntimeStatus {
	out := cloneRuntimeStatus(status)
	out.AutoRestart = entry.autoRestart
	out.RestartCount = entry.restartCount
	out.ConsecutiveFailures = entry.consecutiveFailures
	out.NextRestartAt = entry.nextRestartAt
	out.CircuitOpen = entry.circuitOpen
	out.CircuitReason = entry.circuitReason
	out.Restarting = !entry.nextRestartAt.IsZero() && !entry.circuitOpen
	return out
}

func (h *Host) startManagedPlugin(ctx context.Context, id string, entry *managedPlugin, pluginCfg config.PluginConfig) error {
	toolRegistrar := newPluginAIToolRegistrar(id, h.aiTools)
	entry.aiTools = toolRegistrar
	env := sdk.Env{
		Logger:        h.logger.With("plugin", id),
		Messenger:     h.messenger,
		BotAPI:        h.botAPI,
		AITools:       toolRegistrar,
		Config:        mapConfigReader{raw: pluginCfg.Config},
		PluginCatalog: hostPluginCatalog{host: h},
		App:           h.appInfo,
	}

	if err := entry.instance.Start(ctx, env); err != nil {
		releaseManagedPluginAITools(entry)
		h.mu.Lock()
		if current, exists := h.managed[id]; exists && current == entry {
			entry.state = PluginFailed
			entry.lastError = err.Error()
			h.captureRuntimeStatusLocked(entry)
			entry.runtime = mergeRuntimeStatus(entry, entry.runtime)
		}
		h.mu.Unlock()
		return err
	}

	h.mu.Lock()
	if current, exists := h.managed[id]; exists && current == entry {
		entry.state = PluginRunning
		entry.lastError = ""
		entry.nextRestartAt = time.Time{}
		entry.circuitOpen = false
		entry.circuitReason = ""
		h.captureRuntimeStatusLocked(entry)
		entry.runtime = mergeRuntimeStatus(entry, entry.runtime)
		h.startRuntimeMonitorLocked(id, entry)
	}
	h.mu.Unlock()
	return nil
}

func (h *Host) startRuntimeMonitorLocked(id string, entry *managedPlugin) {
	if entry == nil {
		return
	}
	if _, ok := entry.instance.(sdk.RuntimeStatusProvider); !ok {
		entry.runtime = mergeRuntimeStatus(entry, entry.runtime)
		return
	}
	if entry.monitorCancel != nil {
		entry.monitorCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	entry.monitorCancel = cancel
	go h.monitorRuntime(ctx, id, entry)
}

func (h *Host) monitorRuntime(ctx context.Context, id string, entry *managedPlugin) {
	provider, ok := entry.instance.(sdk.RuntimeStatusProvider)
	if !ok {
		return
	}
	ticker := time.NewTicker(runtimeMonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			status := provider.RuntimeStatus()

			var delay time.Duration
			var shouldRestart bool

			h.mu.Lock()
			current, exists := h.managed[id]
			if !exists || current != entry {
				h.mu.Unlock()
				return
			}

			entry.runtime = mergeRuntimeStatus(entry, status)
			if status.Running {
				if !status.StartedAt.IsZero() && time.Since(status.StartedAt) >= autoRestartStableWindow {
					entry.consecutiveFailures = 0
				}
				entry.runtime = mergeRuntimeStatus(entry, status)
				h.mu.Unlock()
				continue
			}

			if status.LastError != "" {
				entry.lastError = status.LastError
			} else if entry.lastError == "" {
				entry.lastError = fmt.Sprintf("插件进程已退出: %s", id)
			}
			if entry.state != PluginStopping {
				entry.state = PluginFailed
			}
			releaseManagedPluginAITools(entry)
			delay, shouldRestart = h.planManagedRestartLocked(id, entry, status)
			entry.runtime = mergeRuntimeStatus(entry, status)
			h.mu.Unlock()

			if shouldRestart {
				go h.restartManagedPluginAfterDelay(id, entry, delay, entry.nextRestartAt)
			}
			return
		}
	}
}

func (h *Host) planManagedRestartLocked(id string, entry *managedPlugin, status sdk.RuntimeStatus) (time.Duration, bool) {
	if entry == nil || !entry.autoRestart || !entry.enabled || entry.state == PluginStopping || entry.circuitOpen || !entry.nextRestartAt.IsZero() {
		return 0, false
	}

	now := time.Now()
	if !status.StartedAt.IsZero() && now.Sub(status.StartedAt) >= autoRestartStableWindow {
		entry.consecutiveFailures = 0
	}

	entry.lastUnexpectedStopAt = now
	entry.consecutiveFailures++
	if entry.consecutiveFailures >= autoRestartMaxConsecutiveFail {
		entry.circuitOpen = true
		entry.circuitReason = fmt.Sprintf("连续异常退出达到上限（%d 次）", entry.consecutiveFailures)
		entry.lastError = entry.circuitReason
		entry.runtime = mergeRuntimeStatus(entry, status)
		return 0, false
	}

	entry.restartCount++
	delay := autoRestartBaseDelay << max(0, entry.consecutiveFailures-1)
	if delay > autoRestartMaxDelay {
		delay = autoRestartMaxDelay
	}
	entry.nextRestartAt = now.Add(delay)
	entry.state = PluginStarting
	entry.runtime = mergeRuntimeStatus(entry, status)
	h.logger.Warn("插件发生异常退出，准备自动重启", "plugin", id, "delay", delay, "consecutive_failures", entry.consecutiveFailures)
	return delay, true
}

func (h *Host) restartManagedPluginAfterDelay(id string, entry *managedPlugin, delay time.Duration, scheduledAt time.Time) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	<-timer.C

	h.mu.Lock()
	current, exists := h.managed[id]
	if !exists || current != entry || current.circuitOpen || !current.enabled || current.state == PluginStopping || !current.nextRestartAt.Equal(scheduledAt) {
		h.mu.Unlock()
		return
	}

	pluginCfg, ok := h.configured[id]
	if !ok {
		current.nextRestartAt = time.Time{}
		current.runtime = mergeRuntimeStatus(current, current.runtime)
		h.mu.Unlock()
		return
	}
	factory, ok := h.registry[id]
	if !ok {
		current.nextRestartAt = time.Time{}
		current.lastError = fmt.Sprintf("插件未注册: %s", id)
		current.state = PluginFailed
		current.runtime = mergeRuntimeStatus(current, current.runtime)
		h.mu.Unlock()
		return
	}

	current.instance = factory()
	current.manifest = current.instance.Manifest()
	current.state = PluginStarting
	current.nextRestartAt = time.Time{}
	current.runtime = mergeRuntimeStatus(current, current.runtime)
	h.mu.Unlock()

	if err := h.startManagedPlugin(context.Background(), id, entry, pluginCfg); err != nil {
		h.logger.Error("自动重启插件失败", "plugin", id, "error", err)

		h.mu.Lock()
		current, exists := h.managed[id]
		if !exists || current != entry {
			h.mu.Unlock()
			return
		}
		current.state = PluginFailed
		current.lastError = err.Error()
		current.runtime = mergeRuntimeStatus(current, current.runtime)
		delay, shouldRestart := h.planManagedRestartLocked(id, current, current.runtime)
		scheduledAt = current.nextRestartAt
		h.mu.Unlock()
		if shouldRestart {
			go h.restartManagedPluginAfterDelay(id, current, delay, scheduledAt)
		}
	}
}

func toConfiguredMap(plugins []config.PluginConfig) map[string]config.PluginConfig {
	out := make(map[string]config.PluginConfig, len(plugins))
	for _, plugin := range plugins {
		out[plugin.ID] = plugin
	}
	return out
}

func copyConfiguredMap(in map[string]config.PluginConfig) map[string]config.PluginConfig {
	out := make(map[string]config.PluginConfig, len(in))
	for id, cfg := range in {
		out[id] = cfg
	}
	return out
}

func sortedPluginIDs(items map[string]struct{}) []string {
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

type mapConfigReader struct {
	raw map[string]any
}

func (r mapConfigReader) Unmarshal(target any) error {
	v := viper.New()
	for k, val := range r.raw {
		v.Set(k, val)
	}
	return v.Unmarshal(target)
}

func (r mapConfigReader) Raw() map[string]any {
	if r.raw == nil {
		return map[string]any{}
	}
	return r.raw
}
