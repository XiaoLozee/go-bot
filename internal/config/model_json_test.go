package config

import (
	"encoding/json"
	"testing"
)

func TestAIConfigJSONSnakeCaseDecode(t *testing.T) {
	payload := []byte(`{
		"enabled": true,
		"provider": {
			"kind": "openai_compatible",
			"vendor": "openai",
			"base_url": "https://api.openai.com/v1",
			"api_key": "sk-test",
			"model": "gpt-4.1-mini",
			"timeout_ms": 30000,
			"temperature": 0.8
		},
		"reply": {
			"enabled_in_group": true,
			"enabled_in_private": true,
			"reply_on_at": true,
			"reply_on_bot_name": false,
			"reply_on_quote": true,
			"cooldown_seconds": 20,
			"max_context_messages": 16,
			"max_output_tokens": 160,
			"thinking_mode": "enabled",
			"thinking_effort": "max",
			"thinking_format": "anthropic",
			"split": {
				"enabled": true,
				"only_casual": true,
				"max_chars": 80,
				"max_parts": 3,
				"delay_ms": 650
			}
		},
		"proactive": {
			"enabled": true,
			"min_interval_seconds": 900,
			"daily_limit_per_group": 8,
			"probability": 0.08,
			"min_recent_messages": 4,
			"recent_window_seconds": 600,
			"quiet_hours": ["00:00-08:00"]
		},
		"memory": {
			"enabled": true,
			"candidate_enabled": true,
			"session_window": 24,
			"promote_threshold": 2,
			"max_prompt_long_term": 4,
			"max_prompt_candidates": 3,
			"reflection_raw_limit": 768,
			"reflection_per_group_limit": 36
		},
		"cli": {
			"enabled": true,
			"allowed_commands": ["git", "go"],
			"timeout_seconds": 15,
			"max_output_bytes": 4096
		},
		"prompt": {
			"bot_name": "罗纸酱",
			"system_prompt": "你是测试机器人"
		},
		"private_active_persona_id": "persona_main"
	}`)

	var cfg AIConfig
	if err := json.Unmarshal(payload, &cfg); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if !cfg.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if cfg.Provider.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("Provider.BaseURL = %q, want https://api.openai.com/v1", cfg.Provider.BaseURL)
	}
	if cfg.Provider.TimeoutMS != 30000 {
		t.Fatalf("Provider.TimeoutMS = %d, want 30000", cfg.Provider.TimeoutMS)
	}
	if !cfg.Reply.EnabledInGroup || !cfg.Reply.EnabledInPrivate {
		t.Fatalf("Reply = %+v, want both group/private enabled", cfg.Reply)
	}
	if cfg.Reply.MaxContextMsgs != 16 || cfg.Reply.MaxOutputTokens != 160 {
		t.Fatalf("Reply = %+v, want context=16 output=160", cfg.Reply)
	}
	if cfg.Reply.ThinkingMode != "enabled" || cfg.Reply.ThinkingEffort != "max" || cfg.Reply.ThinkingFormat != "anthropic" {
		t.Fatalf("Reply thinking = %+v, want enabled/max/anthropic", cfg.Reply)
	}
	if !cfg.Reply.Split.Enabled || !cfg.Reply.Split.OnlyCasual || cfg.Reply.Split.MaxChars != 80 || cfg.Reply.Split.MaxParts != 3 || cfg.Reply.Split.DelayMS != 650 {
		t.Fatalf("Reply.Split = %+v, want enabled casual split limits decoded", cfg.Reply.Split)
	}
	if !cfg.Reply.ReplyOnAt || cfg.Reply.ReplyOnBotName || !cfg.Reply.ReplyOnQuote {
		t.Fatalf("Reply triggers = %+v, want at=true bot_name=false quote=true", cfg.Reply)
	}
	if !cfg.Proactive.Enabled || cfg.Proactive.MinIntervalSeconds != 900 || cfg.Proactive.DailyLimitPerGroup != 8 || cfg.Proactive.Probability != 0.08 || cfg.Proactive.MinRecentMessages != 4 || cfg.Proactive.RecentWindowSeconds != 600 || len(cfg.Proactive.QuietHours) != 1 {
		t.Fatalf("Proactive = %+v, want proactive settings decoded", cfg.Proactive)
	}
	if cfg.Memory.SessionWindow != 24 || cfg.Memory.PromoteThreshold != 2 || !cfg.Memory.CandidateEnabled || cfg.Memory.MaxPromptLongTerm != 4 || cfg.Memory.MaxPromptCandidates != 3 || cfg.Memory.ReflectionRawLimit != 768 || cfg.Memory.ReflectionPerGroupLimit != 36 {
		t.Fatalf("Memory = %+v, want decoded memory policy", cfg.Memory)
	}
	if !cfg.CLI.Enabled || cfg.CLI.TimeoutSeconds != 15 || cfg.CLI.MaxOutputBytes != 4096 || len(cfg.CLI.AllowedCommands) != 2 {
		t.Fatalf("CLI = %+v, want enabled whitelist + limits decoded", cfg.CLI)
	}
	if cfg.Prompt.BotName != "罗纸酱" || cfg.Prompt.SystemPrompt != "你是测试机器人" {
		t.Fatalf("Prompt = %+v, want bot_name/system_prompt decoded", cfg.Prompt)
	}
	if cfg.PrivateActivePersonaID != "persona_main" {
		t.Fatalf("PrivateActivePersonaID = %q, want persona_main", cfg.PrivateActivePersonaID)
	}
}

func TestConnectionConfigJSONSnakeCaseDecode(t *testing.T) {
	payload := []byte(`{
		"id": "napcat-main",
		"enabled": true,
		"platform": "onebot_v11",
		"ingress": {
			"type": "ws_reverse",
			"url": "ws://127.0.0.1:3001/ws",
			"retry_interval_ms": 5000
		},
		"action": {
			"type": "napcat_http",
			"base_url": "http://127.0.0.1:3000",
			"timeout_ms": 10000,
			"access_token": "secret-token"
		}
	}`)

	var cfg ConnectionConfig
	if err := json.Unmarshal(payload, &cfg); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if cfg.Ingress.RetryIntervalMS != 5000 {
		t.Fatalf("Ingress.RetryIntervalMS = %d, want 5000", cfg.Ingress.RetryIntervalMS)
	}
	if cfg.Action.BaseURL != "http://127.0.0.1:3000" {
		t.Fatalf("Action.BaseURL = %q, want http://127.0.0.1:3000", cfg.Action.BaseURL)
	}
	if cfg.Action.TimeoutMS != 10000 {
		t.Fatalf("Action.TimeoutMS = %d, want 10000", cfg.Action.TimeoutMS)
	}
	if cfg.Action.AccessToken != "secret-token" {
		t.Fatalf("Action.AccessToken = %q, want secret-token", cfg.Action.AccessToken)
	}
}
