package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
var qqPattern = regexp.MustCompile(`^\d+$`)

func Validate(cfg *Config) error {
	var errs []string

	if cfg.App.Name == "" {
		errs = append(errs, "app.name（机器人昵称）不能为空")
	}
	if cfg.App.DataDir == "" {
		errs = append(errs, "app.data_dir 不能为空")
	}
	if ownerQQ := strings.TrimSpace(cfg.App.OwnerQQ); ownerQQ != "" && !qqPattern.MatchString(ownerQQ) {
		errs = append(errs, "app.owner_qq 只能包含数字")
	}
	switch cfg.App.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, "app.log_level 只能是 debug/info/warn/error")
	}

	if (cfg.Server.Admin.Enabled || cfg.Server.WebUI.Enabled) && cfg.Server.Admin.Listen == "" {
		errs = append(errs, "启用 server.admin 或 server.webui 时，server.admin.listen 不能为空")
	}
	if theme := NormalizeWebUITheme(cfg.Server.WebUI.Theme); !IsSupportedWebUITheme(theme) {
		errs = append(errs, fmt.Sprintf("server.webui.theme 只能是 %s", SupportedWebUIThemeList("/")))
	}

	errs = append(errs, validateStorage(cfg.Storage)...)

	seenConnections := map[string]struct{}{}
	for i, conn := range cfg.Connections {
		prefix := fmt.Sprintf("connections[%d]", i)
		if conn.ID == "" {
			errs = append(errs, prefix+".id 不能为空")
		} else {
			if !idPattern.MatchString(conn.ID) {
				errs = append(errs, prefix+".id 只能包含字母、数字、-、_")
			}
			if _, exists := seenConnections[conn.ID]; exists {
				errs = append(errs, prefix+".id 重复: "+conn.ID)
			}
			seenConnections[conn.ID] = struct{}{}
		}

		if conn.Platform != "onebot_v11" {
			errs = append(errs, prefix+".platform 当前只支持 onebot_v11")
		}

		switch conn.Ingress.Type {
		case "ws_server":
			if conn.Ingress.Listen == "" {
				errs = append(errs, prefix+".ingress.listen 不能为空")
			}
		case "ws_reverse":
			if conn.Ingress.URL == "" {
				errs = append(errs, prefix+".ingress.url 不能为空")
			}
		case "http_callback":
			if conn.Ingress.Listen == "" {
				errs = append(errs, prefix+".ingress.listen 不能为空")
			}
			if conn.Ingress.Path == "" {
				errs = append(errs, prefix+".ingress.path 不能为空")
			}
		default:
			errs = append(errs, prefix+".ingress.type 当前只支持 ws_server/ws_reverse/http_callback")
		}

		actionType := NormalizeConnectionActionType(conn)
		switch actionType {
		case ActionTypeNapCatHTTP, ActionTypeOneBotWS:
		default:
			errs = append(errs, prefix+".action.type 当前只支持 napcat_http/onebot_ws")
		}
		if actionType == ActionTypeOneBotWS && conn.Ingress.Type == "http_callback" {
			errs = append(errs, prefix+".action.type=onebot_ws 仅支持 ws_server/ws_reverse")
		}
		if actionType == ActionTypeNapCatHTTP && strings.TrimSpace(conn.Action.BaseURL) == "" {
			errs = append(errs, prefix+".action.base_url 不能为空")
		}
		if conn.Action.TimeoutMS <= 0 {
			errs = append(errs, prefix+".action.timeout_ms 必须大于 0")
		}
	}

	errs = append(errs, validateAI(cfg.AI)...)

	seenPlugins := map[string]struct{}{}
	for i, plugin := range cfg.Plugins {
		prefix := fmt.Sprintf("plugins[%d]", i)
		if plugin.ID == "" {
			errs = append(errs, prefix+".id 不能为空")
		} else {
			if !idPattern.MatchString(plugin.ID) {
				errs = append(errs, prefix+".id 只能包含字母、数字、-、_")
			}
			if _, exists := seenPlugins[plugin.ID]; exists {
				errs = append(errs, prefix+".id 重复: "+plugin.ID)
			}
			seenPlugins[plugin.ID] = struct{}{}
		}
		if plugin.Kind == "" {
			plugin.Kind = "builtin"
		}
		switch plugin.Kind {
		case "builtin", "external_exec":
		default:
			errs = append(errs, prefix+".kind 当前只支持 builtin/external_exec")
		}
	}

	if cfg.Security.AdminAuth.Enabled {
		if strings.TrimSpace(cfg.Security.AdminAuth.Password) == "" {
			errs = append(errs, "启用 security.admin_auth 时，security.admin_auth.password 不能为空")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("配置校验失败:\n- %s", strings.Join(errs, "\n- "))
	}
	return nil
}

func validateAI(ai AIConfig) []string {
	var errs []string

	errs = append(errs, validateAIProviderKind(ai.Provider, "ai.provider")...)
	errs = append(errs, validateAIThinking(ai.Reply)...)
	errs = append(errs, validateAIVision(ai.Vision, ai.Enabled)...)
	errs = append(errs, validateAIReplySplit(ai.Reply.Split)...)
	errs = append(errs, validateAIProactive(ai.Proactive)...)
	errs = append(errs, validateAICLI(ai.CLI)...)
	errs = append(errs, validateAIMCP(ai.MCP)...)
	errs = append(errs, validateAIPrivatePersonas(ai.PrivatePersonas, ai.PrivateActivePersonaID)...)
	errs = append(errs, validateAIGroupPolicies(ai.GroupPolicies)...)

	if !ai.Enabled {
		return errs
	}

	errs = append(errs, validateAIProviderConfig(ai.Provider, "ai.provider")...)
	if ai.Reply.CooldownSeconds < 0 {
		errs = append(errs, "ai.reply.cooldown_seconds 不能小于 0")
	}
	if ai.Reply.MaxContextMsgs <= 0 || ai.Reply.MaxContextMsgs > 64 {
		errs = append(errs, "ai.reply.max_context_messages 必须在 1 到 64 之间")
	}
	if ai.Reply.MaxOutputTokens <= 0 || ai.Reply.MaxOutputTokens > 4096 {
		errs = append(errs, "ai.reply.max_output_tokens 必须在 1 到 4096 之间")
	}
	if ai.Memory.SessionWindow <= 0 || ai.Memory.SessionWindow > 128 {
		errs = append(errs, "ai.memory.session_window 必须在 1 到 128 之间")
	}
	if ai.Memory.PromoteThreshold < 2 {
		errs = append(errs, "ai.memory.promote_threshold 不能小于 2")
	}
	if ai.Memory.MaxPromptLongTerm <= 0 || ai.Memory.MaxPromptLongTerm > 16 {
		errs = append(errs, "ai.memory.max_prompt_long_term 必须在 1 到 16 之间")
	}
	if ai.Memory.MaxPromptCandidates <= 0 || ai.Memory.MaxPromptCandidates > 16 {
		errs = append(errs, "ai.memory.max_prompt_candidates 必须在 1 到 16 之间")
	}
	if ai.Memory.ReflectionRawLimit <= 0 || ai.Memory.ReflectionRawLimit > 2048 {
		errs = append(errs, "ai.memory.reflection_raw_limit 必须在 1 到 2048 之间")
	}
	if ai.Memory.ReflectionPerGroupLimit <= 0 || ai.Memory.ReflectionPerGroupLimit > 128 {
		errs = append(errs, "ai.memory.reflection_per_group_limit 必须在 1 到 128 之间")
	}

	if !ai.Reply.EnabledInGroup && !ai.Reply.EnabledInPrivate {
		errs = append(errs, "启用 ai 时，至少需要开启一种回复场景（群聊或私聊）")
	}

	return errs
}

func validateAIThinking(reply AIReplyConfig) []string {
	var errs []string
	switch NormalizeAIReplyConfig(reply).ThinkingMode {
	case "auto", "high", "xhigh", "disabled":
	default:
		errs = append(errs, "ai.reply.thinking_mode 只能是 auto、high 或 xhigh")
	}
	switch NormalizeAIThinkingEffort(reply.ThinkingEffort) {
	case "high", "max":
	default:
		errs = append(errs, "ai.reply.thinking_effort 只能是 high 或 max")
	}
	switch NormalizeAIThinkingFormat(reply.ThinkingFormat) {
	case "openai", "anthropic":
	default:
		errs = append(errs, "ai.reply.thinking_format 只能是 openai 或 anthropic")
	}
	return errs
}

func validateAIPrivatePersonas(items []AIPrivatePersonaConfig, activeID string) []string {
	var errs []string
	seen := map[string]struct{}{}
	activeID = strings.TrimSpace(activeID)
	hasActive := activeID == ""
	for i, item := range items {
		prefix := fmt.Sprintf("ai.private_personas[%d]", i)
		id := strings.TrimSpace(item.ID)
		name := strings.TrimSpace(item.Name)
		if id == "" {
			errs = append(errs, prefix+".id 不能为空")
		} else {
			if !idPattern.MatchString(id) {
				errs = append(errs, prefix+".id 只能包含字母、数字、-、_")
			}
			if _, exists := seen[id]; exists {
				errs = append(errs, prefix+".id 重复: "+id)
			}
			seen[id] = struct{}{}
			if activeID != "" && activeID == id {
				hasActive = true
			}
		}
		if name == "" {
			errs = append(errs, prefix+".name 不能为空")
		}
		if strings.TrimSpace(item.SystemPrompt) == "" {
			errs = append(errs, prefix+".system_prompt 不能为空")
		}
	}
	if activeID != "" && !hasActive {
		errs = append(errs, "ai.private_active_persona_id 必须指向一个已存在的人格模板")
	}
	return errs
}

func validateAIGroupPolicies(items []AIGroupPolicyConfig) []string {
	var errs []string
	seen := map[string]struct{}{}
	for i, item := range items {
		prefix := fmt.Sprintf("ai.group_policies[%d]", i)
		groupID := strings.TrimSpace(item.GroupID)
		if groupID == "" {
			errs = append(errs, prefix+".group_id 不能为空")
			continue
		}
		if !idPattern.MatchString(groupID) {
			errs = append(errs, prefix+".group_id 只能包含字母、数字、-、_")
		}
		if _, exists := seen[groupID]; exists {
			errs = append(errs, prefix+".group_id 重复: "+groupID)
		}
		seen[groupID] = struct{}{}
		if item.CooldownSeconds < 0 {
			errs = append(errs, prefix+".cooldown_seconds 不能小于 0")
		}
		if item.MaxContextMsgs <= 0 || item.MaxContextMsgs > 64 {
			errs = append(errs, prefix+".max_context_messages 必须在 1 到 64 之间")
		}
		if item.MaxOutputTokens <= 0 || item.MaxOutputTokens > 4096 {
			errs = append(errs, prefix+".max_output_tokens 必须在 1 到 4096 之间")
		}
	}
	return errs
}

func validateAIProviderKind(provider AIProviderConfig, prefix string) []string {
	switch provider.Kind {
	case "", "openai_compatible":
		return nil
	default:
		return []string{prefix + ".kind 当前只支持 openai_compatible"}
	}
}

func validateAIProviderConfig(provider AIProviderConfig, prefix string) []string {
	var errs []string

	if provider.TimeoutMS <= 0 {
		errs = append(errs, prefix+".timeout_ms 必须大于 0")
	}
	if provider.Temperature < 0 || provider.Temperature > 2 {
		errs = append(errs, prefix+".temperature 必须在 0 到 2 之间")
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		errs = append(errs, "启用 ai 时，"+prefix+".base_url 不能为空")
	} else if parsed, err := url.Parse(strings.TrimSpace(provider.BaseURL)); err != nil || parsed.Scheme == "" || parsed.Host == "" {
		errs = append(errs, prefix+".base_url 必须是合法的 http/https 地址")
	}
	if strings.TrimSpace(provider.Model) == "" {
		errs = append(errs, "启用 ai 时，"+prefix+".model 不能为空")
	}

	return errs
}

func validateAIVision(vision AIVisionConfig, aiEnabled bool) []string {
	var errs []string
	mode := normalizeAIVisionMode(vision.Mode)
	if mode == "" {
		errs = append(errs, "ai.vision.mode 只能是 same_as_chat 或 independent")
		return errs
	}
	if !vision.Enabled {
		return errs
	}
	if !aiEnabled {
		errs = append(errs, "启用 ai.vision 时需要先启用 ai.enabled")
	}
	if mode == "independent" {
		errs = append(errs, validateAIProviderKind(vision.Provider, "ai.vision.provider")...)
		errs = append(errs, validateAIProviderConfig(vision.Provider, "ai.vision.provider")...)
	}
	return errs
}

func validateAIReplySplit(split AIReplySplitConfig) []string {
	var errs []string
	split = NormalizeAIReplySplitConfig(split)
	if split.MaxChars < 20 || split.MaxChars > 500 {
		errs = append(errs, "ai.reply.split.max_chars 必须在 20 到 500 之间")
	}
	if split.MaxParts < 1 || split.MaxParts > 6 {
		errs = append(errs, "ai.reply.split.max_parts 必须在 1 到 6 之间")
	}
	if split.DelayMS < 0 || split.DelayMS > 10000 {
		errs = append(errs, "ai.reply.split.delay_ms 必须在 0 到 10000 之间")
	}
	return errs
}

func validateAIProactive(proactive AIProactiveConfig) []string {
	var errs []string
	proactive = NormalizeAIProactiveConfig(proactive)
	if proactive.MinIntervalSeconds < 30 || proactive.MinIntervalSeconds > 86400 {
		errs = append(errs, "ai.proactive.min_interval_seconds 必须在 30 到 86400 之间")
	}
	if proactive.DailyLimitPerGroup < 1 || proactive.DailyLimitPerGroup > 100 {
		errs = append(errs, "ai.proactive.daily_limit_per_group 必须在 1 到 100 之间")
	}
	if proactive.Probability <= 0 || proactive.Probability > 1 {
		errs = append(errs, "ai.proactive.probability 必须大于 0 且不超过 1")
	}
	if proactive.MinRecentMessages < 1 || proactive.MinRecentMessages > 100 {
		errs = append(errs, "ai.proactive.min_recent_messages 必须在 1 到 100 之间")
	}
	if proactive.RecentWindowSeconds < 60 || proactive.RecentWindowSeconds > 86400 {
		errs = append(errs, "ai.proactive.recent_window_seconds 必须在 60 到 86400 之间")
	}
	for i, item := range proactive.QuietHours {
		if strings.TrimSpace(item) == "" {
			errs = append(errs, fmt.Sprintf("ai.proactive.quiet_hours[%d] 不能为空", i))
			continue
		}
		if _, _, ok := parseQuietHourRange(item); !ok {
			errs = append(errs, fmt.Sprintf("ai.proactive.quiet_hours[%d] 必须使用 HH:MM-HH:MM 格式", i))
		}
	}
	return errs
}

func parseQuietHourRange(value string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, ok := parseClockMinute(parts[0])
	if !ok {
		return 0, 0, false
	}
	end, ok := parseClockMinute(parts[1])
	if !ok {
		return 0, 0, false
	}
	return start, end, true
}

func parseClockMinute(value string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, false
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, false
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

func validateAICLI(cli AICLIConfig) []string {
	var errs []string
	cli = NormalizeAICLIConfig(cli)
	if cli.TimeoutSeconds < 1 || cli.TimeoutSeconds > 300 {
		errs = append(errs, "ai.cli.timeout_seconds 必须在 1 到 300 之间")
	}
	if cli.MaxOutputBytes < 256 || cli.MaxOutputBytes > 65536 {
		errs = append(errs, "ai.cli.max_output_bytes 必须在 256 到 65536 之间")
	}
	if cli.Enabled && len(cli.AllowedCommands) == 0 {
		errs = append(errs, "启用 ai.cli 时，ai.cli.allowed_commands 不能为空")
	}
	for i, item := range cli.AllowedCommands {
		if strings.TrimSpace(item) == "" {
			errs = append(errs, fmt.Sprintf("ai.cli.allowed_commands[%d] 不能为空", i))
		}
	}
	return errs
}

func validateAIMCP(mcp AIMCPConfig) []string {
	var errs []string
	mcp = NormalizeAIMCPConfig(mcp)
	seen := map[string]struct{}{}
	for i, server := range mcp.Servers {
		prefix := fmt.Sprintf("ai.mcp.servers[%d]", i)
		if server.ID == "" {
			errs = append(errs, prefix+".id 不能为空")
		} else {
			if !idPattern.MatchString(server.ID) {
				errs = append(errs, prefix+".id 只能包含字母、数字、-、_")
			}
			if _, exists := seen[server.ID]; exists {
				errs = append(errs, prefix+".id 重复: "+server.ID)
			}
			seen[server.ID] = struct{}{}
		}
		if !mcp.Enabled || !server.Enabled {
			continue
		}
		switch server.Transport {
		case "stdio":
			if server.Command == "" {
				errs = append(errs, prefix+".command 不能为空")
			}
		case "http", "streamable_http":
			if server.URL == "" {
				errs = append(errs, prefix+".url 不能为空")
			} else if parsed, err := url.Parse(server.URL); err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				errs = append(errs, prefix+".url 必须是合法的 http/https 地址")
			}
		default:
			errs = append(errs, prefix+".transport 只能是 stdio/http/streamable_http")
		}
		if server.TimeoutSeconds < 1 || server.TimeoutSeconds > 300 {
			errs = append(errs, prefix+".timeout_seconds 必须在 1 到 300 之间")
		}
		if server.MaxOutputBytes < 4096 || server.MaxOutputBytes > 4*1024*1024 {
			errs = append(errs, prefix+".max_output_bytes 必须在 4096 到 4194304 之间")
		}
		for j, item := range server.AllowedTools {
			if strings.TrimSpace(item) == "" {
				errs = append(errs, fmt.Sprintf("%s.allowed_tools[%d] 不能为空", prefix, j))
			}
		}
	}
	return errs
}

func normalizeAIVisionMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "", "same_as_chat":
		return "same_as_chat"
	case "independent":
		return "independent"
	case "disabled":
		return "disabled"
	default:
		return strings.TrimSpace(strings.ToLower(mode))
	}
}

func validateStorage(storage StorageConfig) []string {
	var errs []string

	engine := normalizeStorageEngine(storage.Engine)
	switch engine {
	case "sqlite":
		if strings.TrimSpace(storage.SQLite.Path) == "" {
			errs = append(errs, "storage.sqlite.path 不能为空")
		}
	case "mysql":
		if strings.TrimSpace(storage.MySQL.Host) == "" {
			errs = append(errs, "storage.mysql.host 不能为空")
		}
		if storage.MySQL.Port <= 0 {
			errs = append(errs, "storage.mysql.port 必须大于 0")
		}
		if strings.TrimSpace(storage.MySQL.Username) == "" {
			errs = append(errs, "storage.mysql.username 不能为空")
		}
		if strings.TrimSpace(storage.MySQL.Database) == "" {
			errs = append(errs, "storage.mysql.database 不能为空")
		}
	case "postgresql":
		if strings.TrimSpace(storage.PostgreSQL.Host) == "" {
			errs = append(errs, "storage.postgresql.host 不能为空")
		}
		if storage.PostgreSQL.Port <= 0 {
			errs = append(errs, "storage.postgresql.port 必须大于 0")
		}
		if strings.TrimSpace(storage.PostgreSQL.Username) == "" {
			errs = append(errs, "storage.postgresql.username 不能为空")
		}
		if strings.TrimSpace(storage.PostgreSQL.Database) == "" {
			errs = append(errs, "storage.postgresql.database 不能为空")
		}
		sslMode := strings.TrimSpace(strings.ToLower(storage.PostgreSQL.SSLMode))
		switch sslMode {
		case "", "disable", "require", "verify-ca", "verify-full":
		default:
			errs = append(errs, "storage.postgresql.ssl_mode 只能是 disable/require/verify-ca/verify-full")
		}
	default:
		errs = append(errs, "storage.engine 当前只支持 sqlite/mysql/postgresql")
	}

	if strings.TrimSpace(storage.Logs.Dir) == "" {
		errs = append(errs, "storage.logs.dir 不能为空")
	}
	if storage.Logs.MaxSizeMB <= 0 {
		errs = append(errs, "storage.logs.max_size_mb 必须大于 0")
	}
	if storage.Logs.MaxBackups < 0 {
		errs = append(errs, "storage.logs.max_backups 不能小于 0")
	}
	if storage.Logs.MaxAgeDays < 0 {
		errs = append(errs, "storage.logs.max_age_days 不能小于 0")
	}
	if storage.Media.MaxSizeMB < 0 {
		errs = append(errs, "storage.media.max_size_mb 不能小于 0")
	}
	if storage.Media.DownloadTimeoutSeconds < 0 {
		errs = append(errs, "storage.media.download_timeout_seconds 不能小于 0")
	}
	if !storage.Media.Enabled {
		return errs
	}

	mediaBackend := normalizeMediaStorageBackend(storage.Media.Backend)
	switch mediaBackend {
	case "local":
		if strings.TrimSpace(storage.Media.Local.Dir) == "" {
			errs = append(errs, "storage.media.local.dir 不能为空")
		}
	case "r2":
		r2 := storage.Media.R2
		if strings.TrimSpace(r2.Bucket) == "" {
			errs = append(errs, "storage.media.r2.bucket 不能为空")
		}
		if strings.TrimSpace(r2.AccessKeyID) == "" {
			errs = append(errs, "storage.media.r2.access_key_id 不能为空")
		}
		if strings.TrimSpace(r2.SecretAccessKey) == "" {
			errs = append(errs, "storage.media.r2.secret_access_key 不能为空")
		}
		endpoint := strings.TrimSpace(r2.Endpoint)
		accountID := strings.TrimSpace(r2.AccountID)
		if endpoint == "" && accountID == "" {
			errs = append(errs, "storage.media.r2.endpoint 与 storage.media.r2.account_id 至少需要配置一项")
		}
		if endpoint != "" {
			if parsed, err := url.Parse(endpoint); err != nil || parsed.Scheme == "" || parsed.Host == "" {
				errs = append(errs, "storage.media.r2.endpoint 必须是合法的 http/https 地址")
			}
		}
		if publicBaseURL := strings.TrimSpace(r2.PublicBaseURL); publicBaseURL != "" {
			if parsed, err := url.Parse(publicBaseURL); err != nil || parsed.Scheme == "" || parsed.Host == "" {
				errs = append(errs, "storage.media.r2.public_base_url 必须是合法的 http/https 地址")
			}
		}
	default:
		errs = append(errs, "storage.media.backend 当前只支持 local/r2")
	}

	return errs
}

func normalizeStorageEngine(engine string) string {
	switch strings.TrimSpace(strings.ToLower(engine)) {
	case "", "sqlite":
		return "sqlite"
	case "mysql":
		return "mysql"
	case "postgres", "postgresql":
		return "postgresql"
	default:
		return strings.TrimSpace(strings.ToLower(engine))
	}
}

func normalizeMediaStorageBackend(backend string) string {
	switch strings.TrimSpace(strings.ToLower(backend)) {
	case "", "local":
		return "local"
	case "r2", "cloudflare_r2", "cloudflare-r2":
		return "r2"
	default:
		return strings.TrimSpace(strings.ToLower(backend))
	}
}
