package config

import (
	"strings"
	"testing"
)

func TestValidate_AllowsNumericOwnerQQ(t *testing.T) {
	cfg := testConfig()
	cfg.App.OwnerQQ = "123456789"

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_AllowsEmptyConnections(t *testing.T) {
	cfg := testConfig()
	cfg.Connections = nil

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_AllowsAllConnectionsDisabled(t *testing.T) {
	cfg := testConfig()
	cfg.Connections[0].Enabled = false

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_RejectsEmptyAppNameWithBotNicknameHint(t *testing.T) {
	cfg := testConfig()
	cfg.App.Name = ""

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want empty app.name error")
	}
	if got := err.Error(); !strings.Contains(got, "app.name（机器人昵称）不能为空") {
		t.Fatalf("Validate() error = %q, want bot nickname hint", got)
	}
}

func TestValidate_RejectsInvalidOwnerQQ(t *testing.T) {
	cfg := testConfig()
	cfg.App.OwnerQQ = "qq-123"

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want invalid owner_qq error")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "app.owner_qq 只能包含数字") {
		t.Fatalf("Validate() error = %q, want owner_qq validation message", got)
	}
}

func TestValidate_RejectsInvalidWebUITheme(t *testing.T) {
	cfg := testConfig()
	cfg.Server.WebUI.Theme = "sunset"

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want invalid webui theme error")
	}
	if got := err.Error(); !strings.Contains(got, "server.webui.theme 只能是 "+SupportedWebUIThemeList("/")) {
		t.Fatalf("Validate() error = %q, want webui theme validation message", got)
	}
}

func TestValidate_AllowsMySQLStorage(t *testing.T) {
	cfg := testConfig()
	cfg.Storage.Engine = "mysql"
	cfg.Storage.MySQL.Host = "127.0.0.1"
	cfg.Storage.MySQL.Port = 3306
	cfg.Storage.MySQL.Username = "root"
	cfg.Storage.MySQL.Database = "go_bot"

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_RejectsInvalidPostgreSQLStorage(t *testing.T) {
	cfg := testConfig()
	cfg.Storage.Engine = "postgresql"
	cfg.Storage.PostgreSQL.Host = ""
	cfg.Storage.PostgreSQL.Port = 0
	cfg.Storage.PostgreSQL.Username = ""
	cfg.Storage.PostgreSQL.Database = ""

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want invalid postgresql storage error")
	}
	if got := err.Error(); !strings.Contains(got, "storage.postgresql.host 不能为空") {
		t.Fatalf("Validate() error = %q, want storage.postgresql.host validation message", got)
	}
}

func TestValidate_AllowsLocalMediaStorage(t *testing.T) {
	cfg := testConfig()
	cfg.Storage.Media.Enabled = true
	cfg.Storage.Media.Backend = "local"
	cfg.Storage.Media.Local.Dir = "./data/media"

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_AllowsWSServerWithoutActionBaseURL(t *testing.T) {
	cfg := testConfig()
	cfg.Connections[0].Ingress.Type = "ws_server"
	cfg.Connections[0].Action = ActionConfig{
		Type:      ActionTypeOneBotWS,
		TimeoutMS: 10000,
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_RejectsHTTPCallbackWithoutActionBaseURL(t *testing.T) {
	cfg := testConfig()
	cfg.Connections[0].Ingress.Type = "http_callback"
	cfg.Connections[0].Ingress.Path = "/callback"
	cfg.Connections[0].Action = ActionConfig{
		Type:      ActionTypeNapCatHTTP,
		TimeoutMS: 10000,
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want action.base_url validation error")
	}
	if got := err.Error(); !strings.Contains(got, "connections[0].action.base_url 不能为空") {
		t.Fatalf("Validate() error = %q, want action.base_url validation message", got)
	}
}

func TestValidate_RejectsHTTPCallbackUsingOneBotWSAction(t *testing.T) {
	cfg := testConfig()
	cfg.Connections[0].Ingress.Type = "http_callback"
	cfg.Connections[0].Ingress.Path = "/callback"
	cfg.Connections[0].Action = ActionConfig{
		Type:      ActionTypeOneBotWS,
		TimeoutMS: 10000,
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want invalid onebot_ws action for http_callback")
	}
	if got := err.Error(); !strings.Contains(got, "connections[0].action.type=onebot_ws 仅支持 ws_server/ws_reverse") {
		t.Fatalf("Validate() error = %q, want onebot_ws action validation message", got)
	}
}

func TestValidate_RejectsInvalidR2MediaStorage(t *testing.T) {
	cfg := testConfig()
	cfg.Storage.Media.Enabled = true
	cfg.Storage.Media.Backend = "r2"
	cfg.Storage.Media.R2 = MediaR2Config{}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want invalid r2 media storage error")
	}
	if got := err.Error(); !strings.Contains(got, "storage.media.r2.bucket 不能为空") {
		t.Fatalf("Validate() error = %q, want storage.media.r2.bucket validation message", got)
	}
}

func TestValidate_AllowsIndependentAIVisionProvider(t *testing.T) {
	cfg := testConfig()
	cfg.AI = testAIConfig()
	cfg.AI.Vision = AIVisionConfig{
		Enabled: true,
		Mode:    "independent",
		Provider: AIProviderConfig{
			Kind:        "openai_compatible",
			Vendor:      "openai",
			BaseURL:     "https://api.openai.com/v1",
			APIKey:      "sk-test",
			Model:       "gpt-4.1-mini",
			TimeoutMS:   30000,
			Temperature: 0.2,
		},
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_RejectsInvalidIndependentAIVisionProvider(t *testing.T) {
	cfg := testConfig()
	cfg.AI = testAIConfig()
	cfg.AI.Vision = AIVisionConfig{
		Enabled: true,
		Mode:    "independent",
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want invalid ai vision provider error")
	}
	if got := err.Error(); !strings.Contains(got, "ai.vision.provider.base_url 不能为空") {
		t.Fatalf("Validate() error = %q, want ai vision provider validation message", got)
	}
}

func TestValidate_RejectsInvalidAIThinkingConfig(t *testing.T) {
	cfg := testConfig()
	cfg.AI = testAIConfig()
	cfg.AI.Reply.ThinkingMode = "maybe"
	cfg.AI.Reply.ThinkingEffort = "medium"
	cfg.AI.Reply.ThinkingFormat = "custom"

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want invalid thinking config error")
	}
	got := err.Error()
	if !strings.Contains(got, "ai.reply.thinking_mode 只能是 auto、high 或 xhigh") {
		t.Fatalf("Validate() error = %q, want thinking_mode validation message", got)
	}
	if !strings.Contains(got, "ai.reply.thinking_format 只能是 openai 或 anthropic") {
		t.Fatalf("Validate() error = %q, want thinking_format validation message", got)
	}
	if strings.Contains(got, "thinking_effort") {
		t.Fatalf("Validate() error = %q, want medium mapped to high for compatibility", got)
	}
}

func TestValidate_AllowsAIGroupPolicies(t *testing.T) {
	cfg := testConfig()
	cfg.AI = testAIConfig()
	cfg.AI.GroupPolicies = []AIGroupPolicyConfig{
		{
			GroupID:         "10001",
			Name:            "测试群",
			ReplyEnabled:    true,
			ReplyOnAt:       false,
			ReplyOnBotName:  true,
			ReplyOnQuote:    false,
			CooldownSeconds: 6,
			MaxContextMsgs:  24,
			MaxOutputTokens: 240,
			VisionEnabled:   true,
			PromptOverride:  "这个群更偏向轻松吐槽风格。",
		},
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_RejectsDuplicateAIGroupPolicies(t *testing.T) {
	cfg := testConfig()
	cfg.AI = testAIConfig()
	cfg.AI.GroupPolicies = []AIGroupPolicyConfig{
		{GroupID: "10001", ReplyEnabled: true, MaxContextMsgs: 16, MaxOutputTokens: 160},
		{GroupID: "10001", ReplyEnabled: false, MaxContextMsgs: 16, MaxOutputTokens: 160},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want duplicate ai group policy error")
	}
	if got := err.Error(); !strings.Contains(got, "ai.group_policies[1].group_id 重复: 10001") {
		t.Fatalf("Validate() error = %q, want duplicate group policy validation message", got)
	}
}

func TestValidate_AllowsAIPrivatePersonas(t *testing.T) {
	cfg := testConfig()
	cfg.AI = testAIConfig()
	cfg.AI.PrivatePersonas = []AIPrivatePersonaConfig{
		{
			ID:           "private_gentle",
			Name:         "温柔陪伴",
			Description:  "适合私聊陪伴场景",
			SystemPrompt: "你说话温柔、克制、会照顾对方情绪。",
			StyleTags:    []string{"温柔", "陪伴"},
			Enabled:      true,
		},
		{
			ID:           "private_playful",
			Name:         "吐槽搭子",
			BotName:      "罗纸酱",
			SystemPrompt: "你可以轻松吐槽，但不要刻薄攻击。",
			Enabled:      true,
		},
	}
	cfg.AI.PrivateActivePersonaID = "private_gentle"

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_AllowsAIPrivatePersonaWithoutBotName(t *testing.T) {
	cfg := testConfig()
	cfg.AI = testAIConfig()
	cfg.AI.PrivatePersonas = []AIPrivatePersonaConfig{
		{ID: "shared_gentle", Name: "温柔陪伴", SystemPrompt: "你说话温柔、克制、会照顾对方情绪。", Enabled: true},
	}
	cfg.AI.PrivateActivePersonaID = "shared_gentle"

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_RejectsDuplicateAIPrivatePersonas(t *testing.T) {
	cfg := testConfig()
	cfg.AI = testAIConfig()
	cfg.AI.PrivatePersonas = []AIPrivatePersonaConfig{
		{ID: "private_gentle", Name: "温柔陪伴", BotName: "罗纸酱", SystemPrompt: "A"},
		{ID: "private_gentle", Name: "吐槽搭子", BotName: "罗纸酱", SystemPrompt: "B"},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want duplicate ai private persona error")
	}
	if got := err.Error(); !strings.Contains(got, "ai.private_personas[1].id 重复: private_gentle") {
		t.Fatalf("Validate() error = %q, want duplicate private persona validation message", got)
	}
}

func TestValidate_RejectsInvalidAIPrivateActivePersonaID(t *testing.T) {
	cfg := testConfig()
	cfg.AI = testAIConfig()
	cfg.AI.PrivatePersonas = []AIPrivatePersonaConfig{
		{ID: "private_gentle", Name: "温柔陪伴", BotName: "罗纸酱", SystemPrompt: "A"},
	}
	cfg.AI.PrivateActivePersonaID = "missing_persona"

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want invalid active private persona id error")
	}
	if got := err.Error(); !strings.Contains(got, "ai.private_active_persona_id 必须指向一个已存在的人格模板") {
		t.Fatalf("Validate() error = %q, want active private persona validation message", got)
	}
}

func TestValidate_AllowsDisabledAICLIWithoutWhitelist(t *testing.T) {
	cfg := testConfig()
	cfg.AI = testAIConfig()
	cfg.AI.CLI = AICLIConfig{}

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_RejectsEnabledAICLIWithoutWhitelist(t *testing.T) {
	cfg := testConfig()
	cfg.AI = testAIConfig()
	cfg.AI.CLI = AICLIConfig{Enabled: true}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want missing CLI whitelist error")
	}
	if got := err.Error(); !strings.Contains(got, "ai.cli.allowed_commands 不能为空") {
		t.Fatalf("Validate() error = %q, want CLI whitelist validation message", got)
	}
}

func TestValidate_AllowsEnabledAIMCPStdioServer(t *testing.T) {
	cfg := testConfig()
	cfg.AI = testAIConfig()
	cfg.AI.MCP = AIMCPConfig{
		Enabled: true,
		Servers: []AIMCPServerConfig{
			{
				ID:             "local_docs",
				Enabled:        true,
				Transport:      "stdio",
				Command:        "docs-mcp",
				TimeoutSeconds: 5,
				MaxOutputBytes: 65536,
			},
		},
	}

	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidate_RejectsEnabledAIMCPHTTPServerWithoutURL(t *testing.T) {
	cfg := testConfig()
	cfg.AI = testAIConfig()
	cfg.AI.MCP = AIMCPConfig{
		Enabled: true,
		Servers: []AIMCPServerConfig{
			{
				ID:             "remote_search",
				Enabled:        true,
				Transport:      "http",
				TimeoutSeconds: 5,
				MaxOutputBytes: 65536,
			},
		},
	}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want missing MCP URL error")
	}
	if got := err.Error(); !strings.Contains(got, "ai.mcp.servers[0].url 不能为空") {
		t.Fatalf("Validate() error = %q, want MCP URL validation message", got)
	}
}

func TestDecodeDraftMapNormalizesConnectionTimeout(t *testing.T) {
	cfg := testConfig()
	cfg.Connections[0].Action.TimeoutMS = 0

	raw, err := configToMap(cfg)
	if err != nil {
		t.Fatalf("configToMap() error = %v", err)
	}

	decoded, err := DecodeDraftMap(raw)
	if err != nil {
		t.Fatalf("DecodeDraftMap() error = %v", err)
	}
	if got := decoded.Connections[0].Action.TimeoutMS; got != 10000 {
		t.Fatalf("Connections[0].Action.TimeoutMS = %d, want 10000", got)
	}
}

func testAIConfig() AIConfig {
	return AIConfig{
		Enabled: true,
		Provider: AIProviderConfig{
			Kind:        "openai_compatible",
			Vendor:      "openai",
			BaseURL:     "https://api.openai.com/v1",
			APIKey:      "sk-test",
			Model:       "gpt-4.1-mini",
			TimeoutMS:   30000,
			Temperature: 0.8,
		},
		Vision: AIVisionConfig{
			Enabled: false,
			Mode:    "same_as_chat",
			Provider: AIProviderConfig{
				Kind:        "openai_compatible",
				Vendor:      "custom",
				TimeoutMS:   30000,
				Temperature: 0.8,
			},
		},
		Reply: AIReplyConfig{
			EnabledInGroup:   true,
			EnabledInPrivate: true,
			ReplyOnAt:        true,
			ReplyOnBotName:   false,
			ReplyOnQuote:     false,
			CooldownSeconds:  20,
			MaxContextMsgs:   16,
			MaxOutputTokens:  160,
		},
		Memory: AIMemoryConfig{
			Enabled:                 true,
			SessionWindow:           24,
			CandidateEnabled:        true,
			PromoteThreshold:        2,
			MaxPromptLongTerm:       4,
			MaxPromptCandidates:     3,
			ReflectionRawLimit:      768,
			ReflectionPerGroupLimit: 36,
		},
		CLI: AICLIConfig{
			Enabled:         false,
			AllowedCommands: nil,
			TimeoutSeconds:  10,
			MaxOutputBytes:  8192,
		},
		Prompt: AIPromptConfig{
			BotName:      "罗纸酱",
			SystemPrompt: "你是测试机器人",
		},
	}
}
