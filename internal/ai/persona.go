package ai

import (
	"strings"

	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
)

func applyPrivatePersonaToAIConfig(cfg config.AIConfig, _ event.Event) config.AIConfig {
	persona, ok := resolveActivePrivatePersona(cfg)
	if !ok {
		return cfg
	}
	next := cfg
	next.Prompt = config.AIPromptConfig{
		BotName:      strings.TrimSpace(cfg.Prompt.BotName),
		SystemPrompt: strings.TrimSpace(firstNonEmpty(persona.SystemPrompt, cfg.Prompt.SystemPrompt)),
	}
	return next
}

func resolveActivePrivatePersona(cfg config.AIConfig) (config.AIPrivatePersonaConfig, bool) {
	activeID := strings.TrimSpace(cfg.PrivateActivePersonaID)
	if activeID == "" {
		return config.AIPrivatePersonaConfig{}, false
	}
	for _, item := range cfg.PrivatePersonas {
		if strings.TrimSpace(item.ID) != activeID {
			continue
		}
		if !item.Enabled {
			return config.AIPrivatePersonaConfig{}, false
		}
		return item, true
	}
	return config.AIPrivatePersonaConfig{}, false
}

func privatePersonaSnapshot(cfg config.AIConfig) (count int, activeID string, activeName string) {
	count = len(cfg.PrivatePersonas)
	activeID = strings.TrimSpace(cfg.PrivateActivePersonaID)
	if persona, ok := resolveActivePrivatePersona(cfg); ok {
		activeName = strings.TrimSpace(persona.Name)
		if activeID == "" {
			activeID = strings.TrimSpace(persona.ID)
		}
	}
	return count, activeID, activeName
}

func effectiveAssistantPrompt(cfg config.AIConfig, _ event.Event) config.AIPromptConfig {
	if persona, ok := resolveActivePrivatePersona(cfg); ok {
		return config.AIPromptConfig{
			BotName:      strings.TrimSpace(cfg.Prompt.BotName),
			SystemPrompt: strings.TrimSpace(firstNonEmpty(persona.SystemPrompt, cfg.Prompt.SystemPrompt)),
		}
	}
	return cfg.Prompt
}
