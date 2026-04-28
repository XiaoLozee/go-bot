package ai

import (
	"testing"

	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
)

func TestEffectiveAssistantPrompt_UsesGlobalBotNameAndPersonaSystemPrompt(t *testing.T) {
	cfg := config.AIConfig{
		Prompt: config.AIPromptConfig{
			BotName:      "系统昵称",
			SystemPrompt: "默认提示",
		},
		PrivatePersonas: []config.AIPrivatePersonaConfig{
			{ID: "shared_gentle", Name: "共享人格", BotName: "旧人格昵称", SystemPrompt: "人格提示", Enabled: true},
		},
		PrivateActivePersonaID: "shared_gentle",
	}

	got := effectiveAssistantPrompt(cfg, event.Event{ChatType: "private"})
	if got.BotName != "系统昵称" {
		t.Fatalf("BotName = %q, want global bot name", got.BotName)
	}
	if got.SystemPrompt != "人格提示" {
		t.Fatalf("SystemPrompt = %q, want active persona system prompt", got.SystemPrompt)
	}
}

func TestServicePrepareReply_GroupPolicyOverridesPersonaPrompt(t *testing.T) {
	s := &Service{
		cfg: config.AIConfig{
			Enabled: true,
			Prompt: config.AIPromptConfig{
				BotName:      "系统昵称",
				SystemPrompt: "默认提示",
			},
			PrivatePersonas: []config.AIPrivatePersonaConfig{
				{ID: "shared_gentle", Name: "共享人格", BotName: "旧人格昵称", SystemPrompt: "人格提示", Enabled: true},
			},
			PrivateActivePersonaID: "shared_gentle",
			GroupPolicies: []config.AIGroupPolicyConfig{
				{
					GroupID:         "10001",
					ReplyEnabled:    true,
					ReplyOnAt:       false,
					ReplyOnBotName:  false,
					ReplyOnQuote:    false,
					CooldownSeconds: 0,
					MaxContextMsgs:  16,
					MaxOutputTokens: 160,
					VisionEnabled:   true,
					PromptOverride:  "群补充提示",
				},
			},
		},
	}

	gotCfg, _, _, _, _, _, blocked, reason := s.prepareReply(event.Event{
		ConnectionID: "conn-1",
		ChatType:     "group",
		GroupID:      "10001",
		UserID:       "20001",
		MessageID:    "msg-1",
	}, "你好")

	if !blocked || reason != "未配置可用的 AI 服务商" {
		t.Fatalf("prepareReply() gate = blocked:%v reason:%q, want generator-missing early return", blocked, reason)
	}
	if gotCfg.Prompt.BotName != "系统昵称" {
		t.Fatalf("prepareReply() BotName = %q, want global bot name", gotCfg.Prompt.BotName)
	}
	wantPrompt := "人格提示\n\n[当前群附加设定]\n群补充提示"
	if gotCfg.Prompt.SystemPrompt != wantPrompt {
		t.Fatalf("prepareReply() SystemPrompt = %q, want %q", gotCfg.Prompt.SystemPrompt, wantPrompt)
	}
}
