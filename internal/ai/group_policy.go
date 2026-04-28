package ai

import (
	"strings"

	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
)

func applyGroupPolicyToAIConfig(base config.AIConfig, evt event.Event) config.AIConfig {
	if strings.TrimSpace(evt.ChatType) != "group" {
		return base
	}
	policy, ok := findAIGroupPolicy(base, evt.GroupID)
	if !ok {
		return base
	}

	cfg := base
	cfg.Reply.EnabledInGroup = policy.ReplyEnabled
	cfg.Reply.ReplyOnAt = policy.ReplyOnAt
	cfg.Reply.ReplyOnBotName = policy.ReplyOnBotName
	cfg.Reply.ReplyOnQuote = policy.ReplyOnQuote
	cfg.Reply.CooldownSeconds = policy.CooldownSeconds
	if policy.MaxContextMsgs > 0 {
		cfg.Reply.MaxContextMsgs = policy.MaxContextMsgs
	}
	if policy.MaxOutputTokens > 0 {
		cfg.Reply.MaxOutputTokens = policy.MaxOutputTokens
	}
	cfg.Vision.Enabled = cfg.Vision.Enabled && policy.VisionEnabled
	if strings.TrimSpace(policy.PromptOverride) != "" {
		cfg.Prompt.SystemPrompt = mergePromptOverride(cfg.Prompt.SystemPrompt, policy.PromptOverride)
	}
	return cfg
}

func findAIGroupPolicy(aiCfg config.AIConfig, groupID string) (config.AIGroupPolicyConfig, bool) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return config.AIGroupPolicyConfig{}, false
	}
	for _, item := range aiCfg.GroupPolicies {
		if strings.TrimSpace(item.GroupID) == groupID {
			return item, true
		}
	}
	return config.AIGroupPolicyConfig{}, false
}

func mergePromptOverride(basePrompt, groupPrompt string) string {
	basePrompt = strings.TrimSpace(basePrompt)
	groupPrompt = strings.TrimSpace(groupPrompt)
	switch {
	case basePrompt == "":
		return groupPrompt
	case groupPrompt == "":
		return basePrompt
	default:
		return basePrompt + "\n\n[当前群附加设定]\n" + groupPrompt
	}
}
