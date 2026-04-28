package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func Clone(cfg *Config) (*Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("配置为空")
	}

	payload, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("克隆配置失败: %w", err)
	}

	var out Config
	if err := yaml.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("克隆配置失败: %w", err)
	}
	return &out, nil
}

func MergeSensitiveValues(base, draft *Config) (*Config, error) {
	if draft == nil {
		return nil, fmt.Errorf("配置草稿为空")
	}
	if base == nil {
		return Clone(draft)
	}

	baseMap, err := configToMap(base)
	if err != nil {
		return nil, err
	}
	draftMap, err := configToMap(draft)
	if err != nil {
		return nil, err
	}

	merged, ok := mergeSensitiveRecursive(baseMap, draftMap).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("合并敏感字段失败")
	}
	return DecodeDraftMap(merged)
}

func configToMap(cfg *Config) (map[string]any, error) {
	payload, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("转换配置失败: %w", err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("转换配置失败: %w", err)
	}
	return out, nil
}

func mergeSensitiveRecursive(base, draft any) any {
	switch draftValue := draft.(type) {
	case map[string]any:
		baseMap, _ := base.(map[string]any)
		out := make(map[string]any, len(draftValue))
		for key, item := range draftValue {
			baseItem := any(nil)
			if baseMap != nil {
				baseItem = baseMap[key]
			}
			if text, ok := item.(string); ok && text == redactedSecret && isSensitiveKey(key) {
				if baseMap != nil {
					out[key] = baseItem
					continue
				}
			}
			out[key] = mergeSensitiveRecursive(baseItem, item)
		}
		return out
	case []any:
		baseList, _ := base.([]any)
		out := make([]any, len(draftValue))
		for i, item := range draftValue {
			baseItem := any(nil)
			if i < len(baseList) {
				baseItem = baseList[i]
			}
			out[i] = mergeSensitiveRecursive(baseItem, item)
		}
		return out
	default:
		return draft
	}
}
