package config

import "strings"

const (
	ActionTypeNapCatHTTP = "napcat_http"
	ActionTypeOneBotWS   = "onebot_ws"

	DefaultMCPProtocolVersion = "2025-06-18"
)

func NormalizeConnectionActionType(conn ConnectionConfig) string {
	if actionType := strings.TrimSpace(conn.Action.Type); actionType != "" {
		return actionType
	}
	switch strings.TrimSpace(conn.Ingress.Type) {
	case "ws_server", "ws_reverse":
		return ActionTypeOneBotWS
	default:
		return ActionTypeNapCatHTTP
	}
}

func ConnectionUsesHTTPAction(conn ConnectionConfig) bool {
	return NormalizeConnectionActionType(conn) == ActionTypeNapCatHTTP
}

func NormalizeConnectionConfig(conn ConnectionConfig) ConnectionConfig {
	conn.Platform = "onebot_v11"
	conn.Action.Type = NormalizeConnectionActionType(conn)
	if conn.Action.TimeoutMS <= 0 {
		conn.Action.TimeoutMS = 10000
	}
	switch strings.TrimSpace(conn.Ingress.Type) {
	case "ws_server":
		if strings.TrimSpace(conn.Ingress.Path) == "" {
			conn.Ingress.Path = "/ws"
		}
	case "http_callback":
		if strings.TrimSpace(conn.Ingress.Path) == "" {
			conn.Ingress.Path = "/callback"
		}
	case "ws_reverse":
		if conn.Ingress.RetryIntervalMS <= 0 {
			conn.Ingress.RetryIntervalMS = 30000
		}
	}
	return conn
}

func NormalizeConnectionConfigs(items []ConnectionConfig) []ConnectionConfig {
	out := make([]ConnectionConfig, 0, len(items))
	for _, item := range items {
		out = append(out, NormalizeConnectionConfig(item))
	}
	return out
}

func NormalizeAIReplySplitConfig(cfg AIReplySplitConfig) AIReplySplitConfig {
	if cfg.MaxChars <= 0 {
		cfg.MaxChars = 80
	}
	if cfg.MaxParts <= 0 {
		cfg.MaxParts = 3
	}
	if cfg.DelayMS < 0 {
		cfg.DelayMS = 0
	}
	return cfg
}

func NormalizeAIReplyConfig(cfg AIReplyConfig) AIReplyConfig {
	if !cfg.ReplyOnAt && cfg.RequireAt {
		cfg.ReplyOnAt = true
	}
	if !cfg.ReplyOnBotName && cfg.ReplyOnQuestion {
		cfg.ReplyOnBotName = true
	}
	cfg.RequireAt = false
	cfg.ReplyOnQuestion = false
	effort := NormalizeAIThinkingEffort(cfg.ThinkingEffort)
	mode := NormalizeAIThinkingMode(cfg.ThinkingMode)
	if mode == "enabled" {
		if effort == "max" {
			mode = "xhigh"
		} else {
			mode = "high"
		}
	}
	cfg.ThinkingMode = mode
	switch mode {
	case "xhigh":
		cfg.ThinkingEffort = "max"
	case "high":
		cfg.ThinkingEffort = "high"
	default:
		cfg.ThinkingEffort = effort
	}
	cfg.ThinkingFormat = NormalizeAIThinkingFormat(cfg.ThinkingFormat)
	cfg.Split = NormalizeAIReplySplitConfig(cfg.Split)
	return cfg
}

func NormalizeAIThinkingMode(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "auto", "default":
		return "auto"
	case "high":
		return "high"
	case "xhigh", "max":
		return "xhigh"
	case "enabled", "enable", "on", "true":
		return "enabled"
	case "disabled", "disable", "off", "false":
		return "disabled"
	default:
		return strings.TrimSpace(strings.ToLower(value))
	}
}

func NormalizeAIThinkingEffort(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "auto", "low", "medium", "high":
		return "high"
	case "xhigh", "max":
		return "max"
	default:
		return strings.TrimSpace(strings.ToLower(value))
	}
}

func NormalizeAIThinkingFormat(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "openai", "openai_compatible":
		return "openai"
	case "anthropic", "claude":
		return "anthropic"
	default:
		return strings.TrimSpace(strings.ToLower(value))
	}
}

func NormalizeAIGroupPolicyConfig(cfg AIGroupPolicyConfig) AIGroupPolicyConfig {
	if !cfg.ReplyOnAt && cfg.RequireAt {
		cfg.ReplyOnAt = true
	}
	if !cfg.ReplyOnBotName && cfg.ReplyOnQuestion {
		cfg.ReplyOnBotName = true
	}
	cfg.RequireAt = false
	cfg.ReplyOnQuestion = false
	return cfg
}

func NormalizeAIProactiveConfig(cfg AIProactiveConfig) AIProactiveConfig {
	if cfg.MinIntervalSeconds <= 0 {
		cfg.MinIntervalSeconds = 900
	}
	if cfg.DailyLimitPerGroup <= 0 {
		cfg.DailyLimitPerGroup = 8
	}
	if cfg.Probability <= 0 {
		cfg.Probability = 0.08
	}
	if cfg.MinRecentMessages <= 0 {
		cfg.MinRecentMessages = 4
	}
	if cfg.RecentWindowSeconds <= 0 {
		cfg.RecentWindowSeconds = 600
	}
	items := make([]string, 0, len(cfg.QuietHours))
	for _, item := range cfg.QuietHours {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		items = append(items, item)
	}
	cfg.QuietHours = items
	return cfg
}

func NormalizeAICLIConfig(cfg AICLIConfig) AICLIConfig {
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 10
	}
	if cfg.MaxOutputBytes <= 0 {
		cfg.MaxOutputBytes = 8192
	}
	items := make([]string, 0, len(cfg.AllowedCommands))
	for _, item := range cfg.AllowedCommands {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		items = append(items, item)
	}
	cfg.AllowedCommands = items
	return cfg
}

func NormalizeAIMCPConfig(cfg AIMCPConfig) AIMCPConfig {
	out := make([]AIMCPServerConfig, 0, len(cfg.Servers))
	for _, server := range cfg.Servers {
		server.ID = strings.TrimSpace(server.ID)
		server.Name = strings.TrimSpace(server.Name)
		server.Transport = strings.TrimSpace(strings.ToLower(server.Transport))
		if server.Transport == "" {
			server.Transport = "stdio"
		}
		server.Command = strings.TrimSpace(server.Command)
		server.URL = strings.TrimSpace(server.URL)
		server.ProtocolVersion = strings.TrimSpace(server.ProtocolVersion)
		if server.ProtocolVersion == "" {
			server.ProtocolVersion = DefaultMCPProtocolVersion
		}
		if server.TimeoutSeconds <= 0 {
			server.TimeoutSeconds = 15
		}
		if server.MaxOutputBytes <= 0 {
			server.MaxOutputBytes = 65536
		}
		server.Args = normalizeStringList(server.Args)
		server.AllowedTools = normalizeStringList(server.AllowedTools)
		server.Env = normalizeStringMap(server.Env)
		server.Headers = normalizeStringMap(server.Headers)
		out = append(out, server)
	}
	cfg.Servers = out
	return cfg
}

func normalizeStringList(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func normalizeStringMap(items map[string]string) map[string]string {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]string, len(items))
	for key, value := range items {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func NormalizeAIConfig(cfg AIConfig) AIConfig {
	cfg.Reply = NormalizeAIReplyConfig(cfg.Reply)
	cfg.Proactive = NormalizeAIProactiveConfig(cfg.Proactive)
	cfg.Memory = NormalizeAIMemoryConfig(cfg.Memory)
	cfg.CLI = NormalizeAICLIConfig(cfg.CLI)
	cfg.MCP = NormalizeAIMCPConfig(cfg.MCP)
	for i := range cfg.GroupPolicies {
		cfg.GroupPolicies[i] = NormalizeAIGroupPolicyConfig(cfg.GroupPolicies[i])
	}
	return cfg
}

func NormalizeAIMemoryConfig(cfg AIMemoryConfig) AIMemoryConfig {
	if cfg.SessionWindow <= 0 {
		cfg.SessionWindow = 24
	}
	if cfg.PromoteThreshold < 2 {
		cfg.PromoteThreshold = 2
	}
	if cfg.MaxPromptLongTerm <= 0 {
		cfg.MaxPromptLongTerm = 4
	}
	if cfg.MaxPromptCandidates <= 0 {
		cfg.MaxPromptCandidates = 3
	}
	if cfg.ReflectionRawLimit <= 0 {
		cfg.ReflectionRawLimit = 768
	}
	if cfg.ReflectionPerGroupLimit <= 0 {
		cfg.ReflectionPerGroupLimit = 36
	}
	return cfg
}

func NormalizeConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.Connections = NormalizeConnectionConfigs(cfg.Connections)
	cfg.AI = NormalizeAIConfig(cfg.AI)
}
