package config

type Config struct {
	App         AppConfig          `mapstructure:"app" yaml:"app" json:"app"`
	Server      ServerConfig       `mapstructure:"server" yaml:"server" json:"server"`
	Storage     StorageConfig      `mapstructure:"storage" yaml:"storage" json:"storage"`
	AI          AIConfig           `mapstructure:"ai" yaml:"ai" json:"ai"`
	Connections []ConnectionConfig `mapstructure:"connections" yaml:"connections" json:"connections"`
	Plugins     []PluginConfig     `mapstructure:"plugins" yaml:"plugins" json:"plugins"`
	Security    SecurityConfig     `mapstructure:"security" yaml:"security" json:"security"`
}

type AppConfig struct {
	Name     string `mapstructure:"name" yaml:"name" json:"name"`
	Env      string `mapstructure:"env" yaml:"env" json:"env"`
	OwnerQQ  string `mapstructure:"owner_qq" yaml:"owner_qq" json:"owner_qq"`
	DataDir  string `mapstructure:"data_dir" yaml:"data_dir" json:"data_dir"`
	LogLevel string `mapstructure:"log_level" yaml:"log_level" json:"log_level"`
}

type ServerConfig struct {
	Admin AdminServerConfig `mapstructure:"admin" yaml:"admin" json:"admin"`
	WebUI WebUIConfig       `mapstructure:"webui" yaml:"webui" json:"webui"`
}

type AdminServerConfig struct {
	Enabled     bool   `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Listen      string `mapstructure:"listen" yaml:"listen" json:"listen"`
	EnablePprof bool   `mapstructure:"enable_pprof" yaml:"enable_pprof" json:"enable_pprof"`
}

type WebUIConfig struct {
	Enabled  bool   `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	BasePath string `mapstructure:"base_path" yaml:"base_path" json:"base_path"`
	Theme    string `mapstructure:"theme" yaml:"theme" json:"theme"`
}

type StorageConfig struct {
	Engine     string           `mapstructure:"engine" yaml:"engine" json:"engine"`
	SQLite     SQLiteConfig     `mapstructure:"sqlite" yaml:"sqlite" json:"sqlite"`
	MySQL      MySQLConfig      `mapstructure:"mysql" yaml:"mysql" json:"mysql"`
	PostgreSQL PostgreSQLConfig `mapstructure:"postgresql" yaml:"postgresql" json:"postgresql"`
	Logs       LogsConfig       `mapstructure:"logs" yaml:"logs" json:"logs"`
	Media      MediaConfig      `mapstructure:"media" yaml:"media" json:"media"`
}

type SQLiteConfig struct {
	Path string `mapstructure:"path" yaml:"path" json:"path"`
}

type MySQLConfig struct {
	Host     string `mapstructure:"host" yaml:"host" json:"host"`
	Port     int    `mapstructure:"port" yaml:"port" json:"port"`
	Username string `mapstructure:"username" yaml:"username" json:"username"`
	Password string `mapstructure:"password" yaml:"password" json:"password"`
	Database string `mapstructure:"database" yaml:"database" json:"database"`
	Params   string `mapstructure:"params" yaml:"params" json:"params"`
}

type PostgreSQLConfig struct {
	Host     string `mapstructure:"host" yaml:"host" json:"host"`
	Port     int    `mapstructure:"port" yaml:"port" json:"port"`
	Username string `mapstructure:"username" yaml:"username" json:"username"`
	Password string `mapstructure:"password" yaml:"password" json:"password"`
	Database string `mapstructure:"database" yaml:"database" json:"database"`
	SSLMode  string `mapstructure:"ssl_mode" yaml:"ssl_mode" json:"ssl_mode"`
	Schema   string `mapstructure:"schema" yaml:"schema" json:"schema"`
}

type LogsConfig struct {
	Dir        string `mapstructure:"dir" yaml:"dir" json:"dir"`
	MaxSizeMB  int    `mapstructure:"max_size_mb" yaml:"max_size_mb" json:"max_size_mb"`
	MaxBackups int    `mapstructure:"max_backups" yaml:"max_backups" json:"max_backups"`
	MaxAgeDays int    `mapstructure:"max_age_days" yaml:"max_age_days" json:"max_age_days"`
}

type MediaConfig struct {
	Enabled                bool             `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Backend                string           `mapstructure:"backend" yaml:"backend" json:"backend"`
	MaxSizeMB              int              `mapstructure:"max_size_mb" yaml:"max_size_mb" json:"max_size_mb"`
	DownloadTimeoutSeconds int              `mapstructure:"download_timeout_seconds" yaml:"download_timeout_seconds" json:"download_timeout_seconds"`
	Local                  MediaLocalConfig `mapstructure:"local" yaml:"local" json:"local"`
	R2                     MediaR2Config    `mapstructure:"r2" yaml:"r2" json:"r2"`
}

type MediaLocalConfig struct {
	Dir string `mapstructure:"dir" yaml:"dir" json:"dir"`
}

type MediaR2Config struct {
	AccountID       string `mapstructure:"account_id" yaml:"account_id" json:"account_id"`
	Endpoint        string `mapstructure:"endpoint" yaml:"endpoint" json:"endpoint"`
	Bucket          string `mapstructure:"bucket" yaml:"bucket" json:"bucket"`
	AccessKeyID     string `mapstructure:"access_key_id" yaml:"access_key_id" json:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key" yaml:"secret_access_key" json:"secret_access_key"`
	PublicBaseURL   string `mapstructure:"public_base_url" yaml:"public_base_url" json:"public_base_url"`
	KeyPrefix       string `mapstructure:"key_prefix" yaml:"key_prefix" json:"key_prefix"`
}

type AIConfig struct {
	Enabled                bool                     `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Provider               AIProviderConfig         `mapstructure:"provider" yaml:"provider" json:"provider"`
	Vision                 AIVisionConfig           `mapstructure:"vision" yaml:"vision" json:"vision"`
	Reply                  AIReplyConfig            `mapstructure:"reply" yaml:"reply" json:"reply"`
	Proactive              AIProactiveConfig        `mapstructure:"proactive" yaml:"proactive" json:"proactive"`
	Memory                 AIMemoryConfig           `mapstructure:"memory" yaml:"memory" json:"memory"`
	CLI                    AICLIConfig              `mapstructure:"cli" yaml:"cli" json:"cli"`
	MCP                    AIMCPConfig              `mapstructure:"mcp" yaml:"mcp" json:"mcp"`
	Prompt                 AIPromptConfig           `mapstructure:"prompt" yaml:"prompt" json:"prompt"`
	PrivatePersonas        []AIPrivatePersonaConfig `mapstructure:"private_personas" yaml:"private_personas" json:"private_personas"`
	PrivateActivePersonaID string                   `mapstructure:"private_active_persona_id" yaml:"private_active_persona_id" json:"private_active_persona_id"`
	GroupPolicies          []AIGroupPolicyConfig    `mapstructure:"group_policies" yaml:"group_policies" json:"group_policies"`
}

type AIProviderConfig struct {
	Kind        string  `mapstructure:"kind" yaml:"kind" json:"kind"`
	Vendor      string  `mapstructure:"vendor" yaml:"vendor" json:"vendor"`
	BaseURL     string  `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
	APIKey      string  `mapstructure:"api_key" yaml:"api_key" json:"api_key"`
	Model       string  `mapstructure:"model" yaml:"model" json:"model"`
	TimeoutMS   int     `mapstructure:"timeout_ms" yaml:"timeout_ms" json:"timeout_ms"`
	Temperature float64 `mapstructure:"temperature" yaml:"temperature" json:"temperature"`
}

type AIVisionConfig struct {
	Enabled  bool             `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Mode     string           `mapstructure:"mode" yaml:"mode" json:"mode"`
	Provider AIProviderConfig `mapstructure:"provider" yaml:"provider" json:"provider"`
}

type AIReplyConfig struct {
	EnabledInGroup   bool               `mapstructure:"enabled_in_group" yaml:"enabled_in_group" json:"enabled_in_group"`
	EnabledInPrivate bool               `mapstructure:"enabled_in_private" yaml:"enabled_in_private" json:"enabled_in_private"`
	ReplyOnAt        bool               `mapstructure:"reply_on_at" yaml:"reply_on_at" json:"reply_on_at"`
	ReplyOnBotName   bool               `mapstructure:"reply_on_bot_name" yaml:"reply_on_bot_name" json:"reply_on_bot_name"`
	ReplyOnQuote     bool               `mapstructure:"reply_on_quote" yaml:"reply_on_quote" json:"reply_on_quote"`
	RequireAt        bool               `mapstructure:"require_at" yaml:"-" json:"-"`
	ReplyOnQuestion  bool               `mapstructure:"reply_on_question" yaml:"-" json:"-"`
	CooldownSeconds  int                `mapstructure:"cooldown_seconds" yaml:"cooldown_seconds" json:"cooldown_seconds"`
	MaxContextMsgs   int                `mapstructure:"max_context_messages" yaml:"max_context_messages" json:"max_context_messages"`
	MaxOutputTokens  int                `mapstructure:"max_output_tokens" yaml:"max_output_tokens" json:"max_output_tokens"`
	ThinkingMode     string             `mapstructure:"thinking_mode" yaml:"thinking_mode" json:"thinking_mode"`
	ThinkingEffort   string             `mapstructure:"thinking_effort" yaml:"thinking_effort" json:"thinking_effort"`
	ThinkingFormat   string             `mapstructure:"thinking_format" yaml:"thinking_format" json:"thinking_format"`
	Split            AIReplySplitConfig `mapstructure:"split" yaml:"split" json:"split"`
}

type AIReplySplitConfig struct {
	Enabled    bool `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	OnlyCasual bool `mapstructure:"only_casual" yaml:"only_casual" json:"only_casual"`
	MaxChars   int  `mapstructure:"max_chars" yaml:"max_chars" json:"max_chars"`
	MaxParts   int  `mapstructure:"max_parts" yaml:"max_parts" json:"max_parts"`
	DelayMS    int  `mapstructure:"delay_ms" yaml:"delay_ms" json:"delay_ms"`
}

type AIProactiveConfig struct {
	Enabled             bool     `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	MinIntervalSeconds  int      `mapstructure:"min_interval_seconds" yaml:"min_interval_seconds" json:"min_interval_seconds"`
	DailyLimitPerGroup  int      `mapstructure:"daily_limit_per_group" yaml:"daily_limit_per_group" json:"daily_limit_per_group"`
	Probability         float64  `mapstructure:"probability" yaml:"probability" json:"probability"`
	MinRecentMessages   int      `mapstructure:"min_recent_messages" yaml:"min_recent_messages" json:"min_recent_messages"`
	RecentWindowSeconds int      `mapstructure:"recent_window_seconds" yaml:"recent_window_seconds" json:"recent_window_seconds"`
	QuietHours          []string `mapstructure:"quiet_hours" yaml:"quiet_hours" json:"quiet_hours"`
}

type AIMemoryConfig struct {
	Enabled                 bool `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	SessionWindow           int  `mapstructure:"session_window" yaml:"session_window" json:"session_window"`
	CandidateEnabled        bool `mapstructure:"candidate_enabled" yaml:"candidate_enabled" json:"candidate_enabled"`
	PromoteThreshold        int  `mapstructure:"promote_threshold" yaml:"promote_threshold" json:"promote_threshold"`
	MaxPromptLongTerm       int  `mapstructure:"max_prompt_long_term" yaml:"max_prompt_long_term" json:"max_prompt_long_term"`
	MaxPromptCandidates     int  `mapstructure:"max_prompt_candidates" yaml:"max_prompt_candidates" json:"max_prompt_candidates"`
	ReflectionRawLimit      int  `mapstructure:"reflection_raw_limit" yaml:"reflection_raw_limit" json:"reflection_raw_limit"`
	ReflectionPerGroupLimit int  `mapstructure:"reflection_per_group_limit" yaml:"reflection_per_group_limit" json:"reflection_per_group_limit"`
}

type AICLIConfig struct {
	Enabled         bool     `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	AllowedCommands []string `mapstructure:"allowed_commands" yaml:"allowed_commands" json:"allowed_commands"`
	TimeoutSeconds  int      `mapstructure:"timeout_seconds" yaml:"timeout_seconds" json:"timeout_seconds"`
	MaxOutputBytes  int      `mapstructure:"max_output_bytes" yaml:"max_output_bytes" json:"max_output_bytes"`
}

type AIMCPConfig struct {
	Enabled bool                `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Servers []AIMCPServerConfig `mapstructure:"servers" yaml:"servers" json:"servers"`
}

type AIMCPServerConfig struct {
	ID              string            `mapstructure:"id" yaml:"id" json:"id"`
	Name            string            `mapstructure:"name" yaml:"name" json:"name"`
	Enabled         bool              `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Transport       string            `mapstructure:"transport" yaml:"transport" json:"transport"`
	Command         string            `mapstructure:"command" yaml:"command" json:"command"`
	Args            []string          `mapstructure:"args" yaml:"args" json:"args"`
	Env             map[string]string `mapstructure:"env" yaml:"env" json:"env"`
	URL             string            `mapstructure:"url" yaml:"url" json:"url"`
	Headers         map[string]string `mapstructure:"headers" yaml:"headers" json:"headers"`
	ProtocolVersion string            `mapstructure:"protocol_version" yaml:"protocol_version" json:"protocol_version"`
	TimeoutSeconds  int               `mapstructure:"timeout_seconds" yaml:"timeout_seconds" json:"timeout_seconds"`
	MaxOutputBytes  int               `mapstructure:"max_output_bytes" yaml:"max_output_bytes" json:"max_output_bytes"`
	AllowedTools    []string          `mapstructure:"allowed_tools" yaml:"allowed_tools" json:"allowed_tools"`
}

type AIPromptConfig struct {
	BotName      string `mapstructure:"bot_name" yaml:"bot_name" json:"bot_name"`
	SystemPrompt string `mapstructure:"system_prompt" yaml:"system_prompt" json:"system_prompt"`
}

type AIPrivatePersonaConfig struct {
	ID           string   `mapstructure:"id" yaml:"id" json:"id"`
	Name         string   `mapstructure:"name" yaml:"name" json:"name"`
	Description  string   `mapstructure:"description" yaml:"description" json:"description"`
	BotName      string   `mapstructure:"bot_name" yaml:"bot_name" json:"bot_name"`
	SystemPrompt string   `mapstructure:"system_prompt" yaml:"system_prompt" json:"system_prompt"`
	StyleTags    []string `mapstructure:"style_tags" yaml:"style_tags" json:"style_tags"`
	Enabled      bool     `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
}

type AIGroupPolicyConfig struct {
	GroupID         string `mapstructure:"group_id" yaml:"group_id" json:"group_id"`
	Name            string `mapstructure:"name" yaml:"name" json:"name"`
	ReplyEnabled    bool   `mapstructure:"reply_enabled" yaml:"reply_enabled" json:"reply_enabled"`
	ReplyOnAt       bool   `mapstructure:"reply_on_at" yaml:"reply_on_at" json:"reply_on_at"`
	ReplyOnBotName  bool   `mapstructure:"reply_on_bot_name" yaml:"reply_on_bot_name" json:"reply_on_bot_name"`
	ReplyOnQuote    bool   `mapstructure:"reply_on_quote" yaml:"reply_on_quote" json:"reply_on_quote"`
	RequireAt       bool   `mapstructure:"require_at" yaml:"-" json:"-"`
	ReplyOnQuestion bool   `mapstructure:"reply_on_question" yaml:"-" json:"-"`
	CooldownSeconds int    `mapstructure:"cooldown_seconds" yaml:"cooldown_seconds" json:"cooldown_seconds"`
	MaxContextMsgs  int    `mapstructure:"max_context_messages" yaml:"max_context_messages" json:"max_context_messages"`
	MaxOutputTokens int    `mapstructure:"max_output_tokens" yaml:"max_output_tokens" json:"max_output_tokens"`
	VisionEnabled   bool   `mapstructure:"vision_enabled" yaml:"vision_enabled" json:"vision_enabled"`
	PromptOverride  string `mapstructure:"prompt_override" yaml:"prompt_override" json:"prompt_override"`
}

type ConnectionConfig struct {
	ID       string        `mapstructure:"id" yaml:"id" json:"id"`
	Enabled  bool          `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Platform string        `mapstructure:"platform" yaml:"platform" json:"platform"`
	Ingress  IngressConfig `mapstructure:"ingress" yaml:"ingress" json:"ingress"`
	Action   ActionConfig  `mapstructure:"action" yaml:"action" json:"action"`
}

type IngressConfig struct {
	Type            string `mapstructure:"type" yaml:"type" json:"type"`
	Listen          string `mapstructure:"listen" yaml:"listen,omitempty" json:"listen,omitempty"`
	URL             string `mapstructure:"url" yaml:"url,omitempty" json:"url,omitempty"`
	Path            string `mapstructure:"path" yaml:"path,omitempty" json:"path,omitempty"`
	RetryIntervalMS int    `mapstructure:"retry_interval_ms" yaml:"retry_interval_ms,omitempty" json:"retry_interval_ms,omitempty"`
}

type ActionConfig struct {
	Type        string `mapstructure:"type" yaml:"type" json:"type"`
	BaseURL     string `mapstructure:"base_url" yaml:"base_url" json:"base_url"`
	TimeoutMS   int    `mapstructure:"timeout_ms" yaml:"timeout_ms" json:"timeout_ms"`
	AccessToken string `mapstructure:"access_token" yaml:"access_token" json:"access_token"`
}

type PluginConfig struct {
	ID      string         `mapstructure:"id" yaml:"id" json:"id"`
	Kind    string         `mapstructure:"kind" yaml:"kind" json:"kind"`
	Enabled bool           `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Config  map[string]any `mapstructure:"config" yaml:"config" json:"config"`
}

type SecurityConfig struct {
	AdminAuth AdminAuthConfig `mapstructure:"admin_auth" yaml:"admin_auth" json:"admin_auth"`
}

type AdminAuthConfig struct {
	Enabled  bool   `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Password string `mapstructure:"password" yaml:"password" json:"password"`
}
