package runtime

import (
	"context"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	"github.com/XiaoLozee/go-bot/internal/ai"
	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/plugin/host"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

type State string

const (
	StateStopped State = "stopped"
	StateRunning State = "running"
)

type Snapshot struct {
	State       State     `json:"state"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	AppName     string    `json:"app_name"`
	Environment string    `json:"environment"`
	Connections int       `json:"connections"`
	Plugins     int       `json:"plugins"`
}

type Metadata struct {
	AppName      string          `json:"app_name"`
	Environment  string          `json:"environment"`
	OwnerQQ      string          `json:"owner_qq,omitempty"`
	AdminEnabled bool            `json:"admin_enabled"`
	WebUIEnabled bool            `json:"webui_enabled"`
	WebUIBaseURL string          `json:"webui_base_path,omitempty"`
	WebUITheme   string          `json:"webui_theme,omitempty"`
	Capabilities map[string]bool `json:"capabilities"`
}

type RuntimeRestartResult struct {
	Accepted    bool      `json:"accepted"`
	Restarted   bool      `json:"restarted"`
	State       State     `json:"state"`
	RestartedAt time.Time `json:"restarted_at,omitempty"`
	Message     string    `json:"message,omitempty"`
}

type ConnectionDetail struct {
	Snapshot adapter.ConnectionSnapshot `json:"snapshot"`
	Config   map[string]any             `json:"config"`
}

type ConnectionSaveResult struct {
	Accepted        bool             `json:"accepted"`
	Persisted       bool             `json:"persisted"`
	RestartRequired bool             `json:"restart_required"`
	HotApplied      bool             `json:"hot_applied,omitempty"`
	HotApplyError   string           `json:"hot_apply_error,omitempty"`
	ConnectionID    string           `json:"connection_id"`
	Path            string           `json:"path,omitempty"`
	BackupPath      string           `json:"backup_path,omitempty"`
	SavedAt         time.Time        `json:"saved_at,omitempty"`
	Detail          ConnectionDetail `json:"detail,omitempty"`
	Message         string           `json:"message,omitempty"`
}

type PluginDetail struct {
	Snapshot          host.Snapshot     `json:"snapshot"`
	Config            map[string]any    `json:"config"`
	Runtime           sdk.RuntimeStatus `json:"runtime,omitempty"`
	ConfigSchema      map[string]any    `json:"config_schema,omitempty"`
	ConfigSchemaPath  string            `json:"config_schema_path,omitempty"`
	ConfigSchemaError string            `json:"config_schema_error,omitempty"`
}

type WebUIBootstrap struct {
	GeneratedAt time.Time                    `json:"generated_at"`
	Meta        Metadata                     `json:"meta"`
	Runtime     Snapshot                     `json:"runtime"`
	AI          AIView                       `json:"ai"`
	Connections []adapter.ConnectionSnapshot `json:"connections"`
	Plugins     []host.Snapshot              `json:"plugins"`
	Config      map[string]any               `json:"config"`
}

type AIView struct {
	Snapshot        ai.Snapshot            `json:"snapshot"`
	Config          map[string]any         `json:"config"`
	Debug           ai.DebugView           `json:"debug"`
	Skills          []ai.SkillView         `json:"skills,omitempty"`
	InstalledSkills []AIInstalledSkillView `json:"installed_skills,omitempty"`
}

type AIMessageListView struct {
	Items []ai.MessageLog    `json:"items"`
	Query ai.MessageLogQuery `json:"query"`
}

type AIMessageDetailView struct {
	Item ai.MessageDetail `json:"item"`
}

type AIForwardMessageView struct {
	ConnectionID string                       `json:"connection_id"`
	ForwardID    string                       `json:"forward_id"`
	Nodes        []adapter.ForwardMessageNode `json:"nodes"`
	FetchedAt    time.Time                    `json:"fetched_at"`
	Cached       bool                         `json:"cached"`
}

type AIMessageSendRequest struct {
	ConnectionID string `json:"connection_id"`
	ChatType     string `json:"chat_type"`
	GroupID      string `json:"group_id,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Text         string `json:"text"`
}

type AIProviderModel struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by,omitempty"`
}

type AIProviderModelsResult struct {
	Accepted  bool              `json:"accepted"`
	Models    []AIProviderModel `json:"models"`
	FetchedAt time.Time         `json:"fetched_at"`
	Message   string            `json:"message,omitempty"`
}

type AIRecentMessagesSyncRequest struct {
	ConnectionID string `json:"connection_id,omitempty"`
	ChatType     string `json:"chat_type"`
	GroupID      string `json:"group_id,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Count        int    `json:"count,omitempty"`
}

type AIRecentMessagesSyncResult struct {
	Accepted     bool      `json:"accepted"`
	ConnectionID string    `json:"connection_id"`
	ChatType     string    `json:"chat_type"`
	GroupID      string    `json:"group_id,omitempty"`
	UserID       string    `json:"user_id,omitempty"`
	Requested    int       `json:"requested"`
	Fetched      int       `json:"fetched"`
	Synced       int       `json:"synced"`
	SyncedAt     time.Time `json:"synced_at"`
	Message      string    `json:"message,omitempty"`
}

type AIRecentMessagesBulkSyncRequest struct {
	ConnectionID string `json:"connection_id,omitempty"`
	ChatType     string `json:"chat_type"`
	Count        int    `json:"count,omitempty"`
}

type AIRecentMessagesBulkSyncResult struct {
	Accepted     bool      `json:"accepted"`
	ConnectionID string    `json:"connection_id"`
	ChatType     string    `json:"chat_type"`
	Targets      int       `json:"targets"`
	Requested    int       `json:"requested"`
	Fetched      int       `json:"fetched"`
	Synced       int       `json:"synced"`
	Failed       int       `json:"failed"`
	SyncedAt     time.Time `json:"synced_at"`
	Message      string    `json:"message,omitempty"`
}

type AIMessageSendResult struct {
	Accepted     bool      `json:"accepted"`
	ConnectionID string    `json:"connection_id"`
	ChatType     string    `json:"chat_type"`
	GroupID      string    `json:"group_id,omitempty"`
	UserID       string    `json:"user_id,omitempty"`
	SentAt       time.Time `json:"sent_at"`
	Message      string    `json:"message,omitempty"`
}

type ConfigSaveResult struct {
	Accepted          bool           `json:"accepted"`
	Persisted         bool           `json:"persisted"`
	RestartRequired   bool           `json:"restart_required"`
	PluginChanged     bool           `json:"plugin_changed,omitempty"`
	NonPluginChanged  bool           `json:"non_plugin_changed,omitempty"`
	HotApplyAttempted bool           `json:"hot_apply_attempted,omitempty"`
	HotApplied        bool           `json:"hot_applied,omitempty"`
	HotApplyError     string         `json:"hot_apply_error,omitempty"`
	SourcePath        string         `json:"source_path,omitempty"`
	Path              string         `json:"path,omitempty"`
	BackupPath        string         `json:"backup_path,omitempty"`
	SavedAt           time.Time      `json:"saved_at,omitempty"`
	NormalizedConfig  map[string]any `json:"normalized_config,omitempty"`
	Message           string         `json:"message,omitempty"`
}

type AISaveResult struct {
	Accepted        bool      `json:"accepted"`
	Persisted       bool      `json:"persisted"`
	RestartRequired bool      `json:"restart_required"`
	HotApplied      bool      `json:"hot_applied,omitempty"`
	HotApplyError   string    `json:"hot_apply_error,omitempty"`
	Path            string    `json:"path,omitempty"`
	BackupPath      string    `json:"backup_path,omitempty"`
	SavedAt         time.Time `json:"saved_at,omitempty"`
	View            AIView    `json:"view"`
	Message         string    `json:"message,omitempty"`
}

type AIMemoryActionResult struct {
	Accepted bool   `json:"accepted"`
	Action   string `json:"action"`
	Target   string `json:"target"`
	ID       string `json:"id"`
	View     AIView `json:"view"`
	Message  string `json:"message,omitempty"`
}

type AIRelationAnalysisRequest struct {
	GroupID string `json:"group_id,omitempty"`
	Force   bool   `json:"force,omitempty"`
}

type AIRelationAnalysisTaskView struct {
	Accepted   bool                      `json:"accepted"`
	TaskID     string                    `json:"task_id"`
	Status     string                    `json:"status"`
	GroupID    string                    `json:"group_id,omitempty"`
	Force      bool                      `json:"force"`
	CreatedAt  time.Time                 `json:"created_at"`
	StartedAt  *time.Time                `json:"started_at,omitempty"`
	FinishedAt *time.Time                `json:"finished_at,omitempty"`
	Result     *AIRelationAnalysisResult `json:"result,omitempty"`
	Error      string                    `json:"error,omitempty"`
	Message    string                    `json:"message,omitempty"`
}

type AIRelationAnalysisResult struct {
	Accepted    bool      `json:"accepted"`
	GroupID     string    `json:"group_id,omitempty"`
	Markdown    string    `json:"markdown"`
	GeneratedAt time.Time `json:"generated_at"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	UserCount   int       `json:"user_count"`
	EdgeCount   int       `json:"edge_count"`
	MemoryCount int       `json:"memory_count"`
	InputHash   string    `json:"input_hash,omitempty"`
	CacheHit    bool      `json:"cache_hit"`
	Message     string    `json:"message,omitempty"`
}

type AIInstalledSkillView struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Description        string    `json:"description,omitempty"`
	SourceType         string    `json:"source_type"`
	SourceLabel        string    `json:"source_label,omitempty"`
	SourceURL          string    `json:"source_url,omitempty"`
	Provider           string    `json:"provider,omitempty"`
	Enabled            bool      `json:"enabled"`
	InstalledAt        time.Time `json:"installed_at,omitempty"`
	UpdatedAt          time.Time `json:"updated_at,omitempty"`
	EntryPath          string    `json:"entry_path,omitempty"`
	Format             string    `json:"format,omitempty"`
	InstructionPreview string    `json:"instruction_preview,omitempty"`
	ContentLength      int       `json:"content_length,omitempty"`
}

type AIInstalledSkillDetailView struct {
	AIInstalledSkillView
	Content string `json:"content,omitempty"`
}

type AISkillActionResult struct {
	Accepted bool                  `json:"accepted"`
	Action   string                `json:"action"`
	ID       string                `json:"id"`
	Enabled  bool                  `json:"enabled,omitempty"`
	Skill    *AIInstalledSkillView `json:"skill,omitempty"`
	View     AIView                `json:"view"`
	Message  string                `json:"message,omitempty"`
}

type AISkillInstallResult struct {
	Accepted    bool                       `json:"accepted"`
	Replaced    bool                       `json:"replaced,omitempty"`
	InstalledTo string                     `json:"installed_to,omitempty"`
	BackupPath  string                     `json:"backup_path,omitempty"`
	Skill       AIInstalledSkillDetailView `json:"skill"`
	View        AIView                     `json:"view"`
	Message     string                     `json:"message,omitempty"`
}

type PluginInstallResult struct {
	PluginID              string `json:"plugin_id"`
	Kind                  string `json:"kind"`
	Format                string `json:"format,omitempty"`
	InstalledTo           string `json:"installed_to"`
	ManifestPath          string `json:"manifest_path,omitempty"`
	BackupPath            string `json:"backup_path,omitempty"`
	DependencyEnvPath     string `json:"dependency_env_path,omitempty"`
	DependenciesInstalled bool   `json:"dependencies_installed,omitempty"`
	Replaced              bool   `json:"replaced"`
	Reloaded              bool   `json:"reloaded"`
	Message               string `json:"message,omitempty"`
}

type PluginConfigSaveResult struct {
	Accepted        bool         `json:"accepted"`
	Persisted       bool         `json:"persisted"`
	RestartRequired bool         `json:"restart_required"`
	HotApplied      bool         `json:"hot_applied,omitempty"`
	HotApplyError   string       `json:"hot_apply_error,omitempty"`
	PluginID        string       `json:"plugin_id"`
	Path            string       `json:"path,omitempty"`
	BackupPath      string       `json:"backup_path,omitempty"`
	SavedAt         time.Time    `json:"saved_at,omitempty"`
	Detail          PluginDetail `json:"detail,omitempty"`
	Message         string       `json:"message,omitempty"`
}

type PluginAPIDebugRequest struct {
	Method  string         `json:"method"`
	Payload map[string]any `json:"payload"`
}

type PluginAPIDebugResult struct {
	Accepted bool   `json:"accepted"`
	PluginID string `json:"plugin_id,omitempty"`
	Method   string `json:"method"`
	Result   any    `json:"result,omitempty"`
	Error    string `json:"error,omitempty"`
	Message  string `json:"message,omitempty"`
}

type AuthStatus struct {
	Enabled       bool `json:"enabled"`
	Configured    bool `json:"configured"`
	RequiresSetup bool `json:"requires_setup"`
}

type AuditLogEntry struct {
	At         time.Time `json:"at"`
	Category   string    `json:"category"`
	Action     string    `json:"action"`
	Target     string    `json:"target,omitempty"`
	Result     string    `json:"result"`
	Summary    string    `json:"summary"`
	Detail     string    `json:"detail,omitempty"`
	Username   string    `json:"username,omitempty"`
	RemoteAddr string    `json:"remote_addr,omitempty"`
	Method     string    `json:"method,omitempty"`
	Path       string    `json:"path,omitempty"`
}

type Provider interface {
	Snapshot() Snapshot
	Metadata() Metadata
	AIView() AIView
	ConfigView() map[string]any
	WebUIBootstrap() WebUIBootstrap
	ConnectionSnapshots() []adapter.ConnectionSnapshot
	ConnectionDetail(id string) (ConnectionDetail, bool)
	RefreshConnection(ctx context.Context, id string) (ConnectionDetail, error)
	SaveConnectionConfig(ctx context.Context, conn config.ConnectionConfig) (ConnectionSaveResult, error)
	SetConnectionEnabled(ctx context.Context, id string, enabled bool) (ConnectionSaveResult, error)
	DeleteConnection(ctx context.Context, id string) (ConnectionSaveResult, error)
	PluginSnapshots() []host.Snapshot
	PluginDetail(id string) (PluginDetail, bool)
	InstallPluginPackage(ctx context.Context, fileName string, payload []byte, overwrite bool) (PluginInstallResult, error)
	InstallPlugin(ctx context.Context, id string) error
	StartPlugin(ctx context.Context, id string) error
	StopPlugin(ctx context.Context, id string) error
	ReloadPlugin(ctx context.Context, id string) error
	RecoverPlugin(ctx context.Context, id string) error
	UninstallPlugin(ctx context.Context, id string) error
	SavePluginConfig(ctx context.Context, id string, enabled bool, pluginConfig map[string]any) (PluginConfigSaveResult, error)
	DebugFrameworkPluginAPI(ctx context.Context, req PluginAPIDebugRequest) (PluginAPIDebugResult, error)
	DebugPluginAPI(ctx context.Context, id string, req PluginAPIDebugRequest) (PluginAPIDebugResult, error)
	SaveAIConfig(ctx context.Context, aiCfg config.AIConfig) (AISaveResult, error)
	ListAIMessageLogs(ctx context.Context, query ai.MessageLogQuery) (AIMessageListView, error)
	ListAIMessageSuggestions(ctx context.Context, query ai.MessageSuggestionQuery) (ai.MessageSearchSuggestions, error)
	GetAIMessageDetail(ctx context.Context, messageID string) (AIMessageDetailView, error)
	GetAIForwardMessage(ctx context.Context, connectionID, forwardID string) (AIForwardMessageView, error)
	SendAIMessage(ctx context.Context, req AIMessageSendRequest) (AIMessageSendResult, error)
	DiscoverAIProviderModels(ctx context.Context, provider config.AIProviderConfig) (AIProviderModelsResult, error)
	SyncAIRecentMessages(ctx context.Context, req AIRecentMessagesSyncRequest) (AIRecentMessagesSyncResult, error)
	SyncAllAIRecentMessages(ctx context.Context, req AIRecentMessagesBulkSyncRequest) (AIRecentMessagesBulkSyncResult, error)
	ResolveAIMessageImagePreview(ctx context.Context, messageID string, segmentIndex int) (ai.MessageImagePreview, error)
	ListAIInstalledSkills(ctx context.Context) ([]AIInstalledSkillView, error)
	GetAIInstalledSkill(ctx context.Context, id string) (AIInstalledSkillDetailView, error)
	InstallAIInstalledSkillPackage(ctx context.Context, fileName string, payload []byte, overwrite bool) (AISkillInstallResult, error)
	InstallAIInstalledSkillFromURL(ctx context.Context, sourceURL string, overwrite bool) (AISkillInstallResult, error)
	SetAIInstalledSkillEnabled(ctx context.Context, id string, enabled bool) (AISkillActionResult, error)
	UninstallAIInstalledSkill(ctx context.Context, id string) (AISkillActionResult, error)
	RunAIReflection(ctx context.Context) (AIMemoryActionResult, error)
	AnalyzeAIRelations(ctx context.Context, req AIRelationAnalysisRequest) (AIRelationAnalysisResult, error)
	StartAIRelationAnalysis(ctx context.Context, req AIRelationAnalysisRequest) (AIRelationAnalysisTaskView, error)
	GetAIRelationAnalysisTask(ctx context.Context, taskID string) (AIRelationAnalysisTaskView, error)
	PromoteAICandidateMemory(ctx context.Context, id string) (AIMemoryActionResult, error)
	DeleteAICandidateMemory(ctx context.Context, id string) (AIMemoryActionResult, error)
	DeleteAILongTermMemory(ctx context.Context, id string) (AIMemoryActionResult, error)
	SaveConfig(ctx context.Context, cfg *config.Config) (ConfigSaveResult, error)
	HotRestart(ctx context.Context) (RuntimeRestartResult, error)
	SaveWebUITheme(ctx context.Context, theme string) (ConfigSaveResult, error)
	AdminAuthStatus() AuthStatus
	ConfigureAdminAuth(ctx context.Context, password string) (ConfigSaveResult, error)
	ChangeAdminPassword(ctx context.Context, currentPassword, newPassword string) (ConfigSaveResult, error)
	VerifyAdminPassword(password string) bool
	AuditLogs(limit int) []AuditLogEntry
	RecordAuditLog(entry AuditLogEntry)
}
