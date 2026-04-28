package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
)

type generator interface {
	RunTurn(ctx context.Context, messages []chatMessage, tools []toolSpec, cfg config.AIConfig) (turnResult, error)
}

type visionGenerator interface {
	Describe(ctx context.Context, prompt string, images []visionImageInput, provider config.AIProviderConfig, maxTokens int) (string, error)
}

type openAICompatibleClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

var (
	openAIToolTrailingCommaPattern = regexp.MustCompile(`,\s*([}\]])`)
	openAIToolUnquotedKeyPattern   = regexp.MustCompile(`([{\[,]\s*)([A-Za-z_][A-Za-z0-9_]*)(\s*:)`)
)

type visionImageInput struct {
	URL string
}

type openAIMessageImageURL struct {
	URL string `json:"url"`
}

type openAIMessagePart struct {
	Type     string                 `json:"type"`
	Text     string                 `json:"text,omitempty"`
	ImageURL *openAIMessageImageURL `json:"image_url,omitempty"`
}

type openAIWireMessage struct {
	Role             string               `json:"role"`
	Content          any                  `json:"content,omitempty"`
	ReasoningContent string               `json:"reasoning_content,omitempty"`
	ToolCallID       string               `json:"tool_call_id,omitempty"`
	ToolCalls        []openAIWireToolCall `json:"tool_calls,omitempty"`
}

type openAIToolDefinition struct {
	Type     string                   `json:"type"`
	Function openAIFunctionDefinition `json:"function"`
}

type openAIFunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openAIWireToolCall struct {
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function openAIWireToolFunction `json:"function"`
}

type openAIWireToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIChatRequest struct {
	Model           string                 `json:"model"`
	Messages        []openAIWireMessage    `json:"messages"`
	Tools           []openAIToolDefinition `json:"tools,omitempty"`
	Temperature     float64                `json:"temperature,omitempty"`
	MaxTokens       int                    `json:"max_tokens,omitempty"`
	Stream          bool                   `json:"stream"`
	Thinking        *openAIThinkingConfig  `json:"thinking,omitempty"`
	ReasoningEffort string                 `json:"reasoning_effort,omitempty"`
	OutputConfig    *openAIOutputConfig    `json:"output_config,omitempty"`
}

type openAIThinkingConfig struct {
	Type string `json:"type"`
}

type openAIOutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content          any                  `json:"content"`
			ReasoningContent any                  `json:"reasoning_content,omitempty"`
			ToolCalls        []openAIWireToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func newGenerator(cfg config.AIConfig) (generator, error) {
	client := newOpenAICompatibleClient(cfg.Provider)
	if client == nil {
		return nil, nil
	}
	return client, nil
}

func newVisionGenerator(cfg config.AIConfig) (visionGenerator, error) {
	provider, ok := effectiveVisionProvider(cfg)
	if !ok {
		return nil, nil
	}
	client := newOpenAICompatibleClient(provider)
	if client == nil {
		return nil, nil
	}
	return client, nil
}

func newOpenAICompatibleClient(provider config.AIProviderConfig) *openAICompatibleClient {
	baseURL := strings.TrimSpace(provider.BaseURL)
	if baseURL == "" {
		return nil
	}
	return &openAICompatibleClient{
		httpClient: &http.Client{Timeout: time.Duration(provider.TimeoutMS) * time.Millisecond},
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     strings.TrimSpace(provider.APIKey),
		model:      strings.TrimSpace(provider.Model),
	}
}

func (c *openAICompatibleClient) RunTurn(ctx context.Context, messages []chatMessage, tools []toolSpec, cfg config.AIConfig) (turnResult, error) {
	if c == nil {
		return turnResult{}, fmt.Errorf("AI 生成器未初始化")
	}
	reply := config.NormalizeAIReplyConfig(cfg.Reply)
	return c.chatCompletion(ctx, messages, tools, cfg.Provider, reply.MaxOutputTokens, &reply)
}

func (c *openAICompatibleClient) Describe(ctx context.Context, prompt string, images []visionImageInput, provider config.AIProviderConfig, maxTokens int) (string, error) {
	if c == nil {
		return "", fmt.Errorf("视觉模型未初始化")
	}
	if len(images) == 0 {
		return "", fmt.Errorf("未提供可识别的图片")
	}
	content := make([]openAIMessagePart, 0, len(images)+1)
	content = append(content, openAIMessagePart{
		Type: "text",
		Text: strings.TrimSpace(prompt),
	})
	for _, item := range images {
		if strings.TrimSpace(item.URL) == "" {
			continue
		}
		content = append(content, openAIMessagePart{
			Type:     "image_url",
			ImageURL: &openAIMessageImageURL{URL: strings.TrimSpace(item.URL)},
		})
	}
	if len(content) <= 1 {
		return "", fmt.Errorf("未提供可识别的图片")
	}
	messages := []chatMessage{
		{
			Role:    "system",
			Content: "你是一个 QQ 聊天场景里的图片理解助手。请只提取对回复有帮助的视觉信息，使用简体中文，简洁、准确，不要编造看不清的细节。",
		},
		{
			Role:    "user",
			Content: content,
		},
	}
	result, err := c.chatCompletion(ctx, messages, nil, provider, maxTokens, nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(result.Text) == "" {
		return "", fmt.Errorf("AI 返回内容为空")
	}
	return result.Text, nil
}

func (c *openAICompatibleClient) chatCompletion(ctx context.Context, messages []chatMessage, tools []toolSpec, provider config.AIProviderConfig, maxTokens int, reply *config.AIReplyConfig) (turnResult, error) {
	if c == nil {
		return turnResult{}, fmt.Errorf("AI 生成器未初始化")
	}
	payload := openAIChatRequest{
		Model:       firstNonEmpty(strings.TrimSpace(provider.Model), c.model),
		Messages:    buildOpenAIWireMessages(messages),
		Temperature: provider.Temperature,
		MaxTokens:   maxTokens,
		Stream:      false,
	}
	if len(tools) > 0 {
		payload.Tools = buildOpenAIToolDefinitions(tools)
	}
	thinkingRetryEnabled := false
	thinkingControlSent := false
	if reply != nil {
		normalizedReply := config.NormalizeAIReplyConfig(*reply)
		thinkingRetryEnabled = normalizedReply.ThinkingMode != "disabled"
		thinkingControlSent = applyOpenAIThinkingControl(&payload, normalizedReply)
	}

	result, err := c.doChatCompletion(ctx, payload)
	if err != nil {
		if thinkingControlSent && shouldRetryOpenAIWithoutThinking(err) {
			clearOpenAIThinkingControl(&payload)
			return c.doChatCompletion(ctx, payload)
		}
		return turnResult{}, err
	}
	if thinkingRetryEnabled && shouldRetryEmptyOpenAIThinkingResult(result) {
		disableOpenAIThinkingControl(&payload)
		fallback, fallbackErr := c.doChatCompletion(ctx, payload)
		if fallbackErr == nil && strings.TrimSpace(fallback.Text) != "" {
			return fallback, nil
		}
	}
	return result, nil
}

func (c *openAICompatibleClient) doChatCompletion(ctx context.Context, payload openAIChatRequest) (turnResult, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return turnResult{}, fmt.Errorf("编码 AI 请求失败: %w", err)
	}

	url := c.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return turnResult{}, fmt.Errorf("创建 AI 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return turnResult{}, fmt.Errorf("请求 AI 服务失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return turnResult{}, fmt.Errorf("读取 AI 响应失败: %w", err)
	}

	var decoded openAIChatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return turnResult{}, fmt.Errorf("解析 AI 响应失败: %w", err)
	}

	if resp.StatusCode >= 400 {
		if decoded.Error != nil && strings.TrimSpace(decoded.Error.Message) != "" {
			return turnResult{}, fmt.Errorf("AI 服务返回错误: %s", decoded.Error.Message)
		}
		return turnResult{}, fmt.Errorf("AI 服务返回错误: HTTP %d", resp.StatusCode)
	}
	if decoded.Error != nil && strings.TrimSpace(decoded.Error.Message) != "" {
		return turnResult{}, fmt.Errorf("AI 服务返回错误: %s", decoded.Error.Message)
	}
	if len(decoded.Choices) == 0 {
		return turnResult{}, fmt.Errorf("AI 服务未返回候选结果")
	}

	choice := decoded.Choices[0]
	result := turnResult{
		Text:             strings.TrimSpace(extractOpenAIContent(choice.Message.Content)),
		ReasoningContent: strings.TrimSpace(extractOpenAIContent(choice.Message.ReasoningContent)),
		FinishReason:     strings.TrimSpace(choice.FinishReason),
	}
	if len(choice.Message.ToolCalls) > 0 {
		calls, err := parseOpenAIToolCalls(choice.Message.ToolCalls)
		if err != nil {
			return turnResult{}, err
		}
		result.ToolCalls = calls
	}
	return result, nil
}

func buildOpenAIWireMessages(messages []chatMessage) []openAIWireMessage {
	out := make([]openAIWireMessage, 0, len(messages))
	for _, item := range messages {
		content := item.Content
		if item.Role == "assistant" && len(item.ToolCalls) > 0 && content == nil {
			content = ""
		}
		out = append(out, openAIWireMessage{
			Role:             item.Role,
			Content:          content,
			ReasoningContent: strings.TrimSpace(item.ReasoningContent),
			ToolCallID:       item.ToolCallID,
			ToolCalls:        buildOpenAIWireToolCalls(item.ToolCalls),
		})
	}
	return out
}

func buildOpenAIToolDefinitions(tools []toolSpec) []openAIToolDefinition {
	out := make([]openAIToolDefinition, 0, len(tools))
	for _, tool := range tools {
		out = append(out, openAIToolDefinition{
			Type: "function",
			Function: openAIFunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}
	return out
}

func applyOpenAIThinkingControl(payload *openAIChatRequest, reply config.AIReplyConfig) bool {
	if payload == nil {
		return false
	}
	reply = config.NormalizeAIReplyConfig(reply)
	switch reply.ThinkingMode {
	case "auto":
		return false
	case "disabled":
		payload.Thinking = &openAIThinkingConfig{Type: "disabled"}
		return true
	}
	payload.Thinking = &openAIThinkingConfig{Type: "enabled"}
	switch reply.ThinkingFormat {
	case "anthropic":
		payload.OutputConfig = &openAIOutputConfig{Effort: reply.ThinkingEffort}
	default:
		payload.ReasoningEffort = reply.ThinkingEffort
	}
	return true
}

func clearOpenAIThinkingControl(payload *openAIChatRequest) {
	if payload == nil {
		return
	}
	payload.Thinking = nil
	payload.ReasoningEffort = ""
	payload.OutputConfig = nil
}

func disableOpenAIThinkingControl(payload *openAIChatRequest) {
	if payload == nil {
		return
	}
	payload.Thinking = &openAIThinkingConfig{Type: "disabled"}
	payload.ReasoningEffort = ""
	payload.OutputConfig = nil
}

func shouldRetryOpenAIWithoutThinking(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	if !strings.Contains(text, "thinking") && !strings.Contains(text, "reasoning_effort") && !strings.Contains(text, "output_config") {
		return false
	}
	for _, marker := range []string{
		"unsupported",
		"not supported",
		"unknown",
		"unrecognized",
		"invalid",
		"extra_forbidden",
		"unexpected",
		"bad request",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func shouldRetryEmptyOpenAIThinkingResult(result turnResult) bool {
	if strings.TrimSpace(result.Text) != "" || len(result.ToolCalls) > 0 {
		return false
	}
	finishReason := strings.TrimSpace(strings.ToLower(result.FinishReason))
	return strings.TrimSpace(result.ReasoningContent) != "" || finishReason == "length" || finishReason == "max_tokens"
}

func buildOpenAIWireToolCalls(calls []toolCall) []openAIWireToolCall {
	out := make([]openAIWireToolCall, 0, len(calls))
	for _, call := range calls {
		arguments := strings.TrimSpace(string(call.Arguments))
		if arguments == "" {
			arguments = "{}"
		}
		out = append(out, openAIWireToolCall{
			ID:   call.ID,
			Type: "function",
			Function: openAIWireToolFunction{
				Name:      call.Name,
				Arguments: arguments,
			},
		})
	}
	return out
}

func parseOpenAIToolCalls(raw []openAIWireToolCall) ([]toolCall, error) {
	out := make([]toolCall, 0, len(raw))
	for _, item := range raw {
		name := strings.TrimSpace(item.Function.Name)
		if name == "" {
			return nil, fmt.Errorf("AI 返回的工具名称为空")
		}
		arguments := strings.TrimSpace(item.Function.Arguments)
		normalizedArguments, err := normalizeOpenAIToolArgumentsForTool(name, arguments)
		if err != nil {
			return nil, fmt.Errorf("AI 工具 %s 返回了非法参数 JSON: %w", name, err)
		}
		out = append(out, toolCall{
			ID:        strings.TrimSpace(item.ID),
			Name:      name,
			Arguments: normalizedArguments,
		})
	}
	return out, nil
}

func normalizeOpenAIToolArgumentsForTool(name string, value string) (json.RawMessage, error) {
	arguments, err := normalizeOpenAIToolArguments(value)
	if err == nil {
		return coerceSingleStringToolArguments(name, arguments), nil
	}
	if name == "generate_image" {
		if fallback, ok := fallbackGenerateImageArguments(value); ok {
			return fallback, nil
		}
	}
	if fallback, ok := fallbackSingleStringToolArguments(name, value); ok {
		return fallback, nil
	}
	return nil, err
}

func coerceSingleStringToolArguments(name string, arguments json.RawMessage) json.RawMessage {
	field := singleStringToolArgumentField(name)
	if field == "" {
		return arguments
	}
	decoder := json.NewDecoder(strings.NewReader(string(arguments)))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return arguments
	}
	value, exists := payload[field]
	if !exists && field == "message_id" {
		value, exists = payload["id"]
		if exists {
			delete(payload, "id")
		}
	}
	if !exists {
		return arguments
	}
	text := stringifyToolArgumentValue(value)
	if text == "" {
		return arguments
	}
	payload[field] = text
	body, err := json.Marshal(payload)
	if err != nil {
		return arguments
	}
	return json.RawMessage(body)
}

func stringifyToolArgumentValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%.0f", typed))
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func normalizeOpenAIToolArguments(value string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return json.RawMessage("{}"), nil
	}
	for _, candidate := range openAIToolArgumentCandidates(trimmed) {
		if json.Valid([]byte(candidate)) {
			return json.RawMessage(candidate), nil
		}
	}
	return nil, fmt.Errorf("无法修复工具参数")
}

func fallbackGenerateImageArguments(value string) (json.RawMessage, bool) {
	prompt := strings.TrimSpace(stripMarkdownFence(normalizeOpenAIToolArgumentText(value)))
	prompt = strings.TrimSpace(strings.Trim(prompt, "{}"))
	if fieldValue := extractLooseFieldValue(prompt, "prompt"); fieldValue != "" {
		prompt = fieldValue
	}
	prompt = strings.TrimSpace(strings.Trim(prompt, "`\"'"))
	if prompt == "" {
		return nil, false
	}
	payload, err := json.Marshal(map[string]any{"prompt": prompt})
	if err != nil {
		return nil, false
	}
	return json.RawMessage(payload), true
}

func fallbackSingleStringToolArguments(name string, value string) (json.RawMessage, bool) {
	field := singleStringToolArgumentField(name)
	if field == "" {
		return nil, false
	}
	trimmed := strings.TrimSpace(stripMarkdownFence(normalizeOpenAIToolArgumentText(value)))
	trimmed = strings.TrimSpace(strings.Trim(trimmed, "{}"))
	if fieldValue := extractLooseFieldValue(trimmed, field); fieldValue != "" {
		trimmed = fieldValue
	} else if field == "message_id" {
		if fieldValue := extractLooseFieldValue(trimmed, "id"); fieldValue != "" {
			trimmed = fieldValue
		}
	}
	trimmed = strings.TrimSpace(strings.Trim(trimmed, "`\"'"))
	if trimmed == "" {
		return nil, false
	}
	payload, err := json.Marshal(map[string]any{field: trimmed})
	if err != nil {
		return nil, false
	}
	return json.RawMessage(payload), true
}

func singleStringToolArgumentField(name string) string {
	switch strings.TrimSpace(name) {
	case "get_message_detail":
		return "message_id"
	case "get_group_member_info":
		return "user_id"
	case "search_message_history":
		return "query"
	case "run_cli_command":
		return "command"
	case "send_message_current":
		return "text"
	case "send_image_current", "send_video_current", "send_file_current":
		return "file"
	default:
		return ""
	}
}

func extractLooseFieldValue(value string, key string) string {
	for _, variant := range []string{`"` + key + `"`, `'` + key + `'`, key} {
		index := strings.Index(value, variant)
		if index < 0 {
			continue
		}
		rest := value[index+len(variant):]
		colon := strings.IndexByte(rest, ':')
		if colon < 0 {
			continue
		}
		return readLooseValue(rest[colon+1:])
	}
	return ""
}

func readLooseValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if quote := trimmed[0]; quote == '"' || quote == '\'' {
		escaped := false
		for index := 1; index < len(trimmed); index++ {
			current := trimmed[index]
			if escaped {
				escaped = false
				continue
			}
			if current == '\\' {
				escaped = true
				continue
			}
			if current == quote {
				return strings.TrimSpace(trimmed[1:index])
			}
		}
		return strings.TrimSpace(trimmed[1:])
	}
	end := len(trimmed)
	for _, delimiter := range []string{",", "\n", "\r", "}"} {
		if index := strings.Index(trimmed, delimiter); index >= 0 && index < end {
			end = index
		}
	}
	return strings.TrimSpace(trimmed[:end])
}

func openAIToolArgumentCandidates(value string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 8)
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		if _, exists := seen[candidate]; exists {
			return
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}

	normalized := normalizeOpenAIToolArgumentText(stripMarkdownFence(value))
	add(value)
	add(normalized)
	add(extractFirstJSONObject(normalized))

	base := append([]string(nil), out...)
	for _, candidate := range base {
		add(repairLooseJSONObject(candidate))
	}
	return out
}

func normalizeOpenAIToolArgumentText(value string) string {
	replacer := strings.NewReplacer(
		"“", `"`,
		"”", `"`,
		"‘", `'`,
		"’", `'`,
	)
	return replacer.Replace(strings.TrimSpace(value))
}

func stripMarkdownFence(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	body := strings.TrimPrefix(trimmed, "```")
	if index := strings.IndexByte(body, '\n'); index >= 0 {
		header := strings.TrimSpace(body[:index])
		if !strings.ContainsAny(header, "{[") {
			body = body[index+1:]
		}
	}
	body = strings.TrimSpace(body)
	if strings.HasSuffix(body, "```") {
		body = strings.TrimSpace(strings.TrimSuffix(body, "```"))
	}
	return body
}

func extractFirstJSONObject(value string) string {
	start := strings.IndexByte(value, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	var quote byte
	escaped := false
	for index := start; index < len(value); index++ {
		current := value[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if current == '\\' {
				escaped = true
				continue
			}
			if current == quote {
				inString = false
			}
			continue
		}
		switch current {
		case '"', '\'':
			inString = true
			quote = current
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(value[start : index+1])
			}
		}
	}
	return ""
}

func repairLooseJSONObject(value string) string {
	repaired := normalizeOpenAIToolArgumentText(stripMarkdownFence(value))
	if strings.Contains(repaired, "'") && !strings.Contains(repaired, `"`) {
		repaired = strings.ReplaceAll(repaired, "'", `"`)
	}
	if !strings.HasPrefix(repaired, "{") && !strings.HasPrefix(repaired, "[") && strings.Contains(repaired, ":") {
		repaired = "{" + repaired + "}"
	}
	repaired = openAIToolUnquotedKeyPattern.ReplaceAllString(repaired, `${1}"${2}"${3}`)
	repaired = openAIToolTrailingCommaPattern.ReplaceAllString(repaired, `$1`)
	return strings.TrimSpace(repaired)
}

func effectiveVisionProvider(cfg config.AIConfig) (config.AIProviderConfig, bool) {
	if !cfg.Vision.Enabled {
		return config.AIProviderConfig{}, false
	}
	switch normalizeVisionMode(cfg.Vision.Mode) {
	case "same_as_chat":
		return cfg.Provider, true
	case "independent":
		return cfg.Vision.Provider, true
	default:
		return config.AIProviderConfig{}, false
	}
}

func effectiveVisionProviderLabel(cfg config.AIConfig) string {
	provider, ok := effectiveVisionProvider(cfg)
	if !ok {
		return ""
	}
	return strings.TrimSpace(provider.Vendor)
}

func effectiveVisionModel(cfg config.AIConfig) string {
	provider, ok := effectiveVisionProvider(cfg)
	if !ok {
		return ""
	}
	return strings.TrimSpace(provider.Model)
}

func normalizeVisionMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "", "same_as_chat":
		return "same_as_chat"
	case "independent":
		return "independent"
	default:
		return "disabled"
	}
}

func extractOpenAIContent(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := part["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
				continue
			}
			if text, ok := part["content"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
