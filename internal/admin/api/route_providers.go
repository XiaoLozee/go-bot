package api

import (
	"context"
	"strings"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	"github.com/XiaoLozee/go-bot/internal/ai"
	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/plugin/host"
	"github.com/XiaoLozee/go-bot/internal/runtime"
)

type adminAuditProvider interface {
	AdminAuthStatus() runtime.AuthStatus
	RecordAuditLog(entry runtime.AuditLogEntry)
}

type systemRouteProvider interface {
	Snapshot() runtime.Snapshot
	Metadata() runtime.Metadata
}

type aiRouteProvider interface {
	adminAuditProvider
	AIView() runtime.AIView
	ListAIMessageLogs(ctx context.Context, query ai.MessageLogQuery) (runtime.AIMessageListView, error)
	ListAIMessageSuggestions(ctx context.Context, query ai.MessageSuggestionQuery) (ai.MessageSearchSuggestions, error)
	GetAIMessageDetail(ctx context.Context, messageID string) (runtime.AIMessageDetailView, error)
	GetAIForwardMessage(ctx context.Context, connectionID, forwardID string) (runtime.AIForwardMessageView, error)
	SendAIMessage(ctx context.Context, req runtime.AIMessageSendRequest) (runtime.AIMessageSendResult, error)
	DiscoverAIProviderModels(ctx context.Context, provider config.AIProviderConfig) (runtime.AIProviderModelsResult, error)
	SyncAIRecentMessages(ctx context.Context, req runtime.AIRecentMessagesSyncRequest) (runtime.AIRecentMessagesSyncResult, error)
	SyncAllAIRecentMessages(ctx context.Context, req runtime.AIRecentMessagesBulkSyncRequest) (runtime.AIRecentMessagesBulkSyncResult, error)
	ResolveAIMessageImagePreview(ctx context.Context, messageID string, segmentIndex int) (ai.MessageImagePreview, error)
	ListAIInstalledSkills(ctx context.Context) ([]runtime.AIInstalledSkillView, error)
	GetAIInstalledSkill(ctx context.Context, id string) (runtime.AIInstalledSkillDetailView, error)
	InstallAIInstalledSkillPackage(ctx context.Context, fileName string, payload []byte, overwrite bool) (runtime.AISkillInstallResult, error)
	InstallAIInstalledSkillFromURL(ctx context.Context, sourceURL string, overwrite bool) (runtime.AISkillInstallResult, error)
	SetAIInstalledSkillEnabled(ctx context.Context, id string, enabled bool) (runtime.AISkillActionResult, error)
	UninstallAIInstalledSkill(ctx context.Context, id string) (runtime.AISkillActionResult, error)
	RunAIReflection(ctx context.Context) (runtime.AIMemoryActionResult, error)
	AnalyzeAIRelations(ctx context.Context, req runtime.AIRelationAnalysisRequest) (runtime.AIRelationAnalysisResult, error)
	StartAIRelationAnalysis(ctx context.Context, req runtime.AIRelationAnalysisRequest) (runtime.AIRelationAnalysisTaskView, error)
	GetAIRelationAnalysisTask(ctx context.Context, taskID string) (runtime.AIRelationAnalysisTaskView, error)
	PromoteAICandidateMemory(ctx context.Context, id string) (runtime.AIMemoryActionResult, error)
	DeleteAICandidateMemory(ctx context.Context, id string) (runtime.AIMemoryActionResult, error)
	DeleteAILongTermMemory(ctx context.Context, id string) (runtime.AIMemoryActionResult, error)
	SaveAIConfig(ctx context.Context, aiCfg config.AIConfig) (runtime.AISaveResult, error)
}

type auditRouteProvider interface {
	AuditLogs(limit int) []runtime.AuditLogEntry
	ConfigView() map[string]any
}

type configRouteProvider interface {
	adminAuditProvider
	ConfigView() map[string]any
	SaveConfig(ctx context.Context, cfg *config.Config) (runtime.ConfigSaveResult, error)
	HotRestart(ctx context.Context) (runtime.RuntimeRestartResult, error)
}

type webUIRouteProvider interface {
	adminAuditProvider
	systemRouteProvider
	WebUIBootstrap() runtime.WebUIBootstrap
	SaveWebUITheme(ctx context.Context, theme string) (runtime.ConfigSaveResult, error)
}

type connectionRouteProvider interface {
	adminAuditProvider
	ConnectionSnapshots() []adapter.ConnectionSnapshot
	ConnectionDetail(id string) (runtime.ConnectionDetail, bool)
	RefreshConnection(ctx context.Context, id string) (runtime.ConnectionDetail, error)
	SaveConnectionConfig(ctx context.Context, conn config.ConnectionConfig) (runtime.ConnectionSaveResult, error)
	SetConnectionEnabled(ctx context.Context, id string, enabled bool) (runtime.ConnectionSaveResult, error)
	DeleteConnection(ctx context.Context, id string) (runtime.ConnectionSaveResult, error)
}

type pluginRouteProvider interface {
	adminAuditProvider
	PluginSnapshots() []host.Snapshot
	PluginDetail(id string) (runtime.PluginDetail, bool)
	InstallPluginPackage(ctx context.Context, fileName string, payload []byte, overwrite bool) (runtime.PluginInstallResult, error)
	InstallPlugin(ctx context.Context, id string) error
	StartPlugin(ctx context.Context, id string) error
	StopPlugin(ctx context.Context, id string) error
	ReloadPlugin(ctx context.Context, id string) error
	RecoverPlugin(ctx context.Context, id string) error
	UninstallPlugin(ctx context.Context, id string) error
	SavePluginConfig(ctx context.Context, id string, enabled bool, pluginConfig map[string]any) (runtime.PluginConfigSaveResult, error)
	DebugFrameworkPluginAPI(ctx context.Context, req runtime.PluginAPIDebugRequest) (runtime.PluginAPIDebugResult, error)
	DebugPluginAPI(ctx context.Context, id string, req runtime.PluginAPIDebugRequest) (runtime.PluginAPIDebugResult, error)
}

func splitRouteParts(path, prefix string) []string {
	tail := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if tail == "" {
		return nil
	}
	return strings.Split(tail, "/")
}
