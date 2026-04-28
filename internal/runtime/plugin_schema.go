package runtime

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

func optionalPluginSchema(manifest sdk.Manifest) map[string]any {
	schema, _, errText := loadPluginSchema(manifest)
	if strings.TrimSpace(errText) != "" {
		return nil
	}
	return schema
}

func mergePluginConfigWithSchemaDefaults(configValue map[string]any, schema map[string]any) map[string]any {
	merged := cloneConfigMap(configValue)
	for key, value := range pluginSchemaDefaultConfig(schema) {
		if _, exists := merged[key]; exists {
			continue
		}
		merged[key] = cloneSchemaValue(value)
	}
	return merged
}

func validatePluginConfigWithSchema(manifest sdk.Manifest, cfg config.PluginConfig) error {
	schema := optionalPluginSchema(manifest)
	if schema == nil {
		return nil
	}

	properties := pluginSchemaProperties(schema)
	if len(properties) == 0 {
		return nil
	}

	var errs []string
	if cfg.Enabled {
		for key := range pluginSchemaRequiredSet(schema) {
			if value, exists := cfg.Config[key]; exists && pluginConfigValuePresent(value) {
				continue
			}
			errs = append(errs, pluginSchemaFieldLabel(key, properties[key])+" 不能为空")
		}
	}

	for key, fieldSchema := range properties {
		value, exists := cfg.Config[key]
		if !exists {
			continue
		}
		if msg := validatePluginConfigValue(key, value, fieldSchema); msg != "" {
			errs = append(errs, msg)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("插件配置校验失败:\n- %s", strings.Join(errs, "\n- "))
	}
	return nil
}

func pluginSchemaDefaultConfig(schema map[string]any) map[string]any {
	properties := pluginSchemaProperties(schema)
	out := make(map[string]any, len(properties))
	for key, fieldSchema := range properties {
		if value, exists := fieldSchema["default"]; exists {
			out[key] = cloneSchemaValue(value)
		}
	}
	return out
}

func pluginSchemaRequiredSet(schema map[string]any) map[string]struct{} {
	out := map[string]struct{}{}
	if schema == nil {
		return out
	}

	switch items := schema["required"].(type) {
	case []any:
		for _, item := range items {
			key := strings.TrimSpace(fmt.Sprint(item))
			if key != "" {
				out[key] = struct{}{}
			}
		}
	case []string:
		for _, item := range items {
			key := strings.TrimSpace(item)
			if key != "" {
				out[key] = struct{}{}
			}
		}
	}
	return out
}

func pluginSchemaProperties(schema map[string]any) map[string]map[string]any {
	if schema == nil {
		return map[string]map[string]any{}
	}
	if typeName := strings.TrimSpace(fmt.Sprint(schema["type"])); typeName != "" && typeName != "object" {
		return map[string]map[string]any{}
	}
	rawProps, _ := schema["properties"].(map[string]any)
	out := make(map[string]map[string]any, len(rawProps))
	for key, value := range rawProps {
		fieldSchema, ok := value.(map[string]any)
		if !ok {
			continue
		}
		out[key] = fieldSchema
	}
	return out
}

func pluginSchemaFieldLabel(key string, fieldSchema map[string]any) string {
	if title := strings.TrimSpace(fmt.Sprint(fieldSchema["title"])); title != "" && title != "<nil>" {
		return title
	}
	return key
}

func validatePluginConfigValue(key string, value any, fieldSchema map[string]any) string {
	label := pluginSchemaFieldLabel(key, fieldSchema)
	typeName := strings.ToLower(strings.TrimSpace(fmt.Sprint(fieldSchema["type"])))

	if enumItems, ok := fieldSchema["enum"].([]any); ok && len(enumItems) > 0 {
		matched := false
		for _, item := range enumItems {
			if schemaValuesEqual(value, item) {
				matched = true
				break
			}
		}
		if !matched {
			return label + " 不在允许的取值范围内"
		}
	}

	switch typeName {
	case "", "string":
		if _, ok := value.(string); !ok {
			return label + " 必须是字符串"
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return label + " 必须是布尔值"
		}
	case "integer":
		number, ok := schemaNumberValue(value)
		if !ok || math.Trunc(number) != number {
			return label + " 必须是整数"
		}
		if minimum, ok := schemaNumberValue(fieldSchema["minimum"]); ok && number < minimum {
			return label + fmt.Sprintf(" 不能小于 %v", minimum)
		}
		if maximum, ok := schemaNumberValue(fieldSchema["maximum"]); ok && number > maximum {
			return label + fmt.Sprintf(" 不能大于 %v", maximum)
		}
	case "number":
		number, ok := schemaNumberValue(value)
		if !ok {
			return label + " 必须是数字"
		}
		if minimum, ok := schemaNumberValue(fieldSchema["minimum"]); ok && number < minimum {
			return label + fmt.Sprintf(" 不能小于 %v", minimum)
		}
		if maximum, ok := schemaNumberValue(fieldSchema["maximum"]); ok && number > maximum {
			return label + fmt.Sprintf(" 不能大于 %v", maximum)
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return label + " 必须是对象"
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return label + " 必须是数组"
		}
	}

	return ""
}

func pluginConfigValuePresent(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func schemaNumberValue(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int8:
		return float64(number), true
	case int16:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint8:
		return float64(number), true
	case uint16:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	default:
		return 0, false
	}
}

func schemaValuesEqual(left, right any) bool {
	if leftNumber, ok := schemaNumberValue(left); ok {
		if rightNumber, ok := schemaNumberValue(right); ok {
			return leftNumber == rightNumber
		}
	}
	return reflect.DeepEqual(left, right)
}

func cloneSchemaValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = cloneSchemaValue(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneSchemaValue(item))
		}
		return out
	default:
		return value
	}
}
