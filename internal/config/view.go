package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const redactedSecret = "******"

var sensitiveKeys = map[string]struct{}{
	"password":          {},
	"access_token":      {},
	"api_key":           {},
	"secret_access_key": {},
	"authorization":     {},
	"token":             {},
	"secret":            {},
}

func SanitizedMap(cfg *Config) map[string]any {
	if cfg == nil {
		return map[string]any{}
	}
	out, ok := SanitizeValue(cfg).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return out
}

func SanitizeValue(value any) any {
	raw, err := yaml.Marshal(value)
	if err != nil {
		return nil
	}

	var decoded any
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	return sanitizeRecursive(decoded)
}

func sanitizeRecursive(value any) any {
	switch current := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(current))
		for key, item := range current {
			if isSensitiveKey(key) {
				if asString, ok := item.(string); ok && asString == "" {
					out[key] = ""
				} else {
					out[key] = redactedSecret
				}
				continue
			}
			out[key] = sanitizeRecursive(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(current))
		for rawKey, item := range current {
			key := strings.TrimSpace(fmt.Sprint(rawKey))
			if isSensitiveKey(key) {
				if asString, ok := item.(string); ok && asString == "" {
					out[key] = ""
				} else {
					out[key] = redactedSecret
				}
				continue
			}
			out[key] = sanitizeRecursive(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(current))
		for _, item := range current {
			out = append(out, sanitizeRecursive(item))
		}
		return out
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if _, ok := sensitiveKeys[normalized]; ok {
		return true
	}
	return strings.Contains(normalized, "api_key") ||
		strings.Contains(normalized, "access_token") ||
		strings.Contains(normalized, "authorization") ||
		strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "secret")
}
