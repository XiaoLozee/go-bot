package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

const (
	maxToolLoopHops      = 4
	builtinToolNamespace = "builtin.core"
	defaultToolNamespace = "default"
	maxToolGuidanceItems = 48
	maxToolGuidanceDesc  = 180
)

type turnResult struct {
	Text             string
	ReasoningContent string
	ToolCalls        []toolCall
	FinishReason     string
	ToolOutboundSent bool
}

type toolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type toolSpec struct {
	Name        string
	Description string
	InputSchema map[string]any
}

type toolDefinition struct {
	Spec    toolSpec
	Handler func(ctx context.Context, exec *toolExecutionContext, args json.RawMessage) (any, error)
}

type toolForwardNodeInput struct {
	UserID    string `json:"user_id"`
	Nickname  string `json:"nickname"`
	Text      string `json:"text"`
	ImageFile string `json:"image_file"`
	VideoFile string `json:"video_file"`
	File      string `json:"file"`
	FileName  string `json:"file_name"`
}

type scheduledOutboundMessage struct {
	Text           string
	Reply          bool
	Segments       []message.Segment
	ForwardNodes   []message.ForwardNode
	ForwardOptions message.ForwardOptions
}

type toolExecutionContext struct {
	service   *Service
	event     event.Event
	target    message.Target
	replyTo   string
	scheduled *scheduledOutboundMessage
}

type strangerInfoGetter interface {
	GetStrangerInfo(ctx context.Context, connectionID, userID string) (*sdk.UserInfo, error)
}

type groupInfoGetter interface {
	GetGroupInfo(ctx context.Context, connectionID, groupID string) (*sdk.GroupInfo, error)
}

type groupMemberInfoGetter interface {
	GetGroupMemberInfo(ctx context.Context, connectionID, groupID, userID string) (*sdk.GroupMemberInfo, error)
}

type groupMemberListGetter interface {
	GetGroupMemberList(ctx context.Context, connectionID, groupID string) ([]sdk.GroupMemberInfo, error)
}

type loginInfoGetter interface {
	GetLoginInfo(ctx context.Context, connectionID string) (*sdk.LoginInfo, error)
}

type statusGetter interface {
	GetStatus(ctx context.Context, connectionID string) (*sdk.BotStatus, error)
}

func (e *toolExecutionContext) Event() event.Event {
	return e.event
}

func (e *toolExecutionContext) Target() message.Target {
	return e.target
}

func (e *toolExecutionContext) ReplyTo() string {
	return e.replyTo
}

func (e *toolExecutionContext) ScheduleCurrentSend(text string, reply bool) error {
	_, err := e.scheduleCurrentSend(text, reply)
	return err
}

func (e *toolExecutionContext) scheduleCurrentSend(text string, reply bool) (scheduledOutboundMessage, error) {
	guarded := guardrailText(text)
	if guarded == "" {
		return scheduledOutboundMessage{}, fmt.Errorf("发送内容为空")
	}
	if e.scheduled != nil {
		return scheduledOutboundMessage{}, fmt.Errorf("当前回合已经调度过发送消息")
	}
	msg := scheduledOutboundMessage{
		Text:  guarded,
		Reply: reply && e.event.ChatType == "group" && strings.TrimSpace(e.replyTo) != "",
	}
	e.scheduled = &msg
	return msg, nil
}

func (e *toolExecutionContext) scheduleCurrentSegments(segs []message.Segment, reply bool, fallbackText string) (scheduledOutboundMessage, error) {
	cleaned := normalizeToolSegments(segs)
	if len(cleaned) == 0 {
		return scheduledOutboundMessage{}, fmt.Errorf("发送消息段为空")
	}
	if e.scheduled != nil {
		return scheduledOutboundMessage{}, fmt.Errorf("当前回合已经调度过发送消息")
	}
	if reply && e.event.ChatType == "group" && strings.TrimSpace(e.replyTo) != "" {
		cleaned = append([]message.Segment{message.Reply(e.replyTo)}, cleaned...)
	} else {
		reply = false
	}
	text := guardrailText(fallbackText)
	if text == "" {
		text = summarizeToolSegments(cleaned)
	}
	msg := scheduledOutboundMessage{
		Text:     text,
		Reply:    reply,
		Segments: cleaned,
	}
	e.scheduled = &msg
	return msg, nil
}

func (e *toolExecutionContext) scheduleCurrentForward(nodes []message.ForwardNode, opts message.ForwardOptions, fallbackText string) (scheduledOutboundMessage, error) {
	if e.event.ChatType != "group" || strings.TrimSpace(e.target.GroupID) == "" {
		return scheduledOutboundMessage{}, fmt.Errorf("合并转发当前仅支持群聊")
	}
	cleaned, err := normalizeToolForwardNodes(nodes)
	if err != nil {
		return scheduledOutboundMessage{}, err
	}
	if len(cleaned) == 0 {
		return scheduledOutboundMessage{}, fmt.Errorf("合并转发节点为空")
	}
	if e.scheduled != nil {
		return scheduledOutboundMessage{}, fmt.Errorf("当前回合已经调度过发送消息")
	}
	text := guardrailText(fallbackText)
	if text == "" {
		text = summarizeToolForwardNodes(cleaned)
	}
	msg := scheduledOutboundMessage{
		Text:           text,
		ForwardNodes:   cleaned,
		ForwardOptions: normalizeToolForwardOptions(opts),
	}
	e.scheduled = &msg
	return msg, nil
}

func (s *Service) RegisterTools(namespace string, tools []sdk.AIToolDefinition) error {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return fmt.Errorf("AI tool namespace 不能为空")
	}
	normalized, err := normalizeAIToolDefinitions(tools)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	nameOwner := make(map[string]string)
	for _, providerID := range s.toolProviderOrder {
		if providerID == namespace {
			continue
		}
		for _, item := range s.toolProviders[providerID] {
			nameOwner[item.Name] = providerID
		}
	}
	for _, item := range normalized {
		if owner, exists := nameOwner[item.Name]; exists {
			return fmt.Errorf("AI tool %s 已被 %s 注册", item.Name, owner)
		}
	}

	if _, exists := s.toolProviders[namespace]; !exists {
		s.toolProviderOrder = append(s.toolProviderOrder, namespace)
	}
	s.toolProviders[namespace] = normalized
	return nil
}

func (s *Service) UnregisterTools(namespace string) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.toolProviders, namespace)
	for idx, item := range s.toolProviderOrder {
		if item != namespace {
			continue
		}
		s.toolProviderOrder = append(s.toolProviderOrder[:idx], s.toolProviderOrder[idx+1:]...)
		break
	}
}

func (s *Service) unregisterToolsByPrefix(prefix string) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	nextOrder := s.toolProviderOrder[:0]
	for _, providerID := range s.toolProviderOrder {
		if strings.HasPrefix(providerID, prefix) {
			delete(s.toolProviders, providerID)
			continue
		}
		nextOrder = append(nextOrder, providerID)
	}
	s.toolProviderOrder = nextOrder
}

func (s *Service) registerBuiltinTools() error {
	return s.RegisterTools(builtinToolNamespace, s.coreToolDefinitions())
}

func (s *Service) applyMCPToolDefinitions(definitions map[string][]sdk.AIToolDefinition) error {
	s.unregisterToolsByPrefix(mcpToolProviderPrefix)
	for providerID, tools := range definitions {
		if len(tools) == 0 {
			continue
		}
		if err := s.RegisterTools(providerID, tools); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SkillCatalog() []SkillView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]SkillView, 0, len(s.toolProviderOrder))
	for _, providerID := range s.toolProviderOrder {
		defs := s.toolProviders[providerID]
		if len(defs) == 0 {
			continue
		}
		out = append(out, buildSkillView(providerID, defs))
	}
	return out
}

func (s *Service) buildToolDefinitions(evt event.Event) []toolDefinition {
	s.mu.RLock()
	defer s.mu.RUnlock()

	defs := make([]toolDefinition, 0, 8)
	for _, namespace := range s.toolProviderOrder {
		for _, item := range s.toolProviders[namespace] {
			if !s.toolEnabledLocked(item.Name) {
				continue
			}
			if item.Available != nil && !item.Available(evt) {
				continue
			}
			registered := item
			defs = append(defs, toolDefinition{
				Spec: toolSpec{
					Name:        registered.Name,
					Description: registered.Description,
					InputSchema: cloneToolInputSchema(registered.InputSchema),
				},
				Handler: func(ctx context.Context, exec *toolExecutionContext, args json.RawMessage) (any, error) {
					return registered.Handle(ctx, exec, args)
				},
			})
		}
	}
	return defs
}

func (s *Service) toolEnabledLocked(name string) bool {
	switch strings.TrimSpace(name) {
	case "run_cli_command":
		return s.cfg.CLI.Enabled
	default:
		return true
	}
}

func (s *Service) coreToolDefinitions() []sdk.AIToolDefinition {
	_, hasStrangerInfo := s.messenger.(strangerInfoGetter)
	_, hasGroupInfo := s.messenger.(groupInfoGetter)
	_, hasGroupMemberInfo := s.messenger.(groupMemberInfoGetter)
	_, hasGroupMemberList := s.messenger.(groupMemberListGetter)
	_, hasLoginInfo := s.messenger.(loginInfoGetter)
	_, hasStatus := s.messenger.(statusGetter)

	defs := []sdk.AIToolDefinition{
		{
			Name:        "send_message_current",
			Description: "Send the final visible message to the current conversation only. After calling this tool, do not repeat the same full message in the final answer.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{
						"type":        "string",
						"description": "Message text to send to the current conversation.",
					},
					"reply": map[string]any{
						"type":        "boolean",
						"description": "Reply to the triggering message when the current conversation is a group chat.",
					},
				},
				"required":             []string{"text"},
				"additionalProperties": false,
			},
			Handle: func(_ context.Context, toolCtx sdk.AIToolContext, args json.RawMessage) (any, error) {
				var payload struct {
					Text  string `json:"text"`
					Reply bool   `json:"reply"`
				}
				if err := decodeToolArgs(args, &payload); err != nil {
					return nil, err
				}
				if err := toolCtx.ScheduleCurrentSend(payload.Text, payload.Reply); err != nil {
					return nil, err
				}
				return map[string]any{
					"scheduled": true,
					"reply":     payload.Reply,
					"preview":   guardrailText(payload.Text),
				}, nil
			},
		},
		{
			Name:        "send_image_current",
			Description: "Send one image or sticker to the current conversation. Use it only when an image reaction is clearly suitable; prefer URLs or data:image/base64 references selected by a trusted library.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file": map[string]any{
						"type":        "string",
						"description": "Image URL, data:image URI, or base64:// payload accepted by the current OneBot connection.",
					},
					"caption": map[string]any{
						"type":        "string",
						"description": "Optional short text sent with the image.",
					},
					"reply": map[string]any{
						"type":        "boolean",
						"description": "Reply to the triggering message when the current conversation is a group chat.",
					},
				},
				"required":             []string{"file"},
				"additionalProperties": false,
			},
			Handle: func(_ context.Context, toolCtx sdk.AIToolContext, args json.RawMessage) (any, error) {
				var payload struct {
					File    string `json:"file"`
					Caption string `json:"caption"`
					Reply   bool   `json:"reply"`
				}
				if err := decodeToolArgs(args, &payload); err != nil {
					return nil, err
				}
				imageRef, err := normalizeToolMediaRef("image", payload.File)
				if err != nil {
					return nil, err
				}
				nativeCtx, ok := toolCtx.(*toolExecutionContext)
				if !ok {
					return nil, fmt.Errorf("当前运行时不支持调度图片消息")
				}
				caption := guardrailText(payload.Caption)
				segs := []message.Segment{message.Image(imageRef)}
				if caption != "" {
					segs = append(segs, message.Text(caption))
				}
				msg, err := nativeCtx.scheduleCurrentSegments(segs, payload.Reply, firstNonEmpty(caption, "[图片]"))
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"scheduled": true,
					"reply":     msg.Reply,
					"image":     imageRef,
					"caption":   caption,
				}, nil
			},
		},
		{
			Name:        "send_video_current",
			Description: "Send one video to the current conversation. Use it only when a video response is explicitly useful and the file reference comes from a trusted source.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file": map[string]any{
						"type":        "string",
						"description": "Video URL, data:video URI, or base64:// payload accepted by the current OneBot connection.",
					},
					"caption": map[string]any{
						"type":        "string",
						"description": "Optional short text sent with the video.",
					},
					"reply": map[string]any{
						"type":        "boolean",
						"description": "Reply to the triggering message when the current conversation is a group chat.",
					},
				},
				"required":             []string{"file"},
				"additionalProperties": false,
			},
			Handle: func(_ context.Context, toolCtx sdk.AIToolContext, args json.RawMessage) (any, error) {
				var payload struct {
					File    string `json:"file"`
					Caption string `json:"caption"`
					Reply   bool   `json:"reply"`
				}
				if err := decodeToolArgs(args, &payload); err != nil {
					return nil, err
				}
				videoRef, err := normalizeToolMediaRef("video", payload.File)
				if err != nil {
					return nil, err
				}
				nativeCtx, ok := toolCtx.(*toolExecutionContext)
				if !ok {
					return nil, fmt.Errorf("当前运行时不支持调度视频消息")
				}
				caption := guardrailText(payload.Caption)
				segs := []message.Segment{message.Video(videoRef)}
				if caption != "" {
					segs = append(segs, message.Text(caption))
				}
				msg, err := nativeCtx.scheduleCurrentSegments(segs, payload.Reply, firstNonEmpty(caption, "[视频]"))
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"scheduled": true,
					"reply":     msg.Reply,
					"video":     videoRef,
					"caption":   caption,
				}, nil
			},
		},
		{
			Name:        "send_file_current",
			Description: "Send one file to the current conversation. Use it only when the user asked for a file or when a trusted plugin produced a downloadable file.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"file": map[string]any{
						"type":        "string",
						"description": "File URL or base64:// payload accepted by the current OneBot connection. Local filesystem paths are not accepted.",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Optional display file name.",
					},
					"caption": map[string]any{
						"type":        "string",
						"description": "Optional short text sent with the file.",
					},
					"reply": map[string]any{
						"type":        "boolean",
						"description": "Reply to the triggering message when the current conversation is a group chat.",
					},
				},
				"required":             []string{"file"},
				"additionalProperties": false,
			},
			Handle: func(_ context.Context, toolCtx sdk.AIToolContext, args json.RawMessage) (any, error) {
				var payload struct {
					File    string `json:"file"`
					Name    string `json:"name"`
					Caption string `json:"caption"`
					Reply   bool   `json:"reply"`
				}
				if err := decodeToolArgs(args, &payload); err != nil {
					return nil, err
				}
				fileRef, err := normalizeToolMediaRef("file", payload.File)
				if err != nil {
					return nil, err
				}
				nativeCtx, ok := toolCtx.(*toolExecutionContext)
				if !ok {
					return nil, fmt.Errorf("当前运行时不支持调度文件消息")
				}
				caption := guardrailText(payload.Caption)
				name := strings.TrimSpace(payload.Name)
				segs := []message.Segment{message.File(fileRef, name)}
				if caption != "" {
					segs = append([]message.Segment{message.Text(caption)}, segs...)
				}
				msg, err := nativeCtx.scheduleCurrentSegments(segs, payload.Reply, firstNonEmpty(caption, "[文件]"))
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"scheduled": true,
					"reply":     msg.Reply,
					"file":      fileRef,
					"name":      name,
					"caption":   caption,
				}, nil
			},
		},
		{
			Name:        "send_group_forward_current",
			Description: "Send a merged forward message to the current group chat. Use it for multi-part summaries, quoted collections, or structured long content; only available in group chats.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"nodes": map[string]any{
						"type":        "array",
						"description": "Forward nodes to send. Each node should contain short text and optional media references.",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"user_id": map[string]any{
									"type":        "string",
									"description": "Display user ID for this node. Leave empty to use the bot account ID when available.",
								},
								"nickname": map[string]any{
									"type":        "string",
									"description": "Display nickname for this node.",
								},
								"text": map[string]any{
									"type":        "string",
									"description": "Text content for this node.",
								},
								"image_file": map[string]any{
									"type":        "string",
									"description": "Optional image URL, data:image URI, or base64:// payload.",
								},
								"video_file": map[string]any{
									"type":        "string",
									"description": "Optional video URL, data:video URI, or base64:// payload.",
								},
								"file": map[string]any{
									"type":        "string",
									"description": "Optional file URL or base64:// payload.",
								},
								"file_name": map[string]any{
									"type":        "string",
									"description": "Optional display file name when file is provided.",
								},
							},
							"additionalProperties": false,
						},
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "Optional prompt shown in the merged forward card.",
					},
					"summary": map[string]any{
						"type":        "string",
						"description": "Optional summary shown in the merged forward card.",
					},
					"source": map[string]any{
						"type":        "string",
						"description": "Optional source shown in the merged forward card.",
					},
				},
				"required":             []string{"nodes"},
				"additionalProperties": false,
			},
			Available: func(evt event.Event) bool {
				return evt.ChatType == "group"
			},
			Handle: func(_ context.Context, toolCtx sdk.AIToolContext, args json.RawMessage) (any, error) {
				var payload struct {
					Nodes   []toolForwardNodeInput `json:"nodes"`
					Prompt  string                 `json:"prompt"`
					Summary string                 `json:"summary"`
					Source  string                 `json:"source"`
				}
				if err := decodeToolArgs(args, &payload); err != nil {
					return nil, err
				}
				nativeCtx, ok := toolCtx.(*toolExecutionContext)
				if !ok {
					return nil, fmt.Errorf("当前运行时不支持调度合并转发")
				}
				nodes, err := buildToolForwardNodes(payload.Nodes, toolCtx.Event())
				if err != nil {
					return nil, err
				}
				opts := message.ForwardOptions{
					Prompt:  guardrailText(payload.Prompt),
					Summary: guardrailText(payload.Summary),
					Source:  guardrailText(payload.Source),
				}
				msg, err := nativeCtx.scheduleCurrentForward(nodes, opts, firstNonEmpty(opts.Summary, opts.Prompt, "[合并转发]"))
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"scheduled": true,
					"group_id":  toolCtx.Event().GroupID,
					"nodes":     len(msg.ForwardNodes),
					"summary":   msg.Text,
				}, nil
			},
		},
		{
			Name:        "get_current_time",
			Description: "Read current server local time for date, schedule, greeting, or time-sensitive wording.",
			InputSchema: emptyToolInputSchema(),
			Handle: func(_ context.Context, _ sdk.AIToolContext, _ json.RawMessage) (any, error) {
				now := time.Now()
				zoneName, zoneOffset := now.Zone()
				return map[string]any{
					"now":             now.Format(time.RFC3339),
					"date":            now.Format("2006-01-02"),
					"time":            now.Format("15:04:05"),
					"weekday":         now.Weekday().String(),
					"unix":            now.Unix(),
					"timezone":        zoneName,
					"utc_offset_sec":  zoneOffset,
					"utc_offset_text": formatUTCOffset(zoneOffset),
				}, nil
			},
		},
		{
			Name:        "get_current_conversation_context",
			Description: "Read normalized metadata of the current conversation and triggering message, such as chat type, IDs, sender display name, text, reply target, and segment types.",
			InputSchema: emptyToolInputSchema(),
			Handle: func(_ context.Context, toolCtx sdk.AIToolContext, _ json.RawMessage) (any, error) {
				return buildCurrentConversationToolPayload(toolCtx.Event()), nil
			},
		},
		{
			Name:        "run_cli_command",
			Description: "Run one allowed CLI executable without shell expansion. This tool is disabled until ai.cli.enabled is turned on and the executable is listed in ai.cli.allowed_commands.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "Executable name or exact allowed path. Shell interpreters are not supported.",
					},
					"args": map[string]any{
						"type":        "array",
						"description": "Optional argument list passed directly to the executable.",
						"items": map[string]any{
							"type": "string",
						},
					},
				},
				"required":             []string{"command"},
				"additionalProperties": false,
			},
			Handle: func(ctx context.Context, _ sdk.AIToolContext, args json.RawMessage) (any, error) {
				var payload struct {
					Command string   `json:"command"`
					Args    []string `json:"args"`
				}
				if err := decodeToolArgs(args, &payload); err != nil {
					return nil, err
				}
				command, commandArgs, err := normalizeCLIInvocation(payload.Command, payload.Args)
				if err != nil {
					return nil, err
				}
				cliCfg := s.cliConfigSnapshot()
				if !cliCfg.Enabled {
					return nil, fmt.Errorf("CLI 能力未启用，请先在 ai.cli.enabled 中开启")
				}
				if !isAllowedCLICommand(command, cliCfg.AllowedCommands) {
					return nil, fmt.Errorf("命令 %q 不在 ai.cli.allowed_commands 白名单中", command)
				}
				runner := s.cliRunner
				if runner == nil {
					runner = execCLICommandRunner{}
				}
				runCtx, cancel := context.WithTimeout(ctx, cliTimeoutDuration(cliCfg.TimeoutSeconds))
				defer cancel()
				result, err := runner.Run(runCtx, command, commandArgs, cliCfg.MaxOutputBytes)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"command":     result.Command,
					"args":        result.Args,
					"exit_code":   result.ExitCode,
					"stdout":      result.Stdout,
					"stderr":      result.Stderr,
					"timed_out":   result.TimedOut,
					"truncated":   result.Truncated,
					"timeout_sec": cliCfg.TimeoutSeconds,
				}, nil
			},
		},
		{
			Name:        "search_message_history",
			Description: "Search stored local message history for the current conversation scope only. Use it to look up recent relevant messages before replying.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Optional keyword to search in local message text. Leave empty to read the latest messages in scope.",
					},
					"scope": map[string]any{
						"type":        "string",
						"description": "History scope. Supported values: current_chat, current_group, current_user.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of messages to return. Recommended 1 to 10.",
					},
				},
				"additionalProperties": false,
			},
			Handle: func(ctx context.Context, toolCtx sdk.AIToolContext, args json.RawMessage) (any, error) {
				var payload struct {
					Query string `json:"query"`
					Scope string `json:"scope"`
					Limit int    `json:"limit"`
				}
				if err := decodeToolArgs(args, &payload); err != nil {
					return nil, err
				}
				query, resolvedScope, err := resolveMessageHistoryQuery(toolCtx.Event(), payload.Scope, strings.TrimSpace(payload.Query), payload.Limit)
				if err != nil {
					return nil, err
				}
				items, err := s.ListMessageLogs(ctx, query)
				if err != nil {
					return nil, err
				}
				results := make([]map[string]any, 0, len(items))
				for _, item := range items {
					results = append(results, buildToolMessageLogPayload(item))
				}
				return map[string]any{
					"scope":    resolvedScope,
					"query":    query.Keyword,
					"returned": len(results),
					"items":    results,
				}, nil
			},
		},
		{
			Name:        "get_message_detail",
			Description: "Read the full stored detail of one message in the current conversation by message_id, including text, reply relation, and image summary.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"message_id": map[string]any{
						"type":        "string",
						"description": "Stored message_id returned by local history search results.",
					},
				},
				"required":             []string{"message_id"},
				"additionalProperties": false,
			},
			Handle: func(ctx context.Context, toolCtx sdk.AIToolContext, args json.RawMessage) (any, error) {
				var payload struct {
					MessageID string `json:"message_id"`
				}
				if err := decodeToolArgs(args, &payload); err != nil {
					return nil, err
				}
				payload.MessageID = strings.TrimSpace(payload.MessageID)
				if payload.MessageID == "" {
					return nil, fmt.Errorf("message_id 不能为空")
				}
				detail, err := s.GetMessageDetail(ctx, payload.MessageID)
				if err != nil {
					return nil, err
				}
				if !messageLogMatchesCurrentScope(detail.Message, toolCtx.Event()) {
					return nil, fmt.Errorf("目标消息不属于当前会话范围")
				}
				return buildToolMessageDetailPayload(detail), nil
			},
		},
	}

	if hasGroupMemberInfo || hasStrangerInfo {
		defs = append(defs, sdk.AIToolDefinition{
			Name:        "get_current_user_info",
			Description: "Read profile information about the user who sent the current message.",
			InputSchema: emptyToolInputSchema(),
			Handle: func(ctx context.Context, toolCtx sdk.AIToolContext, _ json.RawMessage) (any, error) {
				return s.lookupCurrentUserInfo(ctx, toolCtx.Event())
			},
		})
	}

	if hasGroupInfo {
		defs = append(defs, sdk.AIToolDefinition{
			Name:        "get_current_group_info",
			Description: "Read profile information about the current group chat.",
			InputSchema: emptyToolInputSchema(),
			Available: func(evt event.Event) bool {
				return evt.ChatType == "group"
			},
			Handle: func(ctx context.Context, toolCtx sdk.AIToolContext, _ json.RawMessage) (any, error) {
				evt := toolCtx.Event()
				getter := s.messenger.(groupInfoGetter)
				info, err := getter.GetGroupInfo(ctx, evt.ConnectionID, evt.GroupID)
				if err != nil {
					return nil, err
				}
				if info == nil {
					return nil, fmt.Errorf("未获取到当前群资料")
				}
				return map[string]any{
					"group_id":           info.GroupID,
					"group_name":         info.GroupName,
					"member_count":       info.MemberCount,
					"max_member_count":   info.MaxMemberCount,
					"display_name":       firstNonEmpty(info.GroupName, info.GroupID),
					"conversation_scope": "current_group",
				}, nil
			},
		})
	}

	if hasGroupMemberInfo {
		defs = append(defs, sdk.AIToolDefinition{
			Name:        "get_group_member_info",
			Description: "Read profile information about a member in the current group chat by user_id.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"user_id": map[string]any{
						"type":        "string",
						"description": "QQ user ID of the target group member in the current group.",
					},
				},
				"required":             []string{"user_id"},
				"additionalProperties": false,
			},
			Available: func(evt event.Event) bool {
				return evt.ChatType == "group"
			},
			Handle: func(ctx context.Context, toolCtx sdk.AIToolContext, args json.RawMessage) (any, error) {
				var payload struct {
					UserID string `json:"user_id"`
				}
				if err := decodeToolArgs(args, &payload); err != nil {
					return nil, err
				}
				payload.UserID = strings.TrimSpace(payload.UserID)
				if payload.UserID == "" {
					return nil, fmt.Errorf("user_id 不能为空")
				}
				evt := toolCtx.Event()
				getter := s.messenger.(groupMemberInfoGetter)
				info, err := getter.GetGroupMemberInfo(ctx, evt.ConnectionID, evt.GroupID, payload.UserID)
				if err != nil {
					return nil, err
				}
				if info == nil {
					return nil, fmt.Errorf("未获取到群成员资料")
				}
				return groupMemberToolPayload(info), nil
			},
		})
	}

	if hasGroupMemberList {
		defs = append(defs, sdk.AIToolDefinition{
			Name:        "list_current_group_members",
			Description: "List members in the current group chat. You can optionally filter by keyword on QQ number, nickname, or group card.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"keyword": map[string]any{
						"type":        "string",
						"description": "Optional filter keyword matched against user_id, nickname, or group card.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of members to return. Recommended 1 to 20.",
					},
				},
				"additionalProperties": false,
			},
			Available: func(evt event.Event) bool {
				return evt.ChatType == "group"
			},
			Handle: func(ctx context.Context, toolCtx sdk.AIToolContext, args json.RawMessage) (any, error) {
				var payload struct {
					Keyword string `json:"keyword"`
					Limit   int    `json:"limit"`
				}
				if err := decodeToolArgs(args, &payload); err != nil {
					return nil, err
				}
				keyword := strings.TrimSpace(payload.Keyword)
				limit := payload.Limit
				if limit <= 0 {
					limit = 10
				} else {
					limit = maxInt(1, minInt(limit, 20))
				}
				evt := toolCtx.Event()
				getter := s.messenger.(groupMemberListGetter)
				items, err := getter.GetGroupMemberList(ctx, evt.ConnectionID, evt.GroupID)
				if err != nil {
					return nil, err
				}
				matches := make([]map[string]any, 0, minInt(len(items), limit))
				normalizedKeyword := strings.ToLower(keyword)
				for _, item := range items {
					if normalizedKeyword != "" &&
						!strings.Contains(strings.ToLower(item.UserID), normalizedKeyword) &&
						!strings.Contains(strings.ToLower(item.Nickname), normalizedKeyword) &&
						!strings.Contains(strings.ToLower(item.Card), normalizedKeyword) {
						continue
					}
					matches = append(matches, map[string]any{
						"group_id":     item.GroupID,
						"user_id":      item.UserID,
						"nickname":     item.Nickname,
						"card":         item.Card,
						"display_name": firstNonEmpty(item.Card, item.Nickname, item.UserID),
						"role":         item.Role,
						"title":        item.Title,
					})
					if len(matches) >= limit {
						break
					}
				}
				return map[string]any{
					"group_id": evt.GroupID,
					"keyword":  keyword,
					"total":    len(items),
					"returned": len(matches),
					"members":  matches,
				}, nil
			},
		})
	}

	if hasStatus {
		defs = append(defs, sdk.AIToolDefinition{
			Name:        "get_connection_status",
			Description: "Read online and health status of the current bot connection.",
			InputSchema: emptyToolInputSchema(),
			Handle: func(ctx context.Context, toolCtx sdk.AIToolContext, _ json.RawMessage) (any, error) {
				evt := toolCtx.Event()
				getter := s.messenger.(statusGetter)
				status, err := getter.GetStatus(ctx, evt.ConnectionID)
				if err != nil {
					return nil, err
				}
				if status == nil {
					return nil, fmt.Errorf("未获取到连接状态")
				}
				return map[string]any{
					"connection_id": evt.ConnectionID,
					"online":        status.Online,
					"good":          status.Good,
					"stat":          status.Stat,
				}, nil
			},
		})
	}

	if hasLoginInfo {
		defs = append(defs, sdk.AIToolDefinition{
			Name:        "get_current_bot_info",
			Description: "Read profile information about the current connected bot account.",
			InputSchema: emptyToolInputSchema(),
			Handle: func(ctx context.Context, toolCtx sdk.AIToolContext, _ json.RawMessage) (any, error) {
				evt := toolCtx.Event()
				getter := s.messenger.(loginInfoGetter)
				info, err := getter.GetLoginInfo(ctx, evt.ConnectionID)
				if err != nil {
					return nil, err
				}
				selfID := strings.TrimSpace(evt.Meta["self_id"])
				if info == nil && selfID == "" {
					return nil, fmt.Errorf("未获取到机器人资料")
				}
				if info == nil {
					return map[string]any{
						"user_id":      selfID,
						"display_name": selfID,
					}, nil
				}
				return map[string]any{
					"user_id":      firstNonEmpty(info.UserID, selfID),
					"nickname":     info.Nickname,
					"self_id":      selfID,
					"display_name": firstNonEmpty(info.Nickname, info.UserID, selfID),
				}, nil
			},
		})
	}

	return defs
}

func (s *Service) runToolLoop(ctx context.Context, gen generator, cfg config.AIConfig, messages []chatMessage, tools []toolDefinition, exec *toolExecutionContext) (turnResult, error) {
	conversation := append([]chatMessage(nil), messages...)
	toolSpecs := make([]toolSpec, 0, len(tools))
	toolHandlers := make(map[string]toolDefinition, len(tools))
	for _, def := range tools {
		toolSpecs = append(toolSpecs, def.Spec)
		toolHandlers[def.Spec.Name] = def
	}

	toolOutboundSent := false
	for hop := 0; hop < maxToolLoopHops; hop++ {
		result, err := gen.RunTurn(ctx, conversation, toolSpecs, cfg)
		if err != nil {
			return turnResult{}, err
		}
		if len(result.ToolCalls) == 0 {
			if strings.TrimSpace(result.Text) == "" && exec != nil && exec.scheduled == nil {
				result.ToolOutboundSent = toolOutboundSent
			}
			return result, nil
		}

		normalizedCalls := make([]toolCall, 0, len(result.ToolCalls))
		for _, call := range result.ToolCalls {
			call.ID = firstNonEmpty(call.ID, ensureMessageID("toolcall", ""))
			normalizedCalls = append(normalizedCalls, call)
		}
		result.ToolCalls = normalizedCalls

		assistantMsg := chatMessage{
			Role:             "assistant",
			ReasoningContent: strings.TrimSpace(result.ReasoningContent),
			ToolCalls:        normalizedCalls,
		}
		if strings.TrimSpace(result.Text) != "" {
			assistantMsg.Content = strings.TrimSpace(result.Text)
		}
		conversation = append(conversation, assistantMsg)

		for _, call := range normalizedCalls {
			def, ok := toolHandlers[call.Name]
			var toolContent string
			if !ok {
				toolContent = marshalToolExecutionResult(call.Name, nil, fmt.Errorf("未注册工具 %s", call.Name))
			} else {
				value, execErr := def.Handler(ctx, exec, call.Arguments)
				if exec != nil && exec.scheduled != nil {
					toolOutboundSent = true
				}
				if execErr == nil && toolResultIndicatesOutbound(value) {
					toolOutboundSent = true
				}
				toolContent = marshalToolExecutionResult(call.Name, value, execErr)
			}
			conversation = append(conversation, chatMessage{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    toolContent,
			})
		}
	}

	return turnResult{}, fmt.Errorf("AI 工具调用超过最大轮数 %d", maxToolLoopHops)
}

func injectToolGuidanceMessage(messages []chatMessage, tools []toolDefinition) []chatMessage {
	guidance := chatMessage{
		Role:    "system",
		Content: buildToolGuidanceContent(tools),
	}
	if len(messages) == 0 {
		return []chatMessage{guidance}
	}
	out := make([]chatMessage, 0, len(messages)+1)
	out = append(out, messages[:len(messages)-1]...)
	out = append(out, guidance, messages[len(messages)-1])
	return out
}

func buildToolGuidanceContent(tools []toolDefinition) string {
	var builder strings.Builder
	builder.WriteString("当前回合可以使用工具。请按以下规则决定是否调用：\n")
	builder.WriteString("- 如果用户明确要求执行动作、查询资料、读取消息详情、获取群/用户/连接信息、生成图片、发送图片/视频/文件/合并转发，或调用插件能力，应优先调用匹配工具，不要只用文字回答“可以”。\n")
	builder.WriteString("- 普通闲聊或不需要外部能力时，直接文本回复即可，不要为了调用而调用。\n")
	builder.WriteString("- 工具参数必须按工具 schema 填写；能从当前消息、引用消息和上下文推断的参数不要再追问，确实缺少必要参数时再简短追问。\n")
	builder.WriteString("- 发送类工具调用成功后，最终回复不要重复整段已发送内容。\n")
	builder.WriteString("\n当前可用工具：")
	if len(tools) == 0 {
		builder.WriteString("\n- 无")
		return builder.String()
	}
	limit := len(tools)
	if limit > maxToolGuidanceItems {
		limit = maxToolGuidanceItems
	}
	for i := 0; i < limit; i++ {
		spec := tools[i].Spec
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			continue
		}
		desc := trimToolGuidanceDescription(spec.Description)
		if desc == "" {
			desc = "无描述"
		}
		builder.WriteString("\n- ")
		builder.WriteString(name)
		builder.WriteString(": ")
		builder.WriteString(desc)
	}
	if len(tools) > limit {
		builder.WriteString("\n- 另有 ")
		builder.WriteString(fmt.Sprintf("%d", len(tools)-limit))
		builder.WriteString(" 个工具未展开；如果 tools schema 中有更精确匹配的工具，也可以调用。")
	}
	return builder.String()
}

func trimToolGuidanceDescription(value string) string {
	text := strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	runes := []rune(text)
	if len(runes) <= maxToolGuidanceDesc {
		return text
	}
	return string(runes[:maxToolGuidanceDesc]) + "..."
}

func (s *Service) lookupCurrentUserInfo(ctx context.Context, evt event.Event) (any, error) {
	if evt.ChatType == "group" {
		if getter, ok := s.messenger.(groupMemberInfoGetter); ok {
			info, err := getter.GetGroupMemberInfo(ctx, evt.ConnectionID, evt.GroupID, evt.UserID)
			if err == nil && info != nil {
				return groupMemberToolPayload(info), nil
			}
			if err != nil {
				if _, fallback := s.messenger.(strangerInfoGetter); !fallback {
					return nil, err
				}
			}
		}
	}
	getter, ok := s.messenger.(strangerInfoGetter)
	if !ok {
		return nil, fmt.Errorf("当前连接不支持读取用户资料")
	}
	info, err := getter.GetStrangerInfo(ctx, evt.ConnectionID, evt.UserID)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, fmt.Errorf("未获取到当前用户资料")
	}
	return map[string]any{
		"user_id":      info.UserID,
		"nickname":     info.Nickname,
		"sex":          info.Sex,
		"age":          info.Age,
		"display_name": firstNonEmpty(info.Nickname, info.UserID),
		"chat_type":    evt.ChatType,
	}, nil
}

func groupMemberToolPayload(info *sdk.GroupMemberInfo) map[string]any {
	return map[string]any{
		"group_id":       info.GroupID,
		"user_id":        info.UserID,
		"nickname":       info.Nickname,
		"card":           info.Card,
		"display_name":   firstNonEmpty(info.Card, info.Nickname, info.UserID),
		"role":           info.Role,
		"sex":            info.Sex,
		"age":            info.Age,
		"level":          info.Level,
		"title":          info.Title,
		"area":           info.Area,
		"join_time":      info.JoinTime,
		"last_sent_time": info.LastSent,
		"chat_type":      "group",
	}
}

func resolveMessageHistoryQuery(evt event.Event, scope string, keyword string, limit int) (MessageLogQuery, string, error) {
	query := MessageLogQuery{
		Keyword: strings.TrimSpace(keyword),
		Limit:   limit,
	}
	if query.Limit <= 0 {
		query.Limit = 8
	} else {
		query.Limit = maxInt(1, minInt(query.Limit, 10))
	}

	resolvedScope := strings.ToLower(strings.TrimSpace(scope))
	if resolvedScope == "" {
		resolvedScope = "current_chat"
	}
	switch resolvedScope {
	case "current_chat":
		query.ChatType = evt.ChatType
		if evt.ChatType == "group" {
			query.GroupID = evt.GroupID
		} else {
			query.UserID = evt.UserID
		}
	case "current_group":
		if evt.ChatType != "group" {
			return MessageLogQuery{}, "", fmt.Errorf("当前不是群聊，无法检索 current_group")
		}
		query.ChatType = "group"
		query.GroupID = evt.GroupID
	case "current_user":
		query.ChatType = evt.ChatType
		query.UserID = evt.UserID
		if evt.ChatType == "group" {
			query.GroupID = evt.GroupID
		}
	default:
		return MessageLogQuery{}, "", fmt.Errorf("scope 仅支持 current_chat、current_group、current_user")
	}
	return query, resolvedScope, nil
}

func buildToolMessageLogPayload(item MessageLog) map[string]any {
	return map[string]any{
		"message_id":          item.MessageID,
		"chat_type":           item.ChatType,
		"group_id":            item.GroupID,
		"group_name":          item.GroupName,
		"user_id":             item.UserID,
		"sender_name":         item.SenderName,
		"sender_nickname":     item.SenderNickname,
		"sender_role":         item.SenderRole,
		"reply_to_message_id": item.ReplyToMessageID,
		"text_preview":        truncateToolText(item.TextContent, 160),
		"has_image":           item.HasImage,
		"image_count":         item.ImageCount,
		"message_status":      item.MessageStatus,
		"occurred_at":         item.OccurredAt,
	}
}

func buildToolMessageDetailPayload(detail MessageDetail) map[string]any {
	images := make([]map[string]any, 0, len(detail.Images))
	for _, image := range detail.Images {
		images = append(images, map[string]any{
			"segment_index": image.SegmentIndex,
			"origin_ref":    image.OriginRef,
			"summary":       image.VisionSummary,
			"vision_status": image.VisionStatus,
			"mime_type":     image.MimeType,
			"preview_url":   image.PreviewURL,
		})
	}
	return map[string]any{
		"message": buildToolMessageLogPayload(detail.Message),
		"text":    detail.Message.TextContent,
		"images":  images,
	}
}

func messageLogMatchesCurrentScope(item MessageLog, evt event.Event) bool {
	if strings.TrimSpace(item.ConnectionID) != strings.TrimSpace(evt.ConnectionID) {
		return false
	}
	if strings.TrimSpace(item.ChatType) != strings.TrimSpace(evt.ChatType) {
		return false
	}
	switch evt.ChatType {
	case "group":
		return strings.TrimSpace(item.GroupID) == strings.TrimSpace(evt.GroupID)
	case "private":
		return strings.TrimSpace(item.UserID) == strings.TrimSpace(evt.UserID)
	default:
		return false
	}
}

func truncateToolText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit]) + "…"
}

func emptyToolInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func normalizeToolMediaRef(kind string, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s file 不能为空", toolMediaKindLabel(kind))
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "base64://") {
		return trimmed, nil
	}
	switch strings.TrimSpace(kind) {
	case "image":
		if strings.HasPrefix(lower, "data:image/") {
			return trimmed, nil
		}
	case "video":
		if strings.HasPrefix(lower, "data:video/") {
			return trimmed, nil
		}
	}
	if strings.HasPrefix(lower, "data:") {
		return "", fmt.Errorf("%s file 不支持该 data URI 类型", toolMediaKindLabel(kind))
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%s file 仅支持 http(s)、匹配的 data URI 或 base64://", toolMediaKindLabel(kind))
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return trimmed, nil
	default:
		return "", fmt.Errorf("%s file 仅支持 http(s)、匹配的 data URI 或 base64://", toolMediaKindLabel(kind))
	}
}

func toolMediaKindLabel(kind string) string {
	switch strings.TrimSpace(kind) {
	case "image":
		return "图片"
	case "video":
		return "视频"
	case "file":
		return "文件"
	default:
		return "媒体"
	}
}

func buildToolForwardNodes(items []toolForwardNodeInput, evt event.Event) ([]message.ForwardNode, error) {
	nodes := make([]message.ForwardNode, 0, len(items))
	defaultUserID := firstNonEmpty(strings.TrimSpace(evt.Meta["self_id"]), strings.TrimSpace(evt.UserID), "0")
	defaultNickname := firstNonEmpty(strings.TrimSpace(evt.Meta["self_nickname"]), "AI")
	for index, item := range items {
		content := make([]message.Segment, 0, 4)
		if text := guardrailText(item.Text); text != "" {
			content = append(content, message.Text(text))
		}
		if imageFile := strings.TrimSpace(item.ImageFile); imageFile != "" {
			ref, err := normalizeToolMediaRef("image", imageFile)
			if err != nil {
				return nil, fmt.Errorf("nodes[%d].image_file: %w", index, err)
			}
			content = append(content, message.Image(ref))
		}
		if videoFile := strings.TrimSpace(item.VideoFile); videoFile != "" {
			ref, err := normalizeToolMediaRef("video", videoFile)
			if err != nil {
				return nil, fmt.Errorf("nodes[%d].video_file: %w", index, err)
			}
			content = append(content, message.Video(ref))
		}
		if file := strings.TrimSpace(item.File); file != "" {
			ref, err := normalizeToolMediaRef("file", file)
			if err != nil {
				return nil, fmt.Errorf("nodes[%d].file: %w", index, err)
			}
			content = append(content, message.File(ref, strings.TrimSpace(item.FileName)))
		}
		if len(content) == 0 {
			return nil, fmt.Errorf("nodes[%d] 内容为空", index)
		}
		nodes = append(nodes, message.ForwardNode{
			UserID:   firstNonEmpty(strings.TrimSpace(item.UserID), defaultUserID),
			Nickname: firstNonEmpty(strings.TrimSpace(item.Nickname), defaultNickname),
			Content:  content,
		})
	}
	return nodes, nil
}

func normalizeToolForwardNodes(nodes []message.ForwardNode) ([]message.ForwardNode, error) {
	out := make([]message.ForwardNode, 0, len(nodes))
	for i, node := range nodes {
		content := normalizeToolSegments(node.Content)
		if len(content) == 0 {
			return nil, fmt.Errorf("合并转发节点 %d 内容为空", i)
		}
		out = append(out, message.ForwardNode{
			UserID:   strings.TrimSpace(node.UserID),
			Nickname: strings.TrimSpace(node.Nickname),
			Content:  content,
		})
	}
	return out, nil
}

func normalizeToolForwardOptions(opts message.ForwardOptions) message.ForwardOptions {
	return message.ForwardOptions{
		Prompt:  guardrailText(opts.Prompt),
		Summary: guardrailText(opts.Summary),
		Source:  guardrailText(opts.Source),
	}
}

func summarizeToolForwardNodes(nodes []message.ForwardNode) string {
	if len(nodes) == 0 {
		return "[合并转发]"
	}
	previews := make([]string, 0, minInt(len(nodes), 3))
	for _, node := range nodes {
		if len(previews) >= 3 {
			break
		}
		text := truncateToolText(summarizeToolSegments(node.Content), 40)
		if text == "" {
			continue
		}
		previews = append(previews, text)
	}
	if len(previews) == 0 {
		return "[合并转发]"
	}
	return "[合并转发] " + strings.Join(previews, " / ")
}

func normalizeToolSegments(segs []message.Segment) []message.Segment {
	out := make([]message.Segment, 0, len(segs))
	for _, seg := range segs {
		segType := strings.TrimSpace(seg.Type)
		if segType == "" {
			continue
		}
		data := make(map[string]any, len(seg.Data))
		for key, value := range seg.Data {
			data[key] = value
		}
		if segType == "text" {
			text := guardrailText(segmentString(data, "text"))
			if text == "" {
				continue
			}
			data["text"] = text
		}
		out = append(out, message.Segment{Type: segType, Data: data})
	}
	return out
}

func summarizeToolSegments(segs []message.Segment) string {
	var parts []string
	for _, seg := range segs {
		switch strings.TrimSpace(seg.Type) {
		case "text":
			if text := guardrailText(segmentString(seg.Data, "text")); text != "" {
				parts = append(parts, text)
			}
		case "image":
			parts = append(parts, "[图片]")
		case "video":
			parts = append(parts, "[视频]")
		case "file":
			parts = append(parts, "[文件]")
		case "reply":
		default:
			parts = append(parts, "["+strings.TrimSpace(seg.Type)+"]")
		}
	}
	text := guardrailText(strings.Join(parts, ""))
	if text == "" {
		return "[消息]"
	}
	return text
}

func buildCurrentConversationToolPayload(evt event.Event) map[string]any {
	segmentTypes := make([]string, 0, len(evt.Segments))
	hasImage := false
	hasVideo := false
	hasFile := false
	for _, seg := range evt.Segments {
		segType := strings.TrimSpace(seg.Type)
		if segType == "" {
			continue
		}
		segmentTypes = append(segmentTypes, segType)
		if segType == "image" {
			hasImage = true
		}
		if segType == "video" {
			hasVideo = true
		}
		if segType == "file" {
			hasFile = true
		}
	}
	replyTo := extractReplyReference(evt)
	return map[string]any{
		"connection_id":       evt.ConnectionID,
		"chat_type":           evt.ChatType,
		"group_id":            evt.GroupID,
		"user_id":             evt.UserID,
		"message_id":          evt.MessageID,
		"reply_to_message_id": replyTo,
		"self_id":             strings.TrimSpace(evt.Meta["self_id"]),
		"sender_name":         eventSenderName(evt),
		"text":                cleanEventText(evt),
		"segment_types":       segmentTypes,
		"has_image":           hasImage,
		"has_video":           hasVideo,
		"has_file":            hasFile,
		"mentioned_bot":       hasAtSelf(evt.Segments, evt.Meta["self_id"]),
		"is_group":            evt.ChatType == "group",
		"occurred_at":         eventTimestampOrNow(evt.Timestamp).Format(time.RFC3339),
	}
}

func formatUTCOffset(offsetSeconds int) string {
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}

func normalizeAIToolDefinitions(tools []sdk.AIToolDefinition) ([]sdk.AIToolDefinition, error) {
	if len(tools) == 0 {
		return nil, fmt.Errorf("AI tools 不能为空")
	}
	seen := make(map[string]struct{}, len(tools))
	out := make([]sdk.AIToolDefinition, 0, len(tools))
	for _, item := range tools {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return nil, fmt.Errorf("AI tool 名称不能为空")
		}
		if item.Handle == nil {
			return nil, fmt.Errorf("AI tool %s 缺少处理函数", name)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("AI tool %s 在同一 provider 中重复定义", name)
		}
		seen[name] = struct{}{}
		schema := item.InputSchema
		if len(schema) == 0 {
			schema = emptyToolInputSchema()
		} else {
			schema = cloneToolInputSchema(schema)
		}
		out = append(out, sdk.AIToolDefinition{
			Name:        name,
			Description: strings.TrimSpace(item.Description),
			InputSchema: schema,
			Available:   item.Available,
			Handle:      item.Handle,
		})
	}
	return out, nil
}

func cloneToolInputSchema(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return emptyToolInputSchema()
	}
	body, err := json.Marshal(schema)
	if err != nil {
		return emptyToolInputSchema()
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return emptyToolInputSchema()
	}
	if len(out) == 0 {
		return emptyToolInputSchema()
	}
	return out
}

func buildSkillView(providerID string, defs []sdk.AIToolDefinition) SkillView {
	source, pluginID, namespace := parseSkillProviderID(providerID)
	tools := make([]SkillToolView, 0, len(defs))
	for _, item := range defs {
		displayName, displayDescription, availability := toolDisplayMeta(source, item.Name, item.Description)
		tools = append(tools, SkillToolView{
			Name:               item.Name,
			Description:        strings.TrimSpace(item.Description),
			DisplayName:        displayName,
			DisplayDescription: displayDescription,
			Availability:       availability,
		})
	}

	name := providerID
	description := ""
	switch source {
	case "builtin":
		name = "核心技能"
		description = "AI 内置的当前会话发送、资料查询、消息检索、连接状态与受限 CLI 能力。"
	case "plugin":
		name = pluginID
		description = "由插件注册的 AI 技能。"
	case "mcp":
		name = namespace
		description = "由 MCP 工具服务器发现并注册的 AI 技能。"
	default:
		if namespace != "" {
			name = namespace
		}
		description = "由扩展 provider 注册的 AI 技能。"
	}
	if namespace != "" && source != "builtin" {
		name = name + " / " + namespace
	}

	return SkillView{
		ProviderID:  providerID,
		Source:      source,
		PluginID:    pluginID,
		Namespace:   namespace,
		Name:        name,
		Description: description,
		ToolCount:   len(tools),
		Tools:       tools,
	}
}

func toolDisplayMeta(source string, name string, description string) (string, string, string) {
	rawDescription := strings.TrimSpace(description)
	if source != "builtin" {
		return name, rawDescription, ""
	}
	switch strings.TrimSpace(name) {
	case "send_message_current":
		return "发送当前会话消息", "让 AI 直接把最终回复发到当前群聊或私聊；在群聊中也可以选择回复触发消息。", "当前会话"
	case "send_image_current":
		return "发送当前会话图片", "让 AI 向当前会话发送一张图片或表情；适合配合表情包库、贴纸选择工具使用。", "当前会话"
	case "send_video_current":
		return "发送当前会话视频", "让 AI 向当前会话发送一个视频；仅适合用户明确需要视频或插件已产出可信视频资源时使用。", "当前会话"
	case "send_file_current":
		return "发送当前会话文件", "让 AI 向当前会话发送一个文件；仅支持可信 URL 或 base64 资源，不允许任意本地路径。", "当前会话"
	case "send_group_forward_current":
		return "发送群聊合并转发", "让 AI 在当前群聊发送合并转发消息，适合多段内容、整理结果或引用集合。", "仅群聊"
	case "get_current_time":
		return "读取当前时间", "读取服务器当前日期、时间、星期和时区，用于问候、日程和时间相关回复。", "本地时间"
	case "get_current_conversation_context":
		return "读取当前会话上下文", "读取当前消息的会话类型、群号、用户、消息 ID、文本和消息段类型等元数据。", "当前上下文"
	case "search_message_history":
		return "检索当前会话历史", "查询本地已入库的聊天记录，可按当前会话、当前群或当前发言人范围做关键词检索。", "当前上下文"
	case "run_cli_command":
		return "执行白名单 CLI", "按白名单调用宿主上的具体命令行工具，不经过 shell 展开；适合读取版本、目录和状态信息。", "需先启用"
	case "get_message_detail":
		return "查看消息详情", "读取某条已入库消息的完整详情，包括正文、回复关系和图片摘要。", "当前上下文"
	case "get_current_user_info":
		return "查看当前发言人资料", "获取当前发言人的昵称、QQ 号、性别、年龄等资料，便于称呼和识别上下文。", "当前发言人"
	case "get_current_group_info":
		return "查看当前群资料", "读取当前群的群名、人数和群号等基础信息。", "仅群聊"
	case "get_group_member_info":
		return "查看指定群成员资料", "按 QQ 号查询当前群内某位成员的资料，例如群名片、昵称、头衔和角色。", "仅群聊"
	case "list_current_group_members":
		return "列出当前群成员", "列出当前群成员，并支持按 QQ 号、昵称或群名片做筛选。", "仅群聊"
	case "get_connection_status":
		return "查看连接状态", "读取当前连接是否在线、状态是否健康以及连接统计信息。", "当前连接"
	case "get_current_bot_info":
		return "查看机器人资料", "读取当前连接机器人自己的 QQ 号与昵称资料。", "当前机器人"
	default:
		return name, firstNonEmpty(rawDescription, "该工具当前未提供额外中文说明。"), ""
	}
}

func parseSkillProviderID(providerID string) (source string, pluginID string, namespace string) {
	trimmed := strings.TrimSpace(providerID)
	if trimmed == "" {
		return "custom", "", defaultToolNamespace
	}
	if strings.HasPrefix(trimmed, "builtin.") {
		return "builtin", "", strings.TrimPrefix(trimmed, "builtin.")
	}
	if strings.HasPrefix(trimmed, "plugin.") {
		parts := strings.Split(trimmed, ".")
		if len(parts) >= 3 {
			return "plugin", parts[1], strings.Join(parts[2:], ".")
		}
		return "plugin", strings.TrimPrefix(trimmed, "plugin."), defaultToolNamespace
	}
	if strings.HasPrefix(trimmed, "mcp.") {
		return "mcp", "", strings.TrimPrefix(trimmed, "mcp.")
	}
	return "custom", "", trimmed
}

func decodeToolArgs(raw json.RawMessage, target any) error {
	payload := strings.TrimSpace(string(raw))
	if payload == "" {
		payload = "{}"
	}
	if err := json.Unmarshal([]byte(payload), target); err != nil {
		return fmt.Errorf("解析工具参数失败: %w", err)
	}
	return nil
}

func marshalToolExecutionResult(name string, value any, err error) string {
	payload := map[string]any{
		"ok":   err == nil,
		"tool": name,
	}
	if err != nil {
		payload["error"] = err.Error()
	} else {
		payload["result"] = value
	}
	body, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		fallback, _ := json.Marshal(map[string]any{
			"ok":    false,
			"tool":  name,
			"error": marshalErr.Error(),
		})
		return string(fallback)
	}
	return string(body)
}

func toolResultIndicatesOutbound(value any) bool {
	payload, ok := value.(map[string]any)
	if !ok {
		return false
	}
	return boolMapValue(payload, "sent_to_chat") || boolMapValue(payload, "scheduled")
}

func boolMapValue(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	flag, ok := value.(bool)
	return ok && flag
}
