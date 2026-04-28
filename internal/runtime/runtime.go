package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	"github.com/XiaoLozee/go-bot/internal/adapter/onebotv11/httpclient"
	onebotingress "github.com/XiaoLozee/go-bot/internal/adapter/onebotv11/ingress"
	"github.com/XiaoLozee/go-bot/internal/ai"
	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
	"github.com/XiaoLozee/go-bot/internal/media"
	pluginbuiltin "github.com/XiaoLozee/go-bot/internal/plugin/builtin"
	"github.com/XiaoLozee/go-bot/internal/plugin/externalexec"
	"github.com/XiaoLozee/go-bot/internal/plugin/host"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
	"github.com/XiaoLozee/go-bot/internal/skills"
	"github.com/XiaoLozee/go-bot/internal/transport/messenger"
)

const (
	defaultAuditLogRetention = 200
	maxAIForwardCacheEntries = 128
	defaultAIEventQueueSize  = 128
	defaultAIRecentSyncCount = 50
	maxAIRecentSyncCount     = 100
	defaultAIDebugViewLimit  = 120
	maxAIRelationTasks       = 32
)

type Service struct {
	cfg             *config.Config
	configPath      string
	configSaveTo    string
	logger          *slog.Logger
	aiService       *ai.Service
	mediaService    *media.Service
	messenger       *messenger.Router
	host            *host.Host
	mu              sync.RWMutex
	state           State
	startedAt       time.Time
	runCtx          context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	aiEventQueue    chan event.Event
	externalRoot    string
	pluginEnvRoot   string
	skillStore      *skills.Store
	auditMu         sync.RWMutex
	auditLogs       []AuditLogEntry
	auditLimit      int
	forwardCacheMu  sync.RWMutex
	forwardCache    map[string]AIForwardMessageView
	relationTaskMu  sync.RWMutex
	relationTasks   map[string]*aiRelationAnalysisTask
	relationTaskSeq uint64
	connections     map[string]adapter.ConnectionSnapshot
	actionClients   map[string]adapter.ActionClient
	ingresses       map[string]adapter.EventIngress
}

type connectionActionClientBuilder interface {
	BuildActionClient(timeout time.Duration) adapter.ActionClient
}

type actionClientReadiness interface {
	Ready() bool
	ReadinessReason() string
}

type connectionProbeDeferredError struct {
	Reason string
}

func (e *connectionProbeDeferredError) Error() string {
	if reason := strings.TrimSpace(e.Reason); reason != "" {
		return reason
	}
	return "连接动作通道未就绪"
}

func syncAIBotNameWithAppName(appName string, aiCfg config.AIConfig) config.AIConfig {
	next := aiCfg
	if name := strings.TrimSpace(appName); name != "" {
		next.Prompt.BotName = name
	}
	return next
}

type pluginHostBotAPI struct {
	router *messenger.Router
}

func (a pluginHostBotAPI) GetStrangerInfo(ctx context.Context, connectionID, userID string) (*sdk.UserInfo, error) {
	return a.router.GetStrangerInfo(ctx, connectionID, userID)
}

func (a pluginHostBotAPI) GetGroupInfo(ctx context.Context, connectionID, groupID string) (*sdk.GroupInfo, error) {
	return a.router.GetGroupInfo(ctx, connectionID, groupID)
}

func (a pluginHostBotAPI) GetGroupMemberList(ctx context.Context, connectionID, groupID string) ([]sdk.GroupMemberInfo, error) {
	return a.router.GetGroupMemberList(ctx, connectionID, groupID)
}

func (a pluginHostBotAPI) GetGroupMemberInfo(ctx context.Context, connectionID, groupID, userID string) (*sdk.GroupMemberInfo, error) {
	return a.router.GetGroupMemberInfo(ctx, connectionID, groupID, userID)
}

func (a pluginHostBotAPI) GetMessage(ctx context.Context, connectionID, messageID string) (*sdk.MessageDetail, error) {
	return a.router.GetMessage(ctx, connectionID, messageID)
}

func (a pluginHostBotAPI) GetForwardMessage(ctx context.Context, connectionID, forwardID string) (*sdk.ForwardMessage, error) {
	return a.router.GetForwardMessageInfo(ctx, connectionID, forwardID)
}

func (a pluginHostBotAPI) DeleteMessage(ctx context.Context, connectionID, messageID string) error {
	return a.router.DeleteMessage(ctx, connectionID, messageID)
}

func (a pluginHostBotAPI) ResolveMedia(ctx context.Context, connectionID, segmentType, file string) (*sdk.ResolvedMedia, error) {
	return a.router.ResolveMediaInfo(ctx, connectionID, segmentType, file)
}

func (a pluginHostBotAPI) GetLoginInfo(ctx context.Context, connectionID string) (*sdk.LoginInfo, error) {
	return a.router.GetLoginInfo(ctx, connectionID)
}

func (a pluginHostBotAPI) GetStatus(ctx context.Context, connectionID string) (*sdk.BotStatus, error) {
	return a.router.GetStatus(ctx, connectionID)
}

func (a pluginHostBotAPI) SendGroupForward(ctx context.Context, connectionID, groupID string, nodes []message.ForwardNode, opts message.ForwardOptions) error {
	return a.router.SendGroupForward(ctx, connectionID, groupID, nodes, opts)
}

func New(cfg *config.Config, configPath string, logger *slog.Logger) (*Service, error) {
	configSaveTo := config.ResolveWritablePath(configPath)
	externalRoot := externalexec.DefaultPluginRoot(configPath)
	pluginEnvRoot := defaultPluginEnvRoot(configSaveTo, cfg.App.DataDir)
	skillRoot := defaultSkillRoot(configSaveTo, cfg.App.DataDir)
	if err := ensureRuntimeDirectories(configSaveTo, externalRoot, pluginEnvRoot, skillRoot); err != nil {
		return nil, err
	}
	if err := externalexec.EnsureEmbeddedPythonCommonRuntime(filepath.Join(externalRoot, "_common")); err != nil {
		return nil, fmt.Errorf("初始化 Python 插件运行时目录失败: %w", err)
	}
	skillStore, err := skills.NewStore(skillRoot)
	if err != nil {
		return nil, fmt.Errorf("创建技能目录失败: %w", err)
	}

	msgRouter := messenger.New()
	pluginHost := host.New(logger, msgRouter, pluginHostBotAPI{router: msgRouter})
	pluginHost.SetAppInfo(sdk.AppInfo{
		Name:        cfg.App.Name,
		Environment: cfg.App.Env,
		OwnerQQ:     cfg.App.OwnerQQ,
	})
	pluginbuiltin.RegisterAll(pluginHost)
	cfg.AI = syncAIBotNameWithAppName(cfg.App.Name, cfg.AI)
	aiService, err := ai.NewService(cfg.AI, cfg.Storage, logger, msgRouter)
	if err != nil {
		return nil, fmt.Errorf("创建 AI 核心服务失败: %w", err)
	}
	pluginHost.SetAITools(aiService)
	mediaService, err := media.NewService(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("创建媒体存储服务失败: %w", err)
	}

	s := &Service{
		cfg:           cfg,
		configPath:    configPath,
		configSaveTo:  configSaveTo,
		logger:        logger,
		aiService:     aiService,
		mediaService:  mediaService,
		messenger:     msgRouter,
		host:          pluginHost,
		state:         StateStopped,
		externalRoot:  externalRoot,
		pluginEnvRoot: pluginEnvRoot,
		skillStore:    skillStore,
		auditLimit:    defaultAuditLogRetention,
		forwardCache:  make(map[string]AIForwardMessageView),
		relationTasks: make(map[string]*aiRelationAnalysisTask),
		connections:   make(map[string]adapter.ConnectionSnapshot),
		actionClients: make(map[string]adapter.ActionClient),
		ingresses:     make(map[string]adapter.EventIngress),
	}
	s.cfg.Connections = normalizeConnectionConfigs(s.cfg.Connections)

	if err := s.syncExternalPlugins(); err != nil {
		return nil, err
	}
	s.cfg.Plugins = s.normalizePluginConfigs(s.cfg.Plugins, nil)
	s.host.SetConfigured(s.cfg.Plugins)
	if err := s.syncPromptSkills(); err != nil {
		s.logger.Error("同步外部技能到 AI 核心失败", "error", err)
	}

	firstDefault := true
	for _, conn := range s.cfg.Connections {
		s.connections[conn.ID] = adapter.ConnectionSnapshot{
			ID:           conn.ID,
			Platform:     conn.Platform,
			Enabled:      conn.Enabled,
			IngressType:  conn.Ingress.Type,
			ActionType:   conn.Action.Type,
			State:        adapter.ConnectionStopped,
			IngressState: adapter.ConnectionStopped,
			UpdatedAt:    time.Now(),
		}

		ingress, err := s.buildIngress(conn)
		if err != nil {
			snapshot := s.connections[conn.ID]
			snapshot.LastError = err.Error()
			s.connections[conn.ID] = snapshot
		} else if ingress != nil {
			s.ingresses[conn.ID] = ingress
		}

		client, err := s.buildActionClient(conn, ingress)
		if err != nil {
			snapshot := s.connections[conn.ID]
			if snapshot.LastError != "" {
				snapshot.LastError += "; "
			}
			snapshot.LastError += err.Error()
			s.connections[conn.ID] = snapshot
			continue
		}
		msgRouter.Register(client, firstDefault)
		s.actionClients[conn.ID] = client
		firstDefault = false
	}

	return s, nil
}

func ensureRuntimeDirectories(configSaveTo string, externalRoot string, pluginEnvRoot string, skillRoot string) error {
	if configSaveTo = strings.TrimSpace(configSaveTo); configSaveTo != "" {
		if err := os.MkdirAll(filepath.Dir(configSaveTo), 0o755); err != nil {
			return fmt.Errorf("创建配置目录失败: %w", err)
		}
	}
	if externalRoot = strings.TrimSpace(externalRoot); externalRoot != "" {
		if err := os.MkdirAll(externalRoot, 0o755); err != nil {
			return fmt.Errorf("创建插件目录失败: %w", err)
		}
	}
	if pluginEnvRoot = strings.TrimSpace(pluginEnvRoot); pluginEnvRoot != "" {
		if err := os.MkdirAll(pluginEnvRoot, 0o755); err != nil {
			return fmt.Errorf("创建插件依赖环境目录失败: %w", err)
		}
	}
	if skillRoot = strings.TrimSpace(skillRoot); skillRoot != "" {
		if err := os.MkdirAll(skillRoot, 0o755); err != nil {
			return fmt.Errorf("创建技能目录失败: %w", err)
		}
	}
	return nil
}

func defaultPluginEnvRoot(configPath string, dataDir string) string {
	root := strings.TrimSpace(dataDir)
	if root == "" {
		root = "./data"
	}
	if filepath.IsAbs(root) {
		return filepath.Join(root, "plugin-envs")
	}

	base := configProjectRoot(configPath)
	if base == "" {
		return filepath.Join(root, "plugin-envs")
	}
	return filepath.Join(base, root, "plugin-envs")
}

func defaultSkillRoot(configPath string, dataDir string) string {
	root := strings.TrimSpace(dataDir)
	if root == "" {
		root = "./data"
	}
	if filepath.IsAbs(root) {
		return filepath.Join(root, "skills")
	}

	base := configProjectRoot(configPath)
	if base == "" {
		return filepath.Join(root, "skills")
	}
	return filepath.Join(base, root, "skills")
}

func configProjectRoot(configPath string) string {
	resolved := strings.TrimSpace(configPath)
	if resolved == "" {
		return ""
	}
	if abs, err := filepath.Abs(resolved); err == nil {
		resolved = abs
	}
	dir := filepath.Dir(resolved)
	if strings.EqualFold(filepath.Base(dir), "configs") {
		dir = filepath.Dir(dir)
	}
	return dir
}

func (s *Service) Start(ctx context.Context) error {
	if err := s.syncExternalPlugins(); err != nil {
		return err
	}

	s.runCtx, s.cancel = context.WithCancel(context.Background())
	if s.mediaService != nil {
		s.mediaService.Start(s.runCtx)
	}

	if err := s.host.Apply(ctx, s.cfg.Plugins); err != nil {
		if s.cancel != nil {
			s.cancel()
		}
		if s.mediaService != nil {
			_ = s.mediaService.Close()
		}
		s.runCtx = nil
		s.cancel = nil
		return err
	}

	s.mu.Lock()
	s.state = StateRunning
	s.startedAt = time.Now()
	if s.aiService != nil {
		s.aiEventQueue = make(chan event.Event, defaultAIEventQueueSize)
		s.wg.Add(1)
		go s.consumeAIEvents(s.aiEventQueue)
	}
	s.mu.Unlock()

	for _, conn := range s.cfg.Connections {
		if !conn.Enabled {
			continue
		}

		if ingress, ok := s.ingresses[conn.ID]; ok {
			if err := ingress.Start(s.runCtx); err != nil {
				s.mu.Lock()
				snapshot := s.connections[conn.ID]
				snapshot.IngressState = adapter.ConnectionFailed
				if snapshot.LastError != "" {
					snapshot.LastError += "; "
				}
				snapshot.LastError += "启动 ingress 失败: " + err.Error()
				snapshot.UpdatedAt = time.Now()
				s.connections[conn.ID] = snapshot
				s.mu.Unlock()
			} else {
				s.mu.Lock()
				snapshot := s.connections[conn.ID]
				snapshot.IngressState = adapter.ConnectionRunning
				snapshot.UpdatedAt = time.Now()
				s.connections[conn.ID] = snapshot
				s.mu.Unlock()

				s.wg.Add(1)
				go s.consumeIngress(ingress)
			}
		}

		status, login, err := s.probeConnection(ctx, conn.ID)
		s.mu.Lock()
		snapshot := s.connections[conn.ID]
		snapshot.Enabled = true
		applyConnectionProbeResult(&snapshot, status, login, err)
		s.connections[conn.ID] = snapshot
		s.mu.Unlock()
	}

	s.logger.Info("启动", "stage", "runtime", "status", "ready", "app", s.cfg.App.Name, "connections", len(s.cfg.Connections), "plugins", len(s.cfg.Plugins))
	return nil
}

func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	if s.state == StateStopped {
		s.mu.Unlock()
		return nil
	}
	s.state = StateStopped
	s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	for _, ingress := range s.ingresses {
		if err := ingress.Stop(ctx); err != nil {
			s.logger.Error("停止 ingress 失败", "ingress", ingress.ID(), "error", err)
		}
	}
	s.wg.Wait()
	s.mu.Lock()
	s.aiEventQueue = nil
	s.mu.Unlock()
	if s.mediaService != nil {
		if err := s.mediaService.Close(); err != nil {
			s.logger.Error("关闭媒体存储服务失败", "error", err)
		}
	}
	if s.aiService != nil {
		if err := s.aiService.Close(); err != nil {
			s.logger.Error("关闭 AI 服务失败", "error", err)
		}
	}

	if err := s.host.StopAll(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	for id, snapshot := range s.connections {
		snapshot.State = adapter.ConnectionStopped
		snapshot.IngressState = adapter.ConnectionStopped
		snapshot.UpdatedAt = time.Now()
		s.connections[id] = snapshot
	}
	s.mu.Unlock()

	s.logger.Info("运行时已停止")
	return nil
}

func (s *Service) Snapshot() Snapshot {
	s.mu.RLock()
	plugins := len(s.host.Snapshots())
	defer s.mu.RUnlock()
	return Snapshot{
		State:       s.state,
		StartedAt:   s.startedAt,
		AppName:     s.cfg.App.Name,
		Environment: s.cfg.App.Env,
		Connections: len(s.connections),
		Plugins:     plugins,
	}
}

func (s *Service) Metadata() Metadata {
	s.mu.RLock()
	authConfigured := strings.TrimSpace(s.cfg.Security.AdminAuth.Password) != ""
	s.mu.RUnlock()
	aiSnapshot := ai.Snapshot{}
	if s.aiService != nil {
		aiSnapshot = s.aiService.Snapshot()
	}
	aiMessageLogAvailable := aiSnapshot.StoreReady

	return Metadata{
		AppName:      s.cfg.App.Name,
		Environment:  s.cfg.App.Env,
		OwnerQQ:      s.cfg.App.OwnerQQ,
		AdminEnabled: s.cfg.Server.Admin.Enabled,
		WebUIEnabled: s.cfg.Server.WebUI.Enabled,
		WebUIBaseURL: s.cfg.Server.WebUI.BasePath,
		WebUITheme:   config.NormalizeWebUITheme(s.cfg.Server.WebUI.Theme),
		Capabilities: map[string]bool{
			"plugin_control":        true,
			"plugin_hot_reload":     true,
			"plugin_install":        true,
			"plugin_upload":         true,
			"plugin_detail":         true,
			"plugin_api_debug":      true,
			"plugin_reload":         true,
			"plugin_recover":        true,
			"plugin_uninstall":      true,
			"ai_core":               true,
			"ai_config":             true,
			"ai_inspect":            true,
			"ai_message_log":        aiMessageLogAvailable,
			"ai_memory_manage":      true,
			"ai_reflection_run":     true,
			"ai_relation_analysis":  true,
			"connection_inspect":    true,
			"connection_detail":     true,
			"connection_probe":      true,
			"connection_save":       true,
			"connection_delete":     true,
			"config_view":           true,
			"config_validate":       true,
			"config_save":           s.configSaveTo != "",
			"config_hot_restart":    true,
			"webui_theme_save":      s.configSaveTo != "",
			"admin_auth":            true,
			"admin_auth_setup":      !authConfigured,
			"audit_log":             true,
			"webui_bootstrap":       true,
			"group_forward_message": true,
		},
	}
}

func (s *Service) ConfigView() map[string]any {
	return config.SanitizedMap(s.cfg)
}

func (s *Service) AIView() AIView {
	installedSkills := s.listInstalledSkillViews()
	if s.aiService == nil {
		return AIView{
			Snapshot:        ai.Snapshot{Enabled: false, Ready: false, State: "disabled"},
			Config:          map[string]any{},
			Debug:           ai.DebugView{},
			Skills:          nil,
			InstalledSkills: installedSkills,
		}
	}
	cfgView, _ := config.SanitizeValue(s.cfg.AI).(map[string]any)
	if cfgView == nil {
		cfgView = map[string]any{}
	}
	return AIView{
		Snapshot:        s.aiService.Snapshot(),
		Config:          cfgView,
		Debug:           s.aiService.DebugView(defaultAIDebugViewLimit),
		Skills:          s.aiService.SkillCatalog(),
		InstalledSkills: installedSkills,
	}
}

func (s *Service) listInstalledSkillViews() []AIInstalledSkillView {
	if s.skillStore == nil {
		return nil
	}
	items := s.skillStore.List()
	out := make([]AIInstalledSkillView, 0, len(items))
	for _, item := range items {
		out = append(out, AIInstalledSkillView{
			ID:                 item.ID,
			Name:               item.Name,
			Description:        item.Description,
			SourceType:         item.SourceType,
			SourceLabel:        item.SourceLabel,
			SourceURL:          item.SourceURL,
			Provider:           item.Provider,
			Enabled:            item.Enabled,
			InstalledAt:        item.InstalledAt,
			UpdatedAt:          item.UpdatedAt,
			EntryPath:          item.EntryPath,
			Format:             item.Format,
			InstructionPreview: item.InstructionPreview,
			ContentLength:      item.ContentLength,
		})
	}
	return out
}

func (s *Service) syncPromptSkills() error {
	if s.aiService == nil || s.skillStore == nil {
		return nil
	}
	items, err := s.skillStore.EnabledPromptSkills()
	if err != nil {
		return err
	}
	promptSkills := make([]ai.PromptSkill, 0, len(items))
	for _, item := range items {
		promptSkills = append(promptSkills, ai.PromptSkill{
			ID:          item.ID,
			Name:        item.Name,
			Description: item.Description,
			Content:     item.Content,
		})
	}
	s.aiService.SetPromptSkills(promptSkills)
	return nil
}

func (s *Service) WebUIBootstrap() WebUIBootstrap {
	return WebUIBootstrap{
		GeneratedAt: time.Now(),
		Meta:        s.Metadata(),
		Runtime:     s.Snapshot(),
		AI:          s.AIView(),
		Connections: s.ConnectionSnapshots(),
		Plugins:     s.PluginSnapshots(),
		Config:      s.ConfigView(),
	}
}

func (s *Service) ListAIInstalledSkills(_ context.Context) ([]AIInstalledSkillView, error) {
	return s.listInstalledSkillViews(), nil
}

func (s *Service) GetAIInstalledSkill(_ context.Context, id string) (AIInstalledSkillDetailView, error) {
	if s.skillStore == nil {
		return AIInstalledSkillDetailView{}, fmt.Errorf("技能中心未初始化")
	}
	item, err := s.skillStore.Get(id)
	if err != nil {
		return AIInstalledSkillDetailView{}, err
	}
	return AIInstalledSkillDetailView{
		AIInstalledSkillView: AIInstalledSkillView{
			ID:                 item.ID,
			Name:               item.Name,
			Description:        item.Description,
			SourceType:         item.SourceType,
			SourceLabel:        item.SourceLabel,
			SourceURL:          item.SourceURL,
			Provider:           item.Provider,
			Enabled:            item.Enabled,
			InstalledAt:        item.InstalledAt,
			UpdatedAt:          item.UpdatedAt,
			EntryPath:          item.EntryPath,
			Format:             item.Format,
			InstructionPreview: item.InstructionPreview,
			ContentLength:      item.ContentLength,
		},
		Content: item.Content,
	}, nil
}

func (s *Service) InstallAIInstalledSkillPackage(ctx context.Context, fileName string, payload []byte, overwrite bool) (AISkillInstallResult, error) {
	if s.skillStore == nil {
		return AISkillInstallResult{}, fmt.Errorf("技能中心未初始化")
	}
	result, err := s.skillStore.InstallArchive(ctx, fileName, payload, overwrite)
	if err != nil {
		return AISkillInstallResult{}, err
	}
	if err := s.syncPromptSkills(); err != nil {
		return AISkillInstallResult{}, err
	}
	return AISkillInstallResult{
		Accepted:    true,
		Replaced:    result.Replaced,
		InstalledTo: result.InstalledTo,
		BackupPath:  result.BackupPath,
		Skill:       mustAIInstalledSkillDetailView(result.Skill),
		View:        s.AIView(),
		Message:     buildAISkillInstallMessage(result.Replaced),
	}, nil
}

func (s *Service) InstallAIInstalledSkillFromURL(ctx context.Context, sourceURL string, overwrite bool) (AISkillInstallResult, error) {
	if s.skillStore == nil {
		return AISkillInstallResult{}, fmt.Errorf("技能中心未初始化")
	}
	result, err := s.skillStore.InstallFromURL(ctx, sourceURL, overwrite)
	if err != nil {
		return AISkillInstallResult{}, err
	}
	if err := s.syncPromptSkills(); err != nil {
		return AISkillInstallResult{}, err
	}
	label := strings.TrimSpace(result.Skill.SourceLabel)
	if label == "" {
		label = "外部技能"
	}
	return AISkillInstallResult{
		Accepted:    true,
		Replaced:    result.Replaced,
		InstalledTo: result.InstalledTo,
		BackupPath:  result.BackupPath,
		Skill:       mustAIInstalledSkillDetailView(result.Skill),
		View:        s.AIView(),
		Message:     label + "已导入并同步到 AI 技能中心",
	}, nil
}

func (s *Service) SetAIInstalledSkillEnabled(_ context.Context, id string, enabled bool) (AISkillActionResult, error) {
	if s.skillStore == nil {
		return AISkillActionResult{}, fmt.Errorf("技能中心未初始化")
	}
	item, err := s.skillStore.SetEnabled(id, enabled)
	if err != nil {
		return AISkillActionResult{}, err
	}
	if err := s.syncPromptSkills(); err != nil {
		return AISkillActionResult{}, err
	}
	viewItem := aiInstalledSkillView(item)
	return AISkillActionResult{
		Accepted: true,
		Action:   map[bool]string{true: "enable", false: "disable"}[enabled],
		ID:       item.ID,
		Enabled:  enabled,
		Skill:    &viewItem,
		View:     s.AIView(),
		Message:  map[bool]string{true: "技能已启用，并已加入 AI 提示上下文", false: "技能已停用，并已从 AI 提示上下文移除"}[enabled],
	}, nil
}

func (s *Service) UninstallAIInstalledSkill(_ context.Context, id string) (AISkillActionResult, error) {
	if s.skillStore == nil {
		return AISkillActionResult{}, fmt.Errorf("技能中心未初始化")
	}
	item, err := s.skillStore.Uninstall(id)
	if err != nil {
		return AISkillActionResult{}, err
	}
	if err := s.syncPromptSkills(); err != nil {
		return AISkillActionResult{}, err
	}
	viewItem := aiInstalledSkillView(item)
	return AISkillActionResult{
		Accepted: true,
		Action:   "uninstall",
		ID:       item.ID,
		Skill:    &viewItem,
		View:     s.AIView(),
		Message:  "技能已卸载",
	}, nil
}

func aiInstalledSkillView(item skills.InstalledSkill) AIInstalledSkillView {
	return AIInstalledSkillView{
		ID:                 item.ID,
		Name:               item.Name,
		Description:        item.Description,
		SourceType:         item.SourceType,
		SourceLabel:        item.SourceLabel,
		SourceURL:          item.SourceURL,
		Provider:           item.Provider,
		Enabled:            item.Enabled,
		InstalledAt:        item.InstalledAt,
		UpdatedAt:          item.UpdatedAt,
		EntryPath:          item.EntryPath,
		Format:             item.Format,
		InstructionPreview: item.InstructionPreview,
		ContentLength:      item.ContentLength,
	}
}

func mustAIInstalledSkillDetailView(item skills.SkillDetail) AIInstalledSkillDetailView {
	return AIInstalledSkillDetailView{
		AIInstalledSkillView: aiInstalledSkillView(item.InstalledSkill),
		Content:              item.Content,
	}
}

func buildAISkillInstallMessage(replaced bool) string {
	if replaced {
		return "技能已覆盖安装，并已同步到 AI 技能中心"
	}
	return "技能已安装，并已同步到 AI 技能中心"
}

func (s *Service) RefreshConnection(ctx context.Context, id string) (ConnectionDetail, error) {
	conn, ok := s.findConnectionConfig(id)
	if !ok {
		return ConnectionDetail{}, fmt.Errorf("连接不存在: %s", id)
	}

	status, login, err := s.probeConnection(ctx, id)

	s.mu.Lock()
	snapshot, exists := s.connections[id]
	if !exists {
		s.mu.Unlock()
		return ConnectionDetail{}, fmt.Errorf("连接快照不存在: %s", id)
	}
	snapshot.Enabled = conn.Enabled
	snapshot.Platform = conn.Platform
	snapshot.ActionType = conn.Action.Type
	snapshot.IngressType = conn.Ingress.Type
	applyConnectionProbeResult(&snapshot, status, login, err)
	s.connections[id] = snapshot
	s.mu.Unlock()

	detail, ok := s.ConnectionDetail(id)
	if !ok {
		return ConnectionDetail{}, fmt.Errorf("连接详情不存在: %s", id)
	}
	return detail, nil
}

func (s *Service) SaveConfig(ctx context.Context, cfg *config.Config) (ConfigSaveResult, error) {
	s.mu.RLock()
	currentCfg := s.cfg
	running := s.state == StateRunning
	s.mu.RUnlock()

	mergedCfg, err := config.MergeSensitiveValues(currentCfg, cfg)
	if err != nil {
		return ConfigSaveResult{}, err
	}
	if mergedCfg.Security.AdminAuth.Enabled && strings.TrimSpace(mergedCfg.Security.AdminAuth.Password) != "" && !config.IsAdminPasswordHashed(mergedCfg.Security.AdminAuth.Password) {
		passwordHash, hashErr := config.HashAdminPassword(mergedCfg.Security.AdminAuth.Password)
		if hashErr != nil {
			return ConfigSaveResult{}, hashErr
		}
		mergedCfg.Security.AdminAuth.Password = passwordHash
	}
	mergedCfg.AI = syncAIBotNameWithAppName(mergedCfg.App.Name, currentCfg.AI)
	mergedCfg.Connections = cloneConnectionConfigs(currentCfg.Connections)
	mergedCfg.Plugins = s.normalizePluginConfigs(currentCfg.Plugins, currentCfg.Plugins)

	pluginChanged := !reflect.DeepEqual(currentCfg.Plugins, mergedCfg.Plugins)
	storageChanged := !reflect.DeepEqual(currentCfg.Storage, mergedCfg.Storage)
	nonPluginChanged := hasNonPluginChanges(currentCfg, mergedCfg)

	result, err := config.Save(s.configPath, mergedCfg)
	if err != nil {
		return ConfigSaveResult{}, err
	}

	var hotApplyErr error
	hotApplyAttempted := false
	hotApplied := false
	if pluginChanged {
		if running {
			hotApplyAttempted = true
			if err := s.host.Apply(ctx, mergedCfg.Plugins); err != nil {
				hotApplyErr = err
				s.logger.Error("热应用插件配置失败", "error", err)
			} else {
				hotApplied = true
			}
		} else {
			s.host.SetConfigured(mergedCfg.Plugins)
		}
	}
	if storageChanged && s.aiService != nil {
		hotApplyAttempted = true
		if err := s.aiService.UpdateStorageConfig(ctx, mergedCfg.Storage); err != nil {
			hotApplyErr = err
			s.logger.Error("热更新 AI 存储配置失败", "error", err)
		} else if running {
			hotApplied = true
		}
	}

	s.mu.Lock()
	s.cfg = mergedCfg
	s.mu.Unlock()

	restartRequired := nonPluginChanged || hotApplyErr != nil
	message := "配置已保存到磁盘"
	switch {
	case hotApplyErr != nil:
		message = "配置已保存，但热应用失败，请重启实例后生效"
	case pluginChanged && hotApplied && restartRequired:
		message = "插件变更已热应用，其余配置重启后生效"
	case pluginChanged && hotApplied:
		message = "插件变更已热应用"
	case pluginChanged && !running:
		message = "插件配置已保存，当前运行时未启动，将在实例启动时加载"
	case restartRequired:
		message = "配置已保存到磁盘，重启后生效"
	default:
		message = "配置已保存到磁盘，无需重启"
	}

	return ConfigSaveResult{
		Accepted:          true,
		Persisted:         true,
		RestartRequired:   restartRequired,
		PluginChanged:     pluginChanged,
		NonPluginChanged:  nonPluginChanged,
		HotApplyAttempted: hotApplyAttempted,
		HotApplied:        hotApplied,
		HotApplyError:     errorString(hotApplyErr),
		SourcePath:        result.SourcePath,
		Path:              result.TargetPath,
		BackupPath:        result.BackupPath,
		SavedAt:           result.SavedAt,
		NormalizedConfig:  config.SanitizedMap(mergedCfg),
		Message:           message,
	}, nil
}

func (s *Service) HotRestart(ctx context.Context) (RuntimeRestartResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.RLock()
	running := s.state == StateRunning
	sourcePath := s.configPath
	loadPath := s.configSaveTo
	logger := s.logger
	s.mu.RUnlock()

	if !running {
		return RuntimeRestartResult{}, fmt.Errorf("运行时未启动，无法热重启")
	}
	if strings.TrimSpace(loadPath) == "" {
		loadPath = sourcePath
	}
	if strings.TrimSpace(loadPath) == "" {
		return RuntimeRestartResult{}, fmt.Errorf("未配置可重载的配置文件路径")
	}

	nextCfg, err := config.Load(loadPath)
	if err != nil {
		return RuntimeRestartResult{}, fmt.Errorf("加载最新配置失败: %w", err)
	}
	replacement, err := New(nextCfg, sourcePath, logger)
	if err != nil {
		return RuntimeRestartResult{}, fmt.Errorf("重建运行时失败: %w", err)
	}

	auditLogs, auditLimit := s.cloneAuditState()
	if err := s.Stop(ctx); err != nil {
		return RuntimeRestartResult{}, fmt.Errorf("停止当前运行时失败: %w", err)
	}
	s.replaceRuntimeStateForRestart(replacement, auditLogs, auditLimit)
	if err := s.Start(ctx); err != nil {
		return RuntimeRestartResult{}, fmt.Errorf("重新启动运行时失败: %w", err)
	}

	snapshot := s.Snapshot()
	return RuntimeRestartResult{
		Accepted:    true,
		Restarted:   true,
		State:       snapshot.State,
		RestartedAt: snapshot.StartedAt,
		Message:     "运行时已按已保存配置完成热重启",
	}, nil
}

func (s *Service) SaveWebUITheme(_ context.Context, theme string) (ConfigSaveResult, error) {
	normalizedTheme := config.NormalizeWebUITheme(theme)
	if !config.IsSupportedWebUITheme(normalizedTheme) {
		return ConfigSaveResult{}, fmt.Errorf("不支持的 WebUI 主题: %s", strings.TrimSpace(theme))
	}

	s.mu.RLock()
	currentCfg := s.cfg
	s.mu.RUnlock()

	nextCfg, err := config.Clone(currentCfg)
	if err != nil {
		return ConfigSaveResult{}, err
	}
	nextCfg.Server.WebUI.Theme = normalizedTheme

	saveResult, err := config.Save(s.configPath, nextCfg)
	if err != nil {
		return ConfigSaveResult{}, err
	}

	s.mu.Lock()
	s.cfg.Server.WebUI.Theme = normalizedTheme
	s.mu.Unlock()

	return ConfigSaveResult{
		Accepted:         true,
		Persisted:        true,
		RestartRequired:  false,
		NonPluginChanged: false,
		Path:             saveResult.TargetPath,
		BackupPath:       saveResult.BackupPath,
		SavedAt:          saveResult.SavedAt,
		NormalizedConfig: config.SanitizedMap(nextCfg),
		Message:          "WebUI 主题已保存并立即生效",
	}, nil
}

func (s *Service) SaveAIConfig(_ context.Context, aiCfg config.AIConfig) (AISaveResult, error) {
	s.mu.RLock()
	currentCfg := s.cfg
	running := s.state == StateRunning
	s.mu.RUnlock()

	nextCfg, err := config.Clone(currentCfg)
	if err != nil {
		return AISaveResult{}, err
	}
	nextCfg.AI = syncAIBotNameWithAppName(currentCfg.App.Name, aiCfg)

	mergedCfg, err := config.MergeSensitiveValues(currentCfg, nextCfg)
	if err != nil {
		return AISaveResult{}, err
	}
	mergedCfg.AI = syncAIBotNameWithAppName(currentCfg.App.Name, mergedCfg.AI)
	mergedCfg.Connections = cloneConnectionConfigs(currentCfg.Connections)
	mergedCfg.Plugins = clonePluginConfigs(currentCfg.Plugins)

	saveResult, err := config.Save(s.configPath, mergedCfg)
	if err != nil {
		return AISaveResult{}, err
	}

	hotApplied := false
	hotApplyErr := error(nil)
	if s.aiService != nil {
		if err := s.aiService.UpdateConfig(mergedCfg.AI); err != nil {
			hotApplyErr = err
		} else {
			hotApplied = running
		}
	}

	s.mu.Lock()
	s.cfg.AI = mergedCfg.AI
	s.mu.Unlock()

	restartRequired := running && hotApplyErr != nil
	message := "AI 配置已保存"
	switch {
	case hotApplyErr != nil:
		message = "AI 配置已保存，但在线热应用失败，请重启实例后生效"
	case hotApplied:
		message = "AI 配置已保存并已在线生效"
	case !running:
		message = "AI 配置已保存，实例启动后生效"
	}

	return AISaveResult{
		Accepted:        true,
		Persisted:       true,
		RestartRequired: restartRequired,
		HotApplied:      hotApplied,
		HotApplyError:   errorString(hotApplyErr),
		Path:            saveResult.TargetPath,
		BackupPath:      saveResult.BackupPath,
		SavedAt:         saveResult.SavedAt,
		View:            s.AIView(),
		Message:         message,
	}, nil
}

func (s *Service) ListAIMessageLogs(ctx context.Context, query ai.MessageLogQuery) (AIMessageListView, error) {
	if s.aiService == nil {
		return AIMessageListView{}, fmt.Errorf("AI 核心未初始化")
	}
	items, err := s.aiService.ListMessageLogs(ctx, query)
	if err != nil {
		return AIMessageListView{}, err
	}
	s.enrichAIMessageLogs(ctx, items)
	return AIMessageListView{
		Items: items,
		Query: query,
	}, nil
}

func (s *Service) ListAIMessageSuggestions(ctx context.Context, query ai.MessageSuggestionQuery) (ai.MessageSearchSuggestions, error) {
	if s.aiService == nil {
		return ai.MessageSearchSuggestions{}, fmt.Errorf("AI 核心未初始化")
	}
	return s.aiService.ListMessageSearchSuggestions(ctx, query)
}

func (s *Service) GetAIMessageDetail(ctx context.Context, messageID string) (AIMessageDetailView, error) {
	if s.aiService == nil {
		return AIMessageDetailView{}, fmt.Errorf("AI 核心未初始化")
	}
	item, err := s.aiService.GetMessageDetail(ctx, messageID)
	if err != nil {
		return AIMessageDetailView{}, err
	}
	s.enrichAIMessageDetail(ctx, &item)
	return AIMessageDetailView{Item: item}, nil
}

type aiMessageDisplayResolver struct {
	service       *Service
	ctx           context.Context
	groupInfo     map[string]*adapter.GroupInfo
	strangerInfo  map[string]*adapter.UserInfo
	memberInfo    map[string]*adapter.GroupMemberInfo
	replyPeerUser map[string]string
}

func (s *Service) enrichAIMessageLogs(ctx context.Context, items []ai.MessageLog) {
	if len(items) == 0 {
		return
	}
	resolver := newAIMessageDisplayResolver(ctx, s)
	for i := range items {
		resolver.enrichMessage(&items[i])
	}
}

func (s *Service) enrichAIMessageDetail(ctx context.Context, item *ai.MessageDetail) {
	if item == nil {
		return
	}
	resolver := newAIMessageDisplayResolver(ctx, s)
	resolver.enrichMessage(&item.Message)
}

func newAIMessageDisplayResolver(ctx context.Context, service *Service) *aiMessageDisplayResolver {
	return &aiMessageDisplayResolver{
		service:       service,
		ctx:           ctx,
		groupInfo:     make(map[string]*adapter.GroupInfo),
		strangerInfo:  make(map[string]*adapter.UserInfo),
		memberInfo:    make(map[string]*adapter.GroupMemberInfo),
		replyPeerUser: make(map[string]string),
	}
}

func (r *aiMessageDisplayResolver) enrichMessage(item *ai.MessageLog) {
	if item == nil {
		return
	}

	connectionID := strings.TrimSpace(item.ConnectionID)
	chatType := strings.ToLower(strings.TrimSpace(item.ChatType))
	groupID := strings.TrimSpace(item.GroupID)
	userID := strings.TrimSpace(item.UserID)
	senderRole := strings.ToLower(strings.TrimSpace(item.SenderRole))
	shouldPersist := false

	if chatType == "private" && isAIBotRole(senderRole) {
		if peerUserID := r.resolvePrivatePeerUserID(*item); peerUserID != "" {
			item.UserID = peerUserID
			userID = peerUserID
		}
	}

	if chatType == "group" && connectionID != "" && groupID != "" {
		if info := r.loadGroupInfo(connectionID, groupID); info != nil {
			groupName := strings.TrimSpace(info.GroupName)
			if groupName != "" && strings.TrimSpace(item.GroupName) != groupName {
				item.GroupName = groupName
				shouldPersist = true
			}
		}
	}

	if connectionID == "" || userID == "" {
		if shouldPersist {
			r.rememberMessageDisplay(*item)
		}
		return
	}

	switch chatType {
	case "group":
		if groupID == "" {
			if shouldPersist {
				r.rememberMessageDisplay(*item)
			}
			return
		}
		member := r.loadGroupMemberInfo(connectionID, groupID, userID)
		if member == nil {
			if shouldPersist {
				r.rememberMessageDisplay(*item)
			}
			return
		}
		displayName := strings.TrimSpace(member.Card)
		if displayName == "" {
			displayName = strings.TrimSpace(member.Nickname)
		}
		nickname := strings.TrimSpace(member.Nickname)
		if nickname == "" {
			nickname = displayName
		}
		if displayName != "" && strings.TrimSpace(item.SenderName) != displayName {
			item.SenderName = displayName
			shouldPersist = true
		}
		if nickname != "" && strings.TrimSpace(item.SenderNickname) != nickname {
			item.SenderNickname = nickname
			shouldPersist = true
		}
	case "private":
		if isAIBotRole(senderRole) {
			if shouldPersist {
				r.rememberMessageDisplay(*item)
			}
			return
		}
		info := r.loadStrangerInfo(connectionID, userID)
		if info == nil {
			if shouldPersist {
				r.rememberMessageDisplay(*item)
			}
			return
		}
		nickname := strings.TrimSpace(info.Nickname)
		if nickname != "" && strings.TrimSpace(item.SenderName) != nickname {
			item.SenderName = nickname
			shouldPersist = true
		}
		if nickname != "" && strings.TrimSpace(item.SenderNickname) != nickname {
			item.SenderNickname = nickname
			shouldPersist = true
		}
	}
	if shouldPersist {
		r.rememberMessageDisplay(*item)
	}
}

func (r *aiMessageDisplayResolver) rememberMessageDisplay(item ai.MessageLog) {
	if r.service == nil || r.service.aiService == nil {
		return
	}
	if err := r.service.aiService.RememberMessageDisplay(r.ctx, item); err != nil {
		r.service.logger.Debug("持久化 AI 显示缓存失败", "connection_id", item.ConnectionID, "chat_type", item.ChatType, "group_id", item.GroupID, "user_id", item.UserID, "error", err)
	}
}

func (r *aiMessageDisplayResolver) resolvePrivatePeerUserID(item ai.MessageLog) string {
	replyTo := strings.TrimSpace(item.ReplyToMessageID)
	if replyTo == "" {
		return strings.TrimSpace(item.UserID)
	}
	if peerUserID, ok := r.replyPeerUser[replyTo]; ok {
		return peerUserID
	}
	if r.service == nil || r.service.aiService == nil {
		r.replyPeerUser[replyTo] = strings.TrimSpace(item.UserID)
		return r.replyPeerUser[replyTo]
	}
	detail, err := r.service.aiService.GetMessageDetail(r.ctx, replyTo)
	if err != nil {
		r.service.logger.Debug("解析 AI 私聊对端失败", "reply_to_message_id", replyTo, "error", err)
		r.replyPeerUser[replyTo] = strings.TrimSpace(item.UserID)
		return r.replyPeerUser[replyTo]
	}
	peerUserID := strings.TrimSpace(detail.Message.UserID)
	if peerUserID == "" {
		peerUserID = strings.TrimSpace(item.UserID)
	}
	r.replyPeerUser[replyTo] = peerUserID
	return peerUserID
}

func (r *aiMessageDisplayResolver) loadGroupInfo(connectionID, groupID string) *adapter.GroupInfo {
	key := connectionID + "\x00" + groupID
	if info, ok := r.groupInfo[key]; ok {
		return info
	}
	client, err := r.service.resolveClient(connectionID)
	if err != nil {
		r.service.logger.Debug("解析 AI 群名失败", "connection_id", connectionID, "group_id", groupID, "error", err)
		r.groupInfo[key] = nil
		return nil
	}
	getter, ok := client.(interface {
		GetGroupInfo(context.Context, string) (*adapter.GroupInfo, error)
	})
	if !ok {
		r.groupInfo[key] = nil
		return nil
	}
	info, err := getter.GetGroupInfo(r.ctx, groupID)
	if err != nil {
		r.service.logger.Debug("查询群资料失败", "connection_id", connectionID, "group_id", groupID, "error", err)
		r.groupInfo[key] = nil
		return nil
	}
	r.groupInfo[key] = info
	return info
}

func (r *aiMessageDisplayResolver) loadStrangerInfo(connectionID, userID string) *adapter.UserInfo {
	key := connectionID + "\x00" + userID
	if info, ok := r.strangerInfo[key]; ok {
		return info
	}
	client, err := r.service.resolveClient(connectionID)
	if err != nil {
		r.service.logger.Debug("解析 AI 私聊昵称失败", "connection_id", connectionID, "user_id", userID, "error", err)
		r.strangerInfo[key] = nil
		return nil
	}
	info, err := client.GetStrangerInfo(r.ctx, userID)
	if err != nil {
		r.service.logger.Debug("查询陌生人资料失败", "connection_id", connectionID, "user_id", userID, "error", err)
		r.strangerInfo[key] = nil
		return nil
	}
	r.strangerInfo[key] = info
	return info
}

func (r *aiMessageDisplayResolver) loadGroupMemberInfo(connectionID, groupID, userID string) *adapter.GroupMemberInfo {
	key := connectionID + "\x00" + groupID + "\x00" + userID
	if info, ok := r.memberInfo[key]; ok {
		return info
	}
	client, err := r.service.resolveClient(connectionID)
	if err != nil {
		r.service.logger.Debug("解析 AI 群成员昵称失败", "connection_id", connectionID, "group_id", groupID, "user_id", userID, "error", err)
		r.memberInfo[key] = nil
		return nil
	}
	info, err := client.GetGroupMemberInfo(r.ctx, groupID, userID)
	if err != nil {
		r.service.logger.Debug("查询群成员资料失败", "connection_id", connectionID, "group_id", groupID, "user_id", userID, "error", err)
		r.memberInfo[key] = nil
		return nil
	}
	r.memberInfo[key] = info
	return info
}

func isAIBotRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "assistant", "bot", "system":
		return true
	default:
		return false
	}
}

func (s *Service) GetAIForwardMessage(ctx context.Context, connectionID, forwardID string) (AIForwardMessageView, error) {
	connectionID = strings.TrimSpace(connectionID)
	forwardID = strings.TrimSpace(forwardID)
	if forwardID == "" {
		return AIForwardMessageView{}, fmt.Errorf("合并转发 ID 不能为空")
	}

	key := aiForwardCacheKey(connectionID, forwardID)
	if cached, ok := s.cachedAIForwardMessage(key); ok {
		cached.Cached = true
		return cached, nil
	}

	if s.messenger == nil {
		return AIForwardMessageView{}, fmt.Errorf("消息通道未初始化")
	}
	item, err := s.messenger.GetForwardMessage(ctx, connectionID, forwardID)
	if err != nil {
		return AIForwardMessageView{}, err
	}
	if item == nil {
		return AIForwardMessageView{}, fmt.Errorf("合并转发消息为空")
	}

	view := AIForwardMessageView{
		ConnectionID: connectionID,
		ForwardID:    item.ID,
		Nodes:        item.Nodes,
		FetchedAt:    time.Now(),
	}
	if strings.TrimSpace(view.ForwardID) == "" {
		view.ForwardID = forwardID
	}
	s.storeAIForwardMessage(key, view)
	return view, nil
}

func (s *Service) SyncAIRecentMessages(ctx context.Context, req AIRecentMessagesSyncRequest) (AIRecentMessagesSyncResult, error) {
	if s.aiService == nil {
		return AIRecentMessagesSyncResult{}, fmt.Errorf("AI 核心未初始化")
	}
	if s.messenger == nil {
		return AIRecentMessagesSyncResult{}, fmt.Errorf("消息通道未初始化")
	}

	req.ConnectionID = strings.TrimSpace(req.ConnectionID)
	if req.ConnectionID == "" {
		req.ConnectionID = s.defaultActionConnectionID()
	}
	req.ChatType = strings.ToLower(strings.TrimSpace(req.ChatType))
	req.GroupID = strings.TrimSpace(req.GroupID)
	req.UserID = strings.TrimSpace(req.UserID)
	req.Count = normalizeAIRecentSyncCount(req.Count)
	if req.ConnectionID == "" {
		return AIRecentMessagesSyncResult{}, fmt.Errorf("连接 ID 不能为空")
	}

	switch req.ChatType {
	case "group":
		if req.GroupID == "" {
			return AIRecentMessagesSyncResult{}, fmt.Errorf("群号不能为空")
		}
	case "private":
		if req.UserID == "" {
			return AIRecentMessagesSyncResult{}, fmt.Errorf("QQ 号不能为空")
		}
	default:
		return AIRecentMessagesSyncResult{}, fmt.Errorf("会话类型仅支持 group 或 private")
	}

	items, err := s.messenger.GetRecentMessages(ctx, adapter.RecentMessagesRequest{
		ConnectionID: req.ConnectionID,
		ChatType:     req.ChatType,
		GroupID:      req.GroupID,
		UserID:       req.UserID,
		Count:        req.Count,
	})
	if err != nil {
		return AIRecentMessagesSyncResult{}, err
	}
	if len(items) > req.Count {
		items = items[len(items)-req.Count:]
	}

	selfID := s.connectionSelfID(req.ConnectionID)
	events := make([]event.Event, 0, len(items))
	for _, item := range items {
		evt := recentMessageEvent(req.ConnectionID, req.ChatType, req.GroupID, req.UserID, selfID, item)
		if strings.TrimSpace(evt.MessageID) == "" {
			continue
		}
		events = append(events, evt)
	}

	synced, err := s.aiService.ImportRecentEvents(ctx, events)
	if err != nil {
		return AIRecentMessagesSyncResult{}, err
	}

	return AIRecentMessagesSyncResult{
		Accepted:     true,
		ConnectionID: req.ConnectionID,
		ChatType:     req.ChatType,
		GroupID:      req.GroupID,
		UserID:       req.UserID,
		Requested:    req.Count,
		Fetched:      len(items),
		Synced:       synced,
		SyncedAt:     time.Now(),
		Message:      fmt.Sprintf("读取 %d 条，写入 %d 条最近消息", len(items), synced),
	}, nil
}

func (s *Service) DiscoverAIProviderModels(ctx context.Context, provider config.AIProviderConfig) (AIProviderModelsResult, error) {
	kind := strings.TrimSpace(provider.Kind)
	if kind == "" {
		kind = "openai_compatible"
	}
	if kind != "openai_compatible" {
		return AIProviderModelsResult{}, fmt.Errorf("当前只支持 openai_compatible 类型的模型发现")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	if baseURL == "" {
		return AIProviderModelsResult{}, fmt.Errorf("基础地址不能为空")
	}

	timeout := time.Duration(provider.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if timeout > 120*time.Second {
		timeout = 120 * time.Second
	}

	endpoint := baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return AIProviderModelsResult{}, fmt.Errorf("创建模型列表请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if apiKey := strings.TrimSpace(provider.APIKey); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return AIProviderModelsResult{}, fmt.Errorf("获取模型列表失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return AIProviderModelsResult{}, fmt.Errorf("读取模型列表响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return AIProviderModelsResult{}, fmt.Errorf("获取模型列表失败: HTTP %d %s", resp.StatusCode, truncateProviderResponse(body))
	}

	models, err := parseProviderModels(body)
	if err != nil {
		return AIProviderModelsResult{}, err
	}
	return AIProviderModelsResult{
		Accepted:  true,
		Models:    models,
		FetchedAt: time.Now(),
		Message:   fmt.Sprintf("已获取 %d 个可用模型", len(models)),
	}, nil
}

func (s *Service) SyncAllAIRecentMessages(ctx context.Context, req AIRecentMessagesBulkSyncRequest) (AIRecentMessagesBulkSyncResult, error) {
	if s.aiService == nil {
		return AIRecentMessagesBulkSyncResult{}, fmt.Errorf("AI 核心未初始化")
	}
	if s.messenger == nil {
		return AIRecentMessagesBulkSyncResult{}, fmt.Errorf("消息通道未初始化")
	}

	req.ConnectionID = strings.TrimSpace(req.ConnectionID)
	if req.ConnectionID == "" {
		req.ConnectionID = s.defaultActionConnectionID()
	}
	req.ChatType = strings.ToLower(strings.TrimSpace(req.ChatType))
	req.Count = normalizeAIRecentSyncCount(req.Count)
	if req.ConnectionID == "" {
		return AIRecentMessagesBulkSyncResult{}, fmt.Errorf("连接 ID 不能为空")
	}

	result := AIRecentMessagesBulkSyncResult{
		Accepted:     true,
		ConnectionID: req.ConnectionID,
		ChatType:     req.ChatType,
		Requested:    req.Count,
	}

	switch req.ChatType {
	case "group":
		groups, err := s.messenger.GetGroupList(ctx, req.ConnectionID)
		if err != nil {
			return AIRecentMessagesBulkSyncResult{}, err
		}
		for _, group := range groups {
			if item, ok := buildGroupDisplayHint(req.ConnectionID, group); ok {
				s.rememberBulkSyncDisplayHint(ctx, item)
			}
			groupID := strings.TrimSpace(group.GroupID)
			if groupID == "" {
				continue
			}
			result.Targets++
			syncResult, err := s.SyncAIRecentMessages(ctx, AIRecentMessagesSyncRequest{
				ConnectionID: req.ConnectionID,
				ChatType:     "group",
				GroupID:      groupID,
				Count:        req.Count,
			})
			if err != nil {
				result.Failed++
				continue
			}
			result.Fetched += syncResult.Fetched
			result.Synced += syncResult.Synced
		}
		result.Message = fmt.Sprintf("同步 %d 个群聊，读取 %d 条，写入 %d 条，失败 %d 个", result.Targets, result.Fetched, result.Synced, result.Failed)
	case "private":
		friends, err := s.messenger.GetFriendList(ctx, req.ConnectionID)
		if err != nil {
			return AIRecentMessagesBulkSyncResult{}, err
		}
		for _, friend := range friends {
			if item, ok := buildPrivateDisplayHint(req.ConnectionID, friend); ok {
				s.rememberBulkSyncDisplayHint(ctx, item)
			}
			userID := strings.TrimSpace(friend.UserID)
			if userID == "" {
				continue
			}
			result.Targets++
			syncResult, err := s.SyncAIRecentMessages(ctx, AIRecentMessagesSyncRequest{
				ConnectionID: req.ConnectionID,
				ChatType:     "private",
				UserID:       userID,
				Count:        req.Count,
			})
			if err != nil {
				result.Failed++
				continue
			}
			result.Fetched += syncResult.Fetched
			result.Synced += syncResult.Synced
		}
		result.Message = fmt.Sprintf("同步 %d 个私聊，读取 %d 条，写入 %d 条，失败 %d 个", result.Targets, result.Fetched, result.Synced, result.Failed)
	default:
		return AIRecentMessagesBulkSyncResult{}, fmt.Errorf("会话类型仅支持 group 或 private")
	}

	result.SyncedAt = time.Now()
	return result, nil
}

func (s *Service) rememberBulkSyncDisplayHint(ctx context.Context, item ai.MessageLog) {
	if s.aiService == nil {
		return
	}
	if err := s.aiService.RememberMessageDisplay(ctx, item); err != nil {
		s.logger.Debug("批量同步预热显示缓存失败", "connection_id", item.ConnectionID, "chat_type", item.ChatType, "group_id", item.GroupID, "user_id", item.UserID, "error", err)
	}
}

func buildGroupDisplayHint(connectionID string, group adapter.GroupInfo) (ai.MessageLog, bool) {
	connectionID = strings.TrimSpace(connectionID)
	groupID := strings.TrimSpace(group.GroupID)
	groupName := strings.TrimSpace(group.GroupName)
	if connectionID == "" || groupID == "" || groupName == "" {
		return ai.MessageLog{}, false
	}
	return ai.MessageLog{
		ConnectionID: connectionID,
		ChatType:     "group",
		GroupID:      groupID,
		GroupName:    groupName,
	}, true
}

func buildPrivateDisplayHint(connectionID string, user adapter.UserInfo) (ai.MessageLog, bool) {
	connectionID = strings.TrimSpace(connectionID)
	userID := strings.TrimSpace(user.UserID)
	nickname := strings.TrimSpace(user.Nickname)
	if connectionID == "" || userID == "" || nickname == "" {
		return ai.MessageLog{}, false
	}
	return ai.MessageLog{
		ConnectionID:   connectionID,
		ChatType:       "private",
		UserID:         userID,
		SenderRole:     "user",
		SenderName:     nickname,
		SenderNickname: nickname,
	}, true
}

func (s *Service) SendAIMessage(ctx context.Context, req AIMessageSendRequest) (AIMessageSendResult, error) {
	req.ConnectionID = strings.TrimSpace(req.ConnectionID)
	req.ChatType = strings.ToLower(strings.TrimSpace(req.ChatType))
	req.GroupID = strings.TrimSpace(req.GroupID)
	req.UserID = strings.TrimSpace(req.UserID)
	req.Text = strings.TrimSpace(req.Text)

	if req.ConnectionID == "" {
		return AIMessageSendResult{}, fmt.Errorf("连接 ID 不能为空")
	}
	if req.Text == "" {
		return AIMessageSendResult{}, fmt.Errorf("消息内容不能为空")
	}

	target := message.Target{
		ConnectionID: req.ConnectionID,
		ChatType:     req.ChatType,
		GroupID:      req.GroupID,
		UserID:       req.UserID,
	}
	switch target.ChatType {
	case "group":
		if target.GroupID == "" {
			return AIMessageSendResult{}, fmt.Errorf("群号不能为空")
		}
	case "private":
		if target.UserID == "" {
			return AIMessageSendResult{}, fmt.Errorf("QQ 号不能为空")
		}
	default:
		return AIMessageSendResult{}, fmt.Errorf("会话类型仅支持 group 或 private")
	}

	if s.messenger == nil {
		return AIMessageSendResult{}, fmt.Errorf("消息通道未初始化")
	}
	if err := s.messenger.SendText(ctx, target, req.Text); err != nil {
		return AIMessageSendResult{}, err
	}

	return AIMessageSendResult{
		Accepted:     true,
		ConnectionID: target.ConnectionID,
		ChatType:     target.ChatType,
		GroupID:      target.GroupID,
		UserID:       target.UserID,
		SentAt:       time.Now(),
		Message:      "消息已发送",
	}, nil
}

func normalizeAIRecentSyncCount(count int) int {
	if count <= 0 {
		return defaultAIRecentSyncCount
	}
	if count > maxAIRecentSyncCount {
		return maxAIRecentSyncCount
	}
	return count
}

func parseProviderModels(body []byte) ([]AIProviderModel, error) {
	var payload struct {
		Data   []json.RawMessage `json:"data"`
		Models []json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}

	rawItems := payload.Data
	if len(rawItems) == 0 {
		rawItems = payload.Models
	}

	seen := make(map[string]struct{}, len(rawItems))
	models := make([]AIProviderModel, 0, len(rawItems))
	for _, raw := range rawItems {
		model := parseProviderModel(raw)
		if strings.TrimSpace(model.ID) == "" {
			continue
		}
		if _, ok := seen[model.ID]; ok {
			continue
		}
		seen[model.ID] = struct{}{}
		models = append(models, model)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("模型列表为空")
	}
	return models, nil
}

func parseProviderModel(raw json.RawMessage) AIProviderModel {
	var id string
	if err := json.Unmarshal(raw, &id); err == nil {
		return AIProviderModel{ID: strings.TrimSpace(id)}
	}

	var item struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Model   string `json:"model"`
		OwnedBy string `json:"owned_by"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return AIProviderModel{}
	}
	id = strings.TrimSpace(item.ID)
	if id == "" {
		id = strings.TrimSpace(item.Name)
	}
	if id == "" {
		id = strings.TrimSpace(item.Model)
	}

	return AIProviderModel{
		ID:      id,
		OwnedBy: strings.TrimSpace(item.OwnedBy),
	}
}

func truncateProviderResponse(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}
	if len(text) > 240 {
		return text[:240] + "..."
	}
	return text
}

func (s *Service) defaultActionConnectionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cfg != nil {
		for _, conn := range s.cfg.Connections {
			id := strings.TrimSpace(conn.ID)
			if id == "" {
				continue
			}
			if _, ok := s.actionClients[id]; ok {
				return id
			}
		}
	}
	for id := range s.actionClients {
		return id
	}
	return ""
}

func (s *Service) connectionSelfID(connectionID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot, ok := s.connections[strings.TrimSpace(connectionID)]
	if !ok {
		return ""
	}
	return strings.TrimSpace(snapshot.SelfID)
}

func recentMessageEvent(connectionID, chatType, fallbackGroupID, fallbackUserID, selfID string, item adapter.MessageDetail) event.Event {
	groupID := strings.TrimSpace(item.GroupID)
	if groupID == "" {
		groupID = strings.TrimSpace(fallbackGroupID)
	}
	userID := strings.TrimSpace(item.UserID)
	if userID == "" {
		userID = strings.TrimSpace(fallbackUserID)
	}
	actualChatType := strings.TrimSpace(item.MessageType)
	if actualChatType == "" {
		actualChatType = strings.TrimSpace(chatType)
	}
	meta := map[string]string{
		"self_id": strings.TrimSpace(selfID),
	}
	addRecentSenderMeta(meta, item.Sender)
	return event.Event{
		ID:           "history:" + strings.TrimSpace(item.MessageID),
		ConnectionID: strings.TrimSpace(connectionID),
		Platform:     "onebot_v11",
		Kind:         "message",
		ChatType:     actualChatType,
		UserID:       userID,
		GroupID:      groupID,
		MessageID:    strings.TrimSpace(item.MessageID),
		RawText:      strings.TrimSpace(item.RawMessage),
		Segments:     recentMessageSegments(item.Message, item.RawMessage),
		Timestamp:    recentMessageTime(item.Time),
		Meta:         meta,
	}
}

func recentMessageTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now()
	}
	return value
}

func addRecentSenderMeta(meta map[string]string, sender map[string]any) {
	if len(sender) == 0 {
		return
	}
	if card := recentString(sender["card"]); card != "" {
		meta["sender_card"] = card
	}
	if nickname := recentString(sender["nickname"]); nickname != "" {
		meta["sender_nickname"] = nickname
		meta["nickname"] = nickname
	}
}

func recentMessageSegments(raw json.RawMessage, fallback string) []message.Segment {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		if strings.TrimSpace(fallback) == "" {
			return nil
		}
		return []message.Segment{message.Text(fallback)}
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []message.Segment{message.Text(text)}
	}

	var segments []message.Segment
	if err := json.Unmarshal(raw, &segments); err == nil {
		return segments
	}

	var segment message.Segment
	if err := json.Unmarshal(raw, &segment); err == nil && strings.TrimSpace(segment.Type) != "" {
		return []message.Segment{segment}
	}

	if strings.TrimSpace(fallback) == "" {
		return nil
	}
	return []message.Segment{message.Text(fallback)}
}

func recentString(value any) string {
	switch x := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return strings.TrimSpace(x.String())
	case float64:
		if math.Trunc(x) == x {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func aiForwardCacheKey(connectionID, forwardID string) string {
	return strings.TrimSpace(connectionID) + "\x00" + strings.TrimSpace(forwardID)
}

func (s *Service) cachedAIForwardMessage(key string) (AIForwardMessageView, bool) {
	s.forwardCacheMu.RLock()
	defer s.forwardCacheMu.RUnlock()
	if s.forwardCache == nil {
		return AIForwardMessageView{}, false
	}
	item, ok := s.forwardCache[key]
	return item, ok
}

func (s *Service) storeAIForwardMessage(key string, view AIForwardMessageView) {
	s.forwardCacheMu.Lock()
	defer s.forwardCacheMu.Unlock()
	if s.forwardCache == nil {
		s.forwardCache = make(map[string]AIForwardMessageView)
	}
	if _, exists := s.forwardCache[key]; !exists && len(s.forwardCache) >= maxAIForwardCacheEntries {
		oldestKey := ""
		var oldest time.Time
		for cacheKey, item := range s.forwardCache {
			if oldestKey == "" || item.FetchedAt.Before(oldest) {
				oldestKey = cacheKey
				oldest = item.FetchedAt
			}
		}
		if oldestKey != "" {
			delete(s.forwardCache, oldestKey)
		}
	}
	s.forwardCache[key] = view
}

func (s *Service) ResolveAIMessageImagePreview(ctx context.Context, messageID string, segmentIndex int) (ai.MessageImagePreview, error) {
	if s.aiService == nil {
		return ai.MessageImagePreview{}, fmt.Errorf("AI 核心未初始化")
	}
	return s.aiService.ResolveMessageImagePreview(ctx, messageID, segmentIndex)
}

func (s *Service) RunAIReflection(ctx context.Context) (AIMemoryActionResult, error) {
	if s.aiService == nil {
		return AIMemoryActionResult{}, fmt.Errorf("AI 核心未初始化")
	}
	summary, err := s.aiService.RunReflectionOnce(ctx)
	if err != nil {
		return AIMemoryActionResult{}, err
	}
	message := strings.TrimSpace(summary)
	if message == "" {
		message = "AI 后台整理已执行"
	}
	view := s.AIView()
	if warning := strings.TrimSpace(view.Snapshot.LastReflectionError); warning != "" {
		message += "（有告警，请查看最近整理结果）"
	}
	return AIMemoryActionResult{
		Accepted: true,
		Action:   "run",
		Target:   "reflection",
		ID:       "core",
		View:     view,
		Message:  message,
	}, nil
}

func (s *Service) AnalyzeAIRelations(ctx context.Context, req AIRelationAnalysisRequest) (AIRelationAnalysisResult, error) {
	if s.aiService == nil {
		return AIRelationAnalysisResult{}, fmt.Errorf("AI 核心未初始化")
	}
	result, err := s.aiService.GenerateRelationAnalysis(ctx, req.GroupID, req.Force)
	if err != nil {
		return AIRelationAnalysisResult{}, err
	}
	message := "AI 关系分析已生成"
	if strings.TrimSpace(result.GroupID) != "" {
		message = "AI 群关系分析已生成"
	}
	if result.CacheHit {
		message += "（使用缓存）"
	}
	return AIRelationAnalysisResult{
		Accepted:    true,
		GroupID:     result.GroupID,
		Markdown:    result.Markdown,
		GeneratedAt: result.GeneratedAt,
		ExpiresAt:   result.ExpiresAt,
		UserCount:   result.UserCount,
		EdgeCount:   result.EdgeCount,
		MemoryCount: result.MemoryCount,
		InputHash:   result.InputHash,
		CacheHit:    result.CacheHit,
		Message:     message,
	}, nil
}

func (s *Service) PromoteAICandidateMemory(ctx context.Context, id string) (AIMemoryActionResult, error) {
	if s.aiService == nil {
		return AIMemoryActionResult{}, fmt.Errorf("AI 核心未初始化")
	}
	if err := s.aiService.PromoteCandidateMemory(ctx, id); err != nil {
		return AIMemoryActionResult{}, err
	}
	return AIMemoryActionResult{
		Accepted: true,
		Action:   "promote",
		Target:   "candidate_memory",
		ID:       id,
		View:     s.AIView(),
		Message:  "候选记忆已晋升为长期记忆",
	}, nil
}

func (s *Service) DeleteAICandidateMemory(ctx context.Context, id string) (AIMemoryActionResult, error) {
	if s.aiService == nil {
		return AIMemoryActionResult{}, fmt.Errorf("AI 核心未初始化")
	}
	if err := s.aiService.DeleteCandidateMemory(ctx, id); err != nil {
		return AIMemoryActionResult{}, err
	}
	return AIMemoryActionResult{
		Accepted: true,
		Action:   "delete",
		Target:   "candidate_memory",
		ID:       id,
		View:     s.AIView(),
		Message:  "候选记忆已删除",
	}, nil
}

func (s *Service) DeleteAILongTermMemory(ctx context.Context, id string) (AIMemoryActionResult, error) {
	if s.aiService == nil {
		return AIMemoryActionResult{}, fmt.Errorf("AI 核心未初始化")
	}
	if err := s.aiService.DeleteLongTermMemory(ctx, id); err != nil {
		return AIMemoryActionResult{}, err
	}
	return AIMemoryActionResult{
		Accepted: true,
		Action:   "delete",
		Target:   "long_term_memory",
		ID:       id,
		View:     s.AIView(),
		Message:  "长期记忆已删除",
	}, nil
}

func (s *Service) SaveConnectionConfig(ctx context.Context, conn config.ConnectionConfig) (ConnectionSaveResult, error) {
	s.mu.RLock()
	currentCfg := s.cfg
	running := s.state == StateRunning
	s.mu.RUnlock()

	nextCfg, err := config.Clone(currentCfg)
	if err != nil {
		return ConnectionSaveResult{}, err
	}

	normalized := normalizeConnectionConfig(conn)
	found := false
	for i := range nextCfg.Connections {
		if nextCfg.Connections[i].ID != normalized.ID {
			continue
		}
		nextCfg.Connections[i] = normalized
		found = true
		break
	}
	if !found {
		nextCfg.Connections = append(nextCfg.Connections, normalized)
	}
	nextCfg, err = config.MergeSensitiveValues(currentCfg, nextCfg)
	if err != nil {
		return ConnectionSaveResult{}, err
	}

	result, err := config.Save(s.configPath, nextCfg)
	if err != nil {
		return ConnectionSaveResult{}, err
	}

	hotApplyErr := s.rebuildConnections(ctx, nextCfg.Connections)

	s.mu.Lock()
	s.cfg.Connections = cloneConnectionConfigs(nextCfg.Connections)
	s.mu.Unlock()

	detail, ok := s.ConnectionDetail(normalized.ID)
	if !ok {
		return ConnectionSaveResult{}, fmt.Errorf("连接详情不存在: %s", normalized.ID)
	}

	message := "网络配置已保存"
	switch {
	case hotApplyErr != nil:
		message = "网络配置已保存，但热应用失败，请重启实例后生效"
	case running:
		message = "网络配置已保存并已热应用"
	default:
		message = "网络配置已保存，将在实例启动时加载"
	}

	return ConnectionSaveResult{
		Accepted:        true,
		Persisted:       true,
		RestartRequired: hotApplyErr != nil,
		HotApplied:      running && hotApplyErr == nil,
		HotApplyError:   errorString(hotApplyErr),
		ConnectionID:    normalized.ID,
		Path:            result.TargetPath,
		BackupPath:      result.BackupPath,
		SavedAt:         result.SavedAt,
		Detail:          detail,
		Message:         message,
	}, nil
}

func (s *Service) SetConnectionEnabled(ctx context.Context, id string, enabled bool) (ConnectionSaveResult, error) {
	s.mu.RLock()
	currentCfg := s.cfg
	running := s.state == StateRunning
	s.mu.RUnlock()

	nextCfg, err := config.Clone(currentCfg)
	if err != nil {
		return ConnectionSaveResult{}, err
	}

	found := false
	for i := range nextCfg.Connections {
		if nextCfg.Connections[i].ID != id {
			continue
		}
		nextCfg.Connections[i].Enabled = enabled
		found = true
		break
	}
	if !found {
		return ConnectionSaveResult{}, fmt.Errorf("连接不存在: %s", id)
	}

	result, err := config.Save(s.configPath, nextCfg)
	if err != nil {
		return ConnectionSaveResult{}, err
	}

	hotApplyErr := s.rebuildConnections(ctx, nextCfg.Connections)

	s.mu.Lock()
	s.cfg.Connections = cloneConnectionConfigs(nextCfg.Connections)
	s.mu.Unlock()

	detail, ok := s.ConnectionDetail(id)
	if !ok {
		return ConnectionSaveResult{}, fmt.Errorf("连接详情不存在: %s", id)
	}

	actionText := "启用"
	if !enabled {
		actionText = "停用"
	}
	message := "网络连接已" + actionText
	switch {
	case hotApplyErr != nil:
		message = "网络连接已" + actionText + "，但热应用失败，请重启实例后生效"
	case running:
		message = "网络连接已" + actionText + "并已热应用"
	default:
		message = "网络连接已" + actionText + "，将在实例启动时生效"
	}

	return ConnectionSaveResult{
		Accepted:        true,
		Persisted:       true,
		RestartRequired: hotApplyErr != nil,
		HotApplied:      running && hotApplyErr == nil,
		HotApplyError:   errorString(hotApplyErr),
		ConnectionID:    id,
		Path:            result.TargetPath,
		BackupPath:      result.BackupPath,
		SavedAt:         result.SavedAt,
		Detail:          detail,
		Message:         message,
	}, nil
}

func (s *Service) DeleteConnection(ctx context.Context, id string) (ConnectionSaveResult, error) {
	s.mu.RLock()
	currentCfg := s.cfg
	running := s.state == StateRunning
	s.mu.RUnlock()

	nextCfg, err := config.Clone(currentCfg)
	if err != nil {
		return ConnectionSaveResult{}, err
	}

	nextConnections := make([]config.ConnectionConfig, 0, len(nextCfg.Connections))
	found := false
	for _, conn := range nextCfg.Connections {
		if conn.ID == id {
			found = true
			continue
		}
		nextConnections = append(nextConnections, conn)
	}
	if !found {
		return ConnectionSaveResult{}, fmt.Errorf("连接不存在: %s", id)
	}
	nextCfg.Connections = nextConnections

	result, err := config.Save(s.configPath, nextCfg)
	if err != nil {
		return ConnectionSaveResult{}, err
	}

	hotApplyErr := s.rebuildConnections(ctx, nextCfg.Connections)

	s.mu.Lock()
	s.cfg.Connections = cloneConnectionConfigs(nextCfg.Connections)
	s.mu.Unlock()

	message := "网络配置已删除"
	switch {
	case hotApplyErr != nil:
		message = "网络配置已删除，但热应用失败，请重启实例后生效"
	case running:
		message = "网络配置已删除并已热应用"
	default:
		message = "网络配置已删除，将在实例启动时生效"
	}

	return ConnectionSaveResult{
		Accepted:        true,
		Persisted:       true,
		RestartRequired: hotApplyErr != nil,
		HotApplied:      running && hotApplyErr == nil,
		HotApplyError:   errorString(hotApplyErr),
		ConnectionID:    id,
		Path:            result.TargetPath,
		BackupPath:      result.BackupPath,
		SavedAt:         result.SavedAt,
		Message:         message,
	}, nil
}

func (s *Service) SavePluginConfig(ctx context.Context, id string, enabled bool, pluginConfig map[string]any) (PluginConfigSaveResult, error) {
	if err := s.syncExternalPlugins(); err != nil {
		return PluginConfigSaveResult{}, err
	}

	s.mu.RLock()
	currentCfg := s.cfg
	running := s.state == StateRunning
	s.mu.RUnlock()

	nextCfg, err := config.Clone(currentCfg)
	if err != nil {
		return PluginConfigSaveResult{}, err
	}

	manifest, hasManifest := s.host.Manifest(id)
	found := false
	for i := range nextCfg.Plugins {
		if nextCfg.Plugins[i].ID != id {
			continue
		}
		nextCfg.Plugins[i].Enabled = enabled
		nextCfg.Plugins[i].Kind = configuredPluginKind(nextCfg.Plugins[i].Kind, manifest, hasManifest)
		nextCfg.Plugins[i].Config = cloneConfigMap(pluginConfig)
		found = true
		break
	}

	if !found {
		if !hasManifest {
			return PluginConfigSaveResult{}, fmt.Errorf("插件未注册: %s", id)
		}
		nextCfg.Plugins = append(nextCfg.Plugins, config.PluginConfig{
			ID:      id,
			Kind:    configuredPluginKind("", manifest, true),
			Enabled: enabled,
			Config:  cloneConfigMap(pluginConfig),
		})
	}
	nextCfg, err = config.MergeSensitiveValues(currentCfg, nextCfg)
	if err != nil {
		return PluginConfigSaveResult{}, err
	}
	nextCfg.Plugins = s.normalizePluginConfigs(nextCfg.Plugins, currentCfg.Plugins)
	if cfgFound, ok := findConfiguredPlugin(nextCfg.Plugins, id); ok && hasManifest {
		if err := validatePluginConfigWithSchema(manifest, cfgFound); err != nil {
			return PluginConfigSaveResult{}, err
		}
	}

	result, err := config.Save(s.configPath, nextCfg)
	if err != nil {
		return PluginConfigSaveResult{}, err
	}

	var hotApplyErr error
	hotApplied := false
	if running {
		if err := s.host.Apply(ctx, nextCfg.Plugins); err != nil {
			hotApplyErr = err
			s.logger.Error("热应用插件配置失败", "plugin", id, "error", err)
		} else {
			hotApplied = true
		}
	} else {
		s.host.SetConfigured(nextCfg.Plugins)
	}

	s.mu.Lock()
	s.cfg.Plugins = nextCfg.Plugins
	s.mu.Unlock()

	detail, ok := s.PluginDetail(id)
	if !ok {
		return PluginConfigSaveResult{}, fmt.Errorf("插件详情不存在: %s", id)
	}

	message := "插件配置已保存"
	switch {
	case hotApplyErr != nil:
		message = "插件配置已保存，但热应用失败，请重启实例后生效"
	case running:
		message = "插件配置已保存并已热应用"
	default:
		message = "插件配置已保存，将在实例启动时加载"
	}

	return PluginConfigSaveResult{
		Accepted:        true,
		Persisted:       true,
		RestartRequired: hotApplyErr != nil,
		HotApplied:      hotApplied,
		HotApplyError:   errorString(hotApplyErr),
		PluginID:        id,
		Path:            result.TargetPath,
		BackupPath:      result.BackupPath,
		SavedAt:         result.SavedAt,
		Detail:          detail,
		Message:         message,
	}, nil
}

func (s *Service) AdminAuthStatus() AuthStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configured := strings.TrimSpace(s.cfg.Security.AdminAuth.Password) != ""
	return AuthStatus{
		Enabled:       s.cfg.Security.AdminAuth.Enabled,
		Configured:    configured,
		RequiresSetup: !configured,
	}
}

func (s *Service) ConfigureAdminAuth(ctx context.Context, password string) (ConfigSaveResult, error) {
	_ = ctx

	if err := config.ValidateAdminPassword(password); err != nil {
		return ConfigSaveResult{}, err
	}

	passwordHash, err := config.HashAdminPassword(password)
	if err != nil {
		return ConfigSaveResult{}, err
	}

	s.mu.RLock()
	currentCfg := s.cfg
	s.mu.RUnlock()

	nextCfg, err := config.Clone(currentCfg)
	if err != nil {
		return ConfigSaveResult{}, err
	}
	nextCfg.Security.AdminAuth.Enabled = true
	nextCfg.Security.AdminAuth.Password = passwordHash

	result, err := config.Save(s.configPath, nextCfg)
	if err != nil {
		return ConfigSaveResult{}, err
	}

	s.mu.Lock()
	s.cfg.Security.AdminAuth = nextCfg.Security.AdminAuth
	s.mu.Unlock()

	return ConfigSaveResult{
		Accepted:         true,
		Persisted:        true,
		RestartRequired:  false,
		SourcePath:       result.SourcePath,
		Path:             result.TargetPath,
		BackupPath:       result.BackupPath,
		SavedAt:          result.SavedAt,
		NormalizedConfig: config.SanitizedMap(nextCfg),
		Message:          "后台密码已设置",
	}, nil
}

func (s *Service) ChangeAdminPassword(ctx context.Context, currentPassword, newPassword string) (ConfigSaveResult, error) {
	_ = ctx

	s.mu.RLock()
	currentCfg := s.cfg
	stored := s.cfg.Security.AdminAuth.Password
	enabled := s.cfg.Security.AdminAuth.Enabled
	s.mu.RUnlock()

	if !enabled || strings.TrimSpace(stored) == "" {
		return ConfigSaveResult{}, fmt.Errorf("后台密码尚未初始化")
	}
	if !config.VerifyAdminPassword(stored, currentPassword) {
		return ConfigSaveResult{}, fmt.Errorf("当前密码不正确")
	}
	if err := config.ValidateAdminPassword(newPassword); err != nil {
		return ConfigSaveResult{}, err
	}

	passwordHash, err := config.HashAdminPassword(newPassword)
	if err != nil {
		return ConfigSaveResult{}, err
	}

	nextCfg, err := config.Clone(currentCfg)
	if err != nil {
		return ConfigSaveResult{}, err
	}
	nextCfg.Security.AdminAuth.Enabled = true
	nextCfg.Security.AdminAuth.Password = passwordHash

	result, err := config.Save(s.configPath, nextCfg)
	if err != nil {
		return ConfigSaveResult{}, err
	}

	s.mu.Lock()
	s.cfg.Security.AdminAuth = nextCfg.Security.AdminAuth
	s.mu.Unlock()

	return ConfigSaveResult{
		Accepted:         true,
		Persisted:        true,
		RestartRequired:  false,
		SourcePath:       result.SourcePath,
		Path:             result.TargetPath,
		BackupPath:       result.BackupPath,
		SavedAt:          result.SavedAt,
		NormalizedConfig: config.SanitizedMap(nextCfg),
		Message:          "后台密码已更新",
	}, nil
}

func (s *Service) VerifyAdminPassword(password string) bool {
	s.mu.RLock()
	stored := s.cfg.Security.AdminAuth.Password
	s.mu.RUnlock()
	return config.VerifyAdminPassword(stored, password)
}

func (s *Service) AuditLogs(limit int) []AuditLogEntry {
	s.auditMu.RLock()
	defer s.auditMu.RUnlock()

	if len(s.auditLogs) == 0 {
		return nil
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > s.auditLimit {
		limit = s.auditLimit
	}
	if limit > len(s.auditLogs) {
		limit = len(s.auditLogs)
	}

	out := make([]AuditLogEntry, 0, limit)
	for i := len(s.auditLogs) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.auditLogs[i])
	}
	return out
}

func (s *Service) RecordAuditLog(entry AuditLogEntry) {
	entry.Category = strings.TrimSpace(entry.Category)
	entry.Action = strings.TrimSpace(entry.Action)
	entry.Target = strings.TrimSpace(entry.Target)
	entry.Result = strings.TrimSpace(entry.Result)
	entry.Summary = strings.TrimSpace(entry.Summary)
	entry.Detail = strings.TrimSpace(entry.Detail)
	entry.Username = strings.TrimSpace(entry.Username)
	entry.RemoteAddr = strings.TrimSpace(entry.RemoteAddr)
	entry.Method = strings.TrimSpace(entry.Method)
	entry.Path = strings.TrimSpace(entry.Path)
	if entry.At.IsZero() {
		entry.At = time.Now()
	}

	s.auditMu.Lock()
	defer s.auditMu.Unlock()

	s.auditLogs = append(s.auditLogs, entry)
	if overflow := len(s.auditLogs) - s.auditLimit; overflow > 0 {
		s.auditLogs = append([]AuditLogEntry(nil), s.auditLogs[overflow:]...)
	}
}

func (s *Service) ConnectionSnapshots() []adapter.ConnectionSnapshot {
	s.mu.RLock()
	out := make([]adapter.ConnectionSnapshot, 0, len(s.connections))
	for _, snapshot := range s.connections {
		out = append(out, snapshot)
	}
	s.mu.RUnlock()

	for i := range out {
		if ingress, ok := s.ingresses[out[i].ID]; ok {
			ingressSnapshot := ingress.Snapshot()
			out[i].IngressState = ingressSnapshot.State
			out[i].ConnectedClients = ingressSnapshot.ConnectedClients
			out[i].ObservedEvents = ingressSnapshot.ObservedEvents
			out[i].LastEventAt = ingressSnapshot.LastEventAt
			if out[i].LastError == "" {
				out[i].LastError = ingressSnapshot.LastError
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Service) ConnectionDetail(id string) (ConnectionDetail, bool) {
	var cfgFound *config.ConnectionConfig
	for i := range s.cfg.Connections {
		if s.cfg.Connections[i].ID == id {
			cfgFound = &s.cfg.Connections[i]
			break
		}
	}
	if cfgFound == nil {
		return ConnectionDetail{}, false
	}

	var snapshot adapter.ConnectionSnapshot
	found := false
	for _, item := range s.ConnectionSnapshots() {
		if item.ID == id {
			snapshot = item
			found = true
			break
		}
	}
	if !found {
		return ConnectionDetail{}, false
	}

	configView, _ := config.SanitizeValue(cfgFound).(map[string]any)
	return ConnectionDetail{
		Snapshot: snapshot,
		Config:   configView,
	}, true
}

func (s *Service) PluginSnapshots() []host.Snapshot {
	_ = s.syncExternalPlugins()
	return s.host.Snapshots()
}

func (s *Service) PluginDetail(id string) (PluginDetail, bool) {
	_ = s.syncExternalPlugins()
	detail, ok := s.host.Detail(id)
	if !ok {
		return PluginDetail{}, false
	}

	configView, _ := config.SanitizeValue(detail.Config).(map[string]any)
	manifest, _ := s.host.Manifest(id)
	schema, schemaPath, schemaErr := loadPluginSchema(manifest)
	return PluginDetail{
		Snapshot:          detail.Snapshot,
		Config:            configView,
		Runtime:           detail.Runtime,
		ConfigSchema:      schema,
		ConfigSchemaPath:  schemaPath,
		ConfigSchemaError: schemaErr,
	}, true
}

func (s *Service) DebugPluginAPI(ctx context.Context, id string, req PluginAPIDebugRequest) (PluginAPIDebugResult, error) {
	if err := s.syncExternalPlugins(); err != nil {
		return PluginAPIDebugResult{}, err
	}

	method := strings.TrimSpace(req.Method)
	if method == "" {
		return PluginAPIDebugResult{}, fmt.Errorf("调试方法不能为空")
	}

	detail, ok := s.host.Detail(id)
	if !ok {
		return PluginAPIDebugResult{}, fmt.Errorf("插件未注册: %s", id)
	}
	if strings.TrimSpace(detail.Snapshot.Kind) != externalexec.KindExternalExec {
		return PluginAPIDebugResult{}, fmt.Errorf("仅 external_exec 插件支持 API 调试: %s", id)
	}

	result, err := s.DebugFrameworkPluginAPI(ctx, req)
	if err != nil {
		return PluginAPIDebugResult{}, err
	}
	result.PluginID = id
	return result, nil
}

func (s *Service) DebugFrameworkPluginAPI(ctx context.Context, req PluginAPIDebugRequest) (PluginAPIDebugResult, error) {
	method := strings.TrimSpace(req.Method)
	if method == "" {
		return PluginAPIDebugResult{}, fmt.Errorf("调试方法不能为空")
	}

	result := PluginAPIDebugResult{
		Accepted: true,
		Method:   method,
	}

	switch method {
	case "messenger.send_text":
		target, err := decodePluginDebugTarget(req.Payload)
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		text, err := pluginDebugRequiredString(req.Payload, "text")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		if err := s.messenger.SendText(ctx, target, text); err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = map[string]any{"sent": true}
		result.Message = "接口调用成功"
		return result, nil
	case "messenger.reply_text":
		target, err := decodePluginDebugTarget(req.Payload)
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		replyTo, err := pluginDebugRequiredString(req.Payload, "reply_to")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		text, err := pluginDebugRequiredString(req.Payload, "text")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		if err := s.messenger.ReplyText(ctx, target, replyTo, text); err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = map[string]any{"sent": true}
		result.Message = "接口调用成功"
		return result, nil
	case "messenger.send_segments":
		target, err := decodePluginDebugTarget(req.Payload)
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		rawSegments, err := pluginDebugRequiredValue(req.Payload, "segments")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		var segments []message.Segment
		if err := decodePluginDebugValue(rawSegments, &segments); err != nil {
			return PluginAPIDebugResult{}, fmt.Errorf("payload.segments 格式错误: %w", err)
		}
		if err := s.messenger.SendSegments(ctx, target, segments); err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = map[string]any{"sent": true}
		result.Message = "接口调用成功"
		return result, nil
	case externalexec.CallBotGetStrangerInfo:
		connectionID, err := pluginDebugRequiredString(req.Payload, "connection_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		userID, err := pluginDebugRequiredString(req.Payload, "user_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		info, err := s.messenger.GetStrangerInfo(ctx, connectionID, userID)
		if err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = info
		result.Message = "接口调用成功"
		return result, nil
	case externalexec.CallBotGetGroupInfo:
		connectionID, err := pluginDebugRequiredString(req.Payload, "connection_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		groupID, err := pluginDebugRequiredString(req.Payload, "group_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		item, err := s.messenger.GetGroupInfo(ctx, connectionID, groupID)
		if err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = item
		result.Message = "接口调用成功"
		return result, nil
	case externalexec.CallBotGetGroupMembers:
		connectionID, err := pluginDebugRequiredString(req.Payload, "connection_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		groupID, err := pluginDebugRequiredString(req.Payload, "group_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		items, err := s.messenger.GetGroupMemberList(ctx, connectionID, groupID)
		if err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = items
		result.Message = "接口调用成功"
		return result, nil
	case externalexec.CallBotGetGroupMember:
		connectionID, err := pluginDebugRequiredString(req.Payload, "connection_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		groupID, err := pluginDebugRequiredString(req.Payload, "group_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		userID, err := pluginDebugRequiredString(req.Payload, "user_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		item, err := s.messenger.GetGroupMemberInfo(ctx, connectionID, groupID, userID)
		if err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = item
		result.Message = "接口调用成功"
		return result, nil
	case externalexec.CallBotGetMessage:
		connectionID, err := pluginDebugRequiredString(req.Payload, "connection_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		messageID, err := pluginDebugRequiredString(req.Payload, "message_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		item, err := s.messenger.GetMessage(ctx, connectionID, messageID)
		if err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = item
		result.Message = "接口调用成功"
		return result, nil
	case externalexec.CallBotGetForwardMessage:
		connectionID, err := pluginDebugRequiredString(req.Payload, "connection_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		forwardID, err := pluginDebugRequiredString(req.Payload, "forward_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		item, err := s.messenger.GetForwardMessageInfo(ctx, connectionID, forwardID)
		if err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = item
		result.Message = "接口调用成功"
		return result, nil
	case externalexec.CallBotDeleteMessage:
		connectionID, err := pluginDebugRequiredString(req.Payload, "connection_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		messageID, err := pluginDebugRequiredString(req.Payload, "message_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		if err := s.messenger.DeleteMessage(ctx, connectionID, messageID); err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = map[string]any{"deleted": true}
		result.Message = "接口调用成功"
		return result, nil
	case externalexec.CallBotResolveMedia:
		connectionID, err := pluginDebugRequiredString(req.Payload, "connection_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		segmentType, err := pluginDebugRequiredString(req.Payload, "segment_type")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		file, err := pluginDebugRequiredString(req.Payload, "file")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		item, err := s.messenger.ResolveMediaInfo(ctx, connectionID, segmentType, file)
		if err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = item
		result.Message = "接口调用成功"
		return result, nil
	case externalexec.CallBotGetLoginInfo:
		connectionID, err := pluginDebugRequiredString(req.Payload, "connection_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		item, err := s.messenger.GetLoginInfo(ctx, connectionID)
		if err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = item
		result.Message = "接口调用成功"
		return result, nil
	case externalexec.CallBotGetStatus:
		connectionID, err := pluginDebugRequiredString(req.Payload, "connection_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		item, err := s.messenger.GetStatus(ctx, connectionID)
		if err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = item
		result.Message = "接口调用成功"
		return result, nil
	case externalexec.CallBotSendGroupForward:
		connectionID, err := pluginDebugRequiredString(req.Payload, "connection_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}
		groupID, err := pluginDebugRequiredString(req.Payload, "group_id")
		if err != nil {
			return PluginAPIDebugResult{}, err
		}

		var nodes []message.ForwardNode
		if rawNodes, ok := req.Payload["nodes"]; ok && rawNodes != nil {
			if err := decodePluginDebugValue(rawNodes, &nodes); err != nil {
				return PluginAPIDebugResult{}, fmt.Errorf("payload.nodes 格式错误: %w", err)
			}
		}

		var opts message.ForwardOptions
		if rawOptions, ok := req.Payload["options"]; ok && rawOptions != nil {
			if err := decodePluginDebugValue(rawOptions, &opts); err != nil {
				return PluginAPIDebugResult{}, fmt.Errorf("payload.options 格式错误: %w", err)
			}
		}

		if err := s.messenger.SendGroupForward(ctx, connectionID, groupID, nodes, opts); err != nil {
			result.Error = err.Error()
			result.Message = "接口调用返回错误"
			return result, nil
		}
		result.Result = map[string]any{"sent": true}
		result.Message = "接口调用成功"
		return result, nil
	default:
		return PluginAPIDebugResult{}, fmt.Errorf("不支持的调试方法: %s", method)
	}
}

func decodePluginDebugTarget(payload map[string]any) (message.Target, error) {
	rawTarget, err := pluginDebugRequiredValue(payload, "target")
	if err != nil {
		return message.Target{}, err
	}

	var target message.Target
	if err := decodePluginDebugValue(rawTarget, &target); err != nil {
		return message.Target{}, fmt.Errorf("payload.target 格式错误: %w", err)
	}

	target.ConnectionID = strings.TrimSpace(target.ConnectionID)
	target.ChatType = strings.ToLower(strings.TrimSpace(target.ChatType))
	target.UserID = strings.TrimSpace(target.UserID)
	target.GroupID = strings.TrimSpace(target.GroupID)
	target.ReplyTo = strings.TrimSpace(target.ReplyTo)

	if target.ConnectionID == "" {
		return message.Target{}, fmt.Errorf("payload.target.connection_id 不能为空")
	}

	switch target.ChatType {
	case "private":
		if target.UserID == "" {
			return message.Target{}, fmt.Errorf("payload.target.user_id 不能为空")
		}
	case "group":
		if target.GroupID == "" {
			return message.Target{}, fmt.Errorf("payload.target.group_id 不能为空")
		}
	default:
		return message.Target{}, fmt.Errorf("payload.target.chat_type 仅支持 private 或 group")
	}

	return target, nil
}

func (s *Service) InstallPlugin(ctx context.Context, id string) error {
	if manifest, ok := s.host.Manifest(id); ok && manifest.Builtin {
		return fmt.Errorf("内置插件不支持安装: %s", id)
	}
	return s.ensurePluginConfigured(ctx, id, false)
}

func (s *Service) StartPlugin(ctx context.Context, id string) error {
	return s.setPluginEnabled(ctx, id, true)
}

func (s *Service) StopPlugin(ctx context.Context, id string) error {
	return s.setPluginEnabled(ctx, id, false)
}

func (s *Service) ReloadPlugin(ctx context.Context, id string) error {
	if err := s.syncExternalPlugins(); err != nil {
		return err
	}
	s.mu.RLock()
	running := s.state == StateRunning
	s.mu.RUnlock()
	if !running {
		return fmt.Errorf("运行时未启动，无法重载插件")
	}
	return s.host.ReloadPlugin(ctx, id)
}

func (s *Service) RecoverPlugin(ctx context.Context, id string) error {
	if err := s.syncExternalPlugins(); err != nil {
		return err
	}
	s.mu.RLock()
	running := s.state == StateRunning
	s.mu.RUnlock()
	if !running {
		return fmt.Errorf("运行时未启动，无法恢复插件")
	}
	return s.host.RecoverPlugin(ctx, id)
}

func (s *Service) UninstallPlugin(ctx context.Context, id string) error {
	if err := s.syncExternalPlugins(); err != nil {
		return err
	}
	if manifest, ok := s.host.Manifest(id); ok && manifest.Builtin {
		return fmt.Errorf("内置插件不支持卸载: %s", id)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.RLock()
	currentCfg := s.cfg
	running := s.state == StateRunning
	s.mu.RUnlock()

	nextCfg, err := config.Clone(currentCfg)
	if err != nil {
		return err
	}

	nextPlugins := make([]config.PluginConfig, 0, len(nextCfg.Plugins))
	found := false
	for _, plugin := range nextCfg.Plugins {
		if plugin.ID == id {
			found = true
			continue
		}
		nextPlugins = append(nextPlugins, plugin)
	}
	if !found {
		return fmt.Errorf("插件未安装: %s", id)
	}
	nextCfg.Plugins = nextPlugins
	nextCfg.Plugins = s.normalizePluginConfigs(nextCfg.Plugins, currentCfg.Plugins)

	pluginDir, pluginDirExists, err := s.resolveExternalPluginDirectory(id)
	if err != nil {
		return err
	}

	deleteStagePath := ""
	movedPluginDir := false
	configSaved := false
	hostChanged := false
	rollback := func(baseErr error) error {
		var rollbackErrs []error
		if configSaved {
			if _, err := config.Save(s.configPath, currentCfg); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("恢复插件配置失败: %w", err))
			}
		}
		if movedPluginDir {
			if err := restorePluginDirectory(pluginDir, deleteStagePath); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("恢复插件目录失败: %w", err))
			}
		}
		if hostChanged {
			if running {
				if err := s.host.Apply(ctx, currentCfg.Plugins); err != nil {
					rollbackErrs = append(rollbackErrs, fmt.Errorf("恢复运行时插件状态失败: %w", err))
				}
			} else {
				s.host.SetConfigured(currentCfg.Plugins)
			}
		}
		if len(rollbackErrs) == 0 {
			return baseErr
		}
		return errors.Join(append([]error{baseErr}, rollbackErrs...)...)
	}

	if pluginDirExists {
		deleteStagePath = s.nextPluginDeleteStagePath(id)
		if err := os.MkdirAll(filepath.Dir(deleteStagePath), 0o755); err != nil {
			return fmt.Errorf("准备删除插件目录失败: %w", err)
		}
		if err := moveDir(pluginDir, deleteStagePath); err != nil {
			return fmt.Errorf("删除插件目录失败: %w", err)
		}
		movedPluginDir = true
	}

	if running {
		if err := s.host.Apply(ctx, nextCfg.Plugins); err != nil {
			return rollback(err)
		}
		hostChanged = true
	}

	if _, err := config.Save(s.configPath, nextCfg); err != nil {
		return rollback(err)
	}
	configSaved = true

	if !running {
		s.host.SetConfigured(nextCfg.Plugins)
		hostChanged = true
	} else {
		// host state already updated via Apply above.
	}

	if err := s.syncExternalPlugins(); err != nil {
		return rollback(err)
	}

	s.mu.Lock()
	s.cfg.Plugins = nextCfg.Plugins
	s.mu.Unlock()

	if movedPluginDir {
		if err := os.RemoveAll(deleteStagePath); err != nil {
			return fmt.Errorf("插件已删除，但清理临时目录失败: %w", err)
		}
	}
	if err := s.removePythonPluginEnv(id); err != nil {
		return fmt.Errorf("插件已卸载，但清理依赖环境失败: %w", err)
	}
	return nil
}

func (s *Service) setPluginEnabled(ctx context.Context, id string, enabled bool) error {
	if err := s.syncExternalPlugins(); err != nil {
		return err
	}
	s.mu.RLock()
	currentCfg := s.cfg
	running := s.state == StateRunning
	s.mu.RUnlock()

	nextCfg, err := config.Clone(currentCfg)
	if err != nil {
		return err
	}

	manifest, hasManifest := s.host.Manifest(id)

	found := false
	for i := range nextCfg.Plugins {
		if nextCfg.Plugins[i].ID != id {
			continue
		}
		nextCfg.Plugins[i].Enabled = enabled
		if strings.TrimSpace(nextCfg.Plugins[i].Kind) == "" {
			nextCfg.Plugins[i].Kind = configuredPluginKind(nextCfg.Plugins[i].Kind, manifest, hasManifest)
		}
		if nextCfg.Plugins[i].Config == nil {
			nextCfg.Plugins[i].Config = map[string]any{}
		}
		found = true
		break
	}

	if !found {
		if !enabled {
			if running {
				return s.host.StopPlugin(ctx, id)
			}
			return nil
		}

		if !hasManifest {
			return fmt.Errorf("插件未注册: %s", id)
		}
		nextCfg.Plugins = append(nextCfg.Plugins, config.PluginConfig{
			ID:      id,
			Kind:    configuredPluginKind("", manifest, hasManifest),
			Enabled: true,
			Config:  map[string]any{},
		})
	}
	nextCfg.Plugins = s.normalizePluginConfigs(nextCfg.Plugins, currentCfg.Plugins)
	if enabled {
		if manifest, ok := s.host.Manifest(id); ok {
			cfgFound, exists := findConfiguredPlugin(nextCfg.Plugins, id)
			if !exists {
				return fmt.Errorf("插件配置不存在: %s", id)
			}
			if err := validatePluginConfigWithSchema(manifest, cfgFound); err != nil {
				return err
			}
		}
	}

	if _, err := config.Save(s.configPath, nextCfg); err != nil {
		return err
	}

	if running {
		if err := s.host.Apply(ctx, nextCfg.Plugins); err != nil {
			return err
		}
	} else {
		s.host.SetConfigured(nextCfg.Plugins)
	}

	s.mu.Lock()
	s.cfg.Plugins = nextCfg.Plugins
	s.mu.Unlock()
	return nil
}

func (s *Service) ensurePluginConfigured(ctx context.Context, id string, enabled bool) error {
	if err := s.syncExternalPlugins(); err != nil {
		return err
	}
	s.mu.RLock()
	currentCfg := s.cfg
	running := s.state == StateRunning
	s.mu.RUnlock()

	nextCfg, err := config.Clone(currentCfg)
	if err != nil {
		return err
	}

	manifest, hasManifest := s.host.Manifest(id)

	for i := range nextCfg.Plugins {
		if nextCfg.Plugins[i].ID != id {
			continue
		}
		if strings.TrimSpace(nextCfg.Plugins[i].Kind) == "" {
			nextCfg.Plugins[i].Kind = configuredPluginKind(nextCfg.Plugins[i].Kind, manifest, hasManifest)
		}
		if nextCfg.Plugins[i].Config == nil {
			nextCfg.Plugins[i].Config = map[string]any{}
		}
		nextCfg.Plugins[i].Enabled = enabled
		if _, err := config.Save(s.configPath, nextCfg); err != nil {
			return err
		}
		if running {
			if err := s.host.Apply(ctx, nextCfg.Plugins); err != nil {
				return err
			}
		} else {
			s.host.SetConfigured(nextCfg.Plugins)
		}
		s.mu.Lock()
		s.cfg.Plugins = nextCfg.Plugins
		s.mu.Unlock()
		return nil
	}

	if !hasManifest {
		return fmt.Errorf("插件未注册: %s", id)
	}
	if manifest.Builtin {
		return fmt.Errorf("内置插件固定随系统提供，不支持安装: %s", id)
	}
	nextCfg.Plugins = append(nextCfg.Plugins, config.PluginConfig{
		ID:      id,
		Kind:    configuredPluginKind("", manifest, true),
		Enabled: enabled,
		Config:  map[string]any{},
	})
	nextCfg.Plugins = s.normalizePluginConfigs(nextCfg.Plugins, currentCfg.Plugins)

	if _, err := config.Save(s.configPath, nextCfg); err != nil {
		return err
	}
	if running {
		if err := s.host.Apply(ctx, nextCfg.Plugins); err != nil {
			return err
		}
	} else {
		s.host.SetConfigured(nextCfg.Plugins)
	}
	s.mu.Lock()
	s.cfg.Plugins = nextCfg.Plugins
	s.mu.Unlock()
	return nil
}

func pluginDebugRequiredString(payload map[string]any, key string) (string, error) {
	value, err := pluginDebugRequiredValue(payload, key)
	if err != nil {
		return "", err
	}
	text, err := pluginDebugString(value)
	if err != nil {
		return "", fmt.Errorf("payload.%s %w", key, err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("payload.%s 不能为空", key)
	}
	return text, nil
}

func pluginDebugRequiredValue(payload map[string]any, key string) (any, error) {
	if payload == nil {
		return nil, fmt.Errorf("payload.%s 不能为空", key)
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return nil, fmt.Errorf("payload.%s 不能为空", key)
	}
	return value, nil
}

func pluginDebugString(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case json.Number:
		return v.String(), nil
	case float64:
		if math.Trunc(v) == v {
			return strconv.FormatInt(int64(v), 10), nil
		}
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case float32:
		fv := float64(v)
		if math.Trunc(fv) == fv {
			return strconv.FormatInt(int64(fv), 10), nil
		}
		return strconv.FormatFloat(fv, 'f', -1, 32), nil
	case int:
		return strconv.Itoa(v), nil
	case int8:
		return strconv.FormatInt(int64(v), 10), nil
	case int16:
		return strconv.FormatInt(int64(v), 10), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	default:
		return "", fmt.Errorf("必须是字符串或数字")
	}
}

func decodePluginDebugValue(value any, out any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, out)
}

func (s *Service) syncExternalPlugins() error {
	descriptors, err := externalexec.Discover(s.externalRoot)
	if err != nil {
		return err
	}
	registrations := make([]host.Registration, 0, len(descriptors))
	for _, item := range descriptors {
		desc := item
		if strings.EqualFold(desc.Manifest.Runtime, externalexec.RuntimePython) {
			if err := s.syncPythonPluginCommonRuntime(desc.WorkDir); err != nil {
				return fmt.Errorf("修复 Python 插件运行时目录失败 %s: %w", desc.Manifest.ID, err)
			}
			usesEnv, err := requirementsFileHasEntries(filepath.Join(desc.WorkDir, pythonRequirementsFile))
			if err != nil {
				return fmt.Errorf("检查插件依赖文件失败 %s: %w", desc.Manifest.ID, err)
			}
			if usesEnv {
				desc.Manifest.PythonEnv = s.pythonPluginEnvDir(desc.Manifest.ID)
			}
		}
		registrations = append(registrations, host.Registration{
			Manifest: desc.Manifest,
			Factory: func() sdk.Plugin {
				return externalexec.New(desc)
			},
		})
	}
	s.host.SyncDynamic(registrations)
	return nil
}

func configuredPluginKind(current string, manifest sdk.Manifest, hasManifest bool) string {
	if kind := strings.TrimSpace(current); kind != "" {
		return kind
	}
	if hasManifest {
		if kind := strings.TrimSpace(manifest.Kind); kind != "" {
			return kind
		}
		if manifest.Builtin {
			return "builtin"
		}
		return externalexec.KindExternalExec
	}
	return "builtin"
}

func (s *Service) normalizePluginConfigs(nextPlugins []config.PluginConfig, previousPlugins []config.PluginConfig) []config.PluginConfig {
	_ = previousPlugins

	out := make([]config.PluginConfig, 0, len(nextPlugins))
	seen := make(map[string]struct{}, len(nextPlugins))
	for _, plugin := range nextPlugins {
		normalized := clonePluginConfig(plugin)
		if normalized.ID == "" {
			continue
		}
		if _, exists := seen[normalized.ID]; exists {
			continue
		}

		manifest, hasManifest := s.host.Manifest(normalized.ID)
		kind := configuredPluginKind(normalized.Kind, manifest, hasManifest)
		if kind == "builtin" {
			if !hasManifest || !manifest.Builtin {
				continue
			}
			normalized.Kind = "builtin"
		} else if normalized.Kind == "" {
			normalized.Kind = kind
		}
		if hasManifest {
			normalized.Config = mergePluginConfigWithSchemaDefaults(normalized.Config, optionalPluginSchema(manifest))
		} else if normalized.Config == nil {
			normalized.Config = map[string]any{}
		}
		out = append(out, normalized)
		seen[normalized.ID] = struct{}{}
	}
	return out
}

func (s *Service) registeredBuiltinManifests() []sdk.Manifest {
	items := s.host.Snapshots()
	out := make([]sdk.Manifest, 0, len(items))
	for _, item := range items {
		if !item.Builtin {
			continue
		}
		manifest, ok := s.host.Manifest(item.ID)
		if !ok || !manifest.Builtin {
			continue
		}
		out = append(out, manifest)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func clonePluginConfigs(items []config.PluginConfig) []config.PluginConfig {
	out := make([]config.PluginConfig, 0, len(items))
	for _, item := range items {
		out = append(out, clonePluginConfig(item))
	}
	return out
}

func clonePluginConfig(item config.PluginConfig) config.PluginConfig {
	item.Config = cloneConfigMap(item.Config)
	return item
}

func cloneConnectionConfigs(items []config.ConnectionConfig) []config.ConnectionConfig {
	out := make([]config.ConnectionConfig, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

func cloneConfigMap(raw map[string]any) map[string]any {
	if raw == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		out[k] = v
	}
	return out
}

func loadPluginSchema(manifest sdk.Manifest) (map[string]any, string, string) {
	schemaPath := strings.TrimSpace(manifest.ConfigSchema)
	if schemaPath == "" {
		return nil, "", ""
	}

	payload, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, schemaPath, err.Error()
	}

	var schema map[string]any
	if err := json.Unmarshal(payload, &schema); err != nil {
		return nil, schemaPath, err.Error()
	}
	return schema, schemaPath, ""
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *Service) cloneAuditState() ([]AuditLogEntry, int) {
	s.auditMu.RLock()
	defer s.auditMu.RUnlock()
	out := make([]AuditLogEntry, len(s.auditLogs))
	copy(out, s.auditLogs)
	return out, s.auditLimit
}

func (s *Service) replaceRuntimeStateForRestart(next *Service, auditLogs []AuditLogEntry, auditLimit int) {
	s.mu.Lock()
	s.cfg = next.cfg
	s.configPath = next.configPath
	s.configSaveTo = next.configSaveTo
	s.logger = next.logger
	s.aiService = next.aiService
	s.mediaService = next.mediaService
	s.messenger = next.messenger
	s.host = next.host
	s.state = next.state
	s.startedAt = next.startedAt
	s.runCtx = next.runCtx
	s.cancel = next.cancel
	s.wg = sync.WaitGroup{}
	s.externalRoot = next.externalRoot
	if s.relationTasks == nil {
		s.relationTasks = make(map[string]*aiRelationAnalysisTask)
	}
	s.connections = next.connections
	s.actionClients = next.actionClients
	s.ingresses = next.ingresses
	s.mu.Unlock()

	s.auditMu.Lock()
	s.auditLimit = auditLimit
	s.auditLogs = auditLogs
	s.auditMu.Unlock()
}

func hasNonPluginChanges(currentCfg, nextCfg *config.Config) bool {
	if currentCfg == nil || nextCfg == nil {
		return currentCfg != nextCfg
	}
	left := *currentCfg
	right := *nextCfg
	left.Plugins = nil
	right.Plugins = nil
	return !reflect.DeepEqual(left, right)
}

func (s *Service) probeConnection(ctx context.Context, id string) (*adapter.BotStatus, *adapter.LoginInfo, error) {
	client, err := s.resolveClient(id)
	if err != nil {
		return nil, nil, err
	}
	if readiness, ok := client.(actionClientReadiness); ok && !readiness.Ready() {
		return nil, nil, &connectionProbeDeferredError{Reason: readiness.ReadinessReason()}
	}

	status, err := client.GetStatus(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("获取状态失败: %w", err)
	}
	login, err := client.GetLoginInfo(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("获取登录信息失败: %w", err)
	}
	return status, login, nil
}

func (s *Service) resolveClient(id string) (adapter.ActionClient, error) {
	if client, ok := s.actionClients[id]; ok {
		return client, nil
	}
	return nil, fmt.Errorf("连接不存在: %s", id)
}

func (s *Service) buildIngress(conn config.ConnectionConfig) (adapter.EventIngress, error) {
	normalized := normalizeConnectionConfig(conn)
	switch normalized.Ingress.Type {
	case "ws_server":
		return onebotingress.NewWSServer(normalized.ID, normalized.Ingress.Listen, normalized.Ingress.Path, normalized.Action.AccessToken, s.logger), nil
	case "http_callback":
		return onebotingress.NewHTTPCallback(normalized.ID, normalized.Ingress.Listen, normalized.Ingress.Path, s.logger), nil
	case "ws_reverse":
		return onebotingress.NewWSReverse(
			normalized.ID,
			normalized.Ingress.URL,
			normalized.Action.AccessToken,
			time.Duration(normalized.Ingress.RetryIntervalMS)*time.Millisecond,
			s.logger,
		), nil
	default:
		return nil, fmt.Errorf("未知 ingress 类型: %s", normalized.Ingress.Type)
	}
}

func (s *Service) buildActionClient(conn config.ConnectionConfig, ingress adapter.EventIngress) (adapter.ActionClient, error) {
	normalized := normalizeConnectionConfig(conn)
	timeout := time.Duration(normalized.Action.TimeoutMS) * time.Millisecond

	switch normalized.Action.Type {
	case config.ActionTypeOneBotWS:
		builder, ok := ingress.(connectionActionClientBuilder)
		if !ok || ingress == nil {
			return nil, fmt.Errorf("连接 %s 的 %s 不支持 WebSocket 动作通道", normalized.ID, normalized.Ingress.Type)
		}
		return builder.BuildActionClient(timeout), nil
	case config.ActionTypeNapCatHTTP:
		return httpclient.New(
			normalized.ID,
			normalized.Action.BaseURL,
			normalized.Action.AccessToken,
			timeout,
			s.logger,
		), nil
	default:
		return nil, fmt.Errorf("未知动作类型: %s", normalized.Action.Type)
	}
}

func (s *Service) findConnectionConfig(id string) (config.ConnectionConfig, bool) {
	for _, conn := range s.cfg.Connections {
		if conn.ID == id {
			return conn, true
		}
	}
	return config.ConnectionConfig{}, false
}

func (s *Service) rebuildConnections(ctx context.Context, nextConnections []config.ConnectionConfig) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.RLock()
	running := s.state == StateRunning && s.runCtx != nil
	runCtx := s.runCtx
	oldIngresses := make([]adapter.EventIngress, 0, len(s.ingresses))
	for _, ingress := range s.ingresses {
		oldIngresses = append(oldIngresses, ingress)
	}
	s.mu.RUnlock()

	var errs []error
	for _, ingress := range oldIngresses {
		if ingress == nil {
			continue
		}
		if err := ingress.Stop(ctx); err != nil {
			errs = append(errs, err)
			s.logger.Error("停止旧连接 ingress 失败", "connection", ingress.ID(), "error", err)
		}
	}

	snapshots := make(map[string]adapter.ConnectionSnapshot, len(nextConnections))
	actionClients := make(map[string]adapter.ActionClient, len(nextConnections))
	ingresses := make(map[string]adapter.EventIngress, len(nextConnections))
	clientList := make([]adapter.ActionClient, 0, len(nextConnections))
	defaultID := ""

	for _, conn := range nextConnections {
		normalized := normalizeConnectionConfig(conn)
		snapshots[normalized.ID] = connectionSnapshotFromConfig(normalized)

		ingress, err := s.buildIngress(normalized)
		if err != nil {
			snapshot := snapshots[normalized.ID]
			snapshot.LastError = err.Error()
			snapshots[normalized.ID] = snapshot
			errs = append(errs, err)
		}
		if ingress != nil {
			ingresses[normalized.ID] = ingress
		}

		client, err := s.buildActionClient(normalized, ingress)
		if err != nil {
			snapshot := snapshots[normalized.ID]
			if snapshot.LastError != "" {
				snapshot.LastError += "; "
			}
			snapshot.LastError += err.Error()
			snapshots[normalized.ID] = snapshot
			errs = append(errs, err)
			continue
		}
		actionClients[normalized.ID] = client
		clientList = append(clientList, client)
		if defaultID == "" {
			defaultID = normalized.ID
		}
	}

	s.messenger.Replace(clientList, defaultID)

	s.mu.Lock()
	s.connections = snapshots
	s.actionClients = actionClients
	s.ingresses = ingresses
	s.mu.Unlock()

	if !running {
		return errors.Join(errs...)
	}

	for _, conn := range nextConnections {
		normalized := normalizeConnectionConfig(conn)
		if !normalized.Enabled {
			continue
		}

		if ingress, ok := s.ingresses[normalized.ID]; ok {
			if err := ingress.Start(runCtx); err != nil {
				s.mu.Lock()
				snapshot := s.connections[normalized.ID]
				snapshot.IngressState = adapter.ConnectionFailed
				if snapshot.LastError != "" {
					snapshot.LastError += "; "
				}
				snapshot.LastError += "启动 ingress 失败: " + err.Error()
				snapshot.UpdatedAt = time.Now()
				s.connections[normalized.ID] = snapshot
				s.mu.Unlock()
				errs = append(errs, err)
			} else {
				s.mu.Lock()
				snapshot := s.connections[normalized.ID]
				snapshot.IngressState = adapter.ConnectionRunning
				snapshot.UpdatedAt = time.Now()
				s.connections[normalized.ID] = snapshot
				s.mu.Unlock()

				s.wg.Add(1)
				go s.consumeIngress(ingress)
			}
		}

		status, login, err := s.probeConnection(ctx, normalized.ID)
		s.mu.Lock()
		snapshot := s.connections[normalized.ID]
		snapshot.Enabled = true
		applyConnectionProbeResult(&snapshot, status, login, err)
		s.connections[normalized.ID] = snapshot
		s.mu.Unlock()
	}
	return errors.Join(errs...)
}

func applyConnectionProbeResult(snapshot *adapter.ConnectionSnapshot, status *adapter.BotStatus, login *adapter.LoginInfo, err error) {
	snapshot.UpdatedAt = time.Now()
	if err == nil {
		snapshot.State = adapter.ConnectionRunning
		snapshot.Online = status.Online
		snapshot.Good = status.Good
		snapshot.SelfID = login.UserID
		snapshot.SelfNickname = login.Nickname
		if snapshot.IngressState != adapter.ConnectionFailed {
			snapshot.LastError = ""
		}
		return
	}

	snapshot.Online = false
	snapshot.Good = false
	snapshot.SelfID = ""
	snapshot.SelfNickname = ""

	var deferredErr *connectionProbeDeferredError
	if errors.As(err, &deferredErr) {
		if snapshot.IngressState != "" {
			snapshot.State = snapshot.IngressState
		} else {
			snapshot.State = adapter.ConnectionRunning
		}
		return
	}

	snapshot.State = adapter.ConnectionFailed
	appendConnectionError(snapshot, err.Error())
}

func appendConnectionError(snapshot *adapter.ConnectionSnapshot, msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	if strings.TrimSpace(snapshot.LastError) == "" {
		snapshot.LastError = msg
		return
	}
	if strings.Contains(snapshot.LastError, msg) {
		return
	}
	snapshot.LastError += "; " + msg
}

func normalizeConnectionConfig(conn config.ConnectionConfig) config.ConnectionConfig {
	return config.NormalizeConnectionConfig(conn)
}

func normalizeConnectionConfigs(items []config.ConnectionConfig) []config.ConnectionConfig {
	return config.NormalizeConnectionConfigs(items)
}

func connectionSnapshotFromConfig(conn config.ConnectionConfig) adapter.ConnectionSnapshot {
	normalized := normalizeConnectionConfig(conn)
	return adapter.ConnectionSnapshot{
		ID:           normalized.ID,
		Platform:     normalized.Platform,
		Enabled:      normalized.Enabled,
		IngressType:  normalized.Ingress.Type,
		ActionType:   normalized.Action.Type,
		State:        adapter.ConnectionStopped,
		IngressState: adapter.ConnectionStopped,
		UpdatedAt:    time.Now(),
	}
}

func (s *Service) consumeIngress(ingress adapter.EventIngress) {
	defer s.wg.Done()

	for {
		select {
		case <-s.runCtx.Done():
			return
		case evt, ok := <-ingress.Events():
			if !ok {
				return
			}
			if s.mediaService != nil {
				s.mediaService.Enqueue(evt, s.actionClients[evt.ConnectionID])
			}
			if s.aiService != nil {
				s.enqueueAIEvent(evt)
			}
			s.host.Dispatch(s.runCtx, evt)
		}
	}
}

func (s *Service) enqueueAIEvent(evt event.Event) {
	s.mu.RLock()
	ch := s.aiEventQueue
	running := s.state == StateRunning
	s.mu.RUnlock()
	if ch == nil || !running {
		return
	}

	select {
	case ch <- evt:
	default:
		s.logger.Warn("AI event queue is full, dropping event", "connection_id", evt.ConnectionID, "event_kind", evt.Kind, "chat_type", evt.ChatType, "group_id", evt.GroupID, "user_id", evt.UserID)
	}
}

func (s *Service) consumeAIEvents(events <-chan event.Event) {
	defer s.wg.Done()

	for {
		select {
		case <-s.runCtx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			if s.aiService != nil {
				s.aiService.HandleEvent(s.runCtx, evt)
			}
		}
	}
}
