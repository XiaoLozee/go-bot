package externalexec

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
	"github.com/spf13/viper"
)

type runnerProcess struct {
	logger  *slog.Logger
	encoder *json.Encoder

	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan hostResponsePayload
	seq       uint64

	aiToolMu     sync.RWMutex
	aiTools      map[string]sdk.AIToolDefinition
	aiNamespaces map[string][]string
}

type runnerConfigReader struct {
	raw map[string]any
}

type runnerCatalog struct {
	items []sdk.PluginInfo
}

func RunFactory(pluginID string, factory func() sdk.Plugin) error {
	if strings.TrimSpace(pluginID) == "" {
		return fmt.Errorf("plugin id is required")
	}
	if factory == nil {
		return fmt.Errorf("plugin factory is nil: %s", pluginID)
	}

	start, decoder, err := decodeRunnerStart(os.Stdin)
	if err != nil {
		return err
	}
	return runFactory(pluginID, factory, start, decoder)
}

func RunEmbedded(args []string) error {
	fs := flag.NewFlagSet("external-plugin", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pluginID := fs.String("plugin", "", "插件 ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pluginID == "" {
		return fmt.Errorf("缺少 --plugin 参数")
	}

	factory, ok := EmbeddedFactory(*pluginID)
	if !ok {
		return fmt.Errorf("未找到外部适配插件: %s", *pluginID)
	}
	return RunFactory(*pluginID, factory)
}

func decodeRunnerStart(input io.Reader) (startPayload, *json.Decoder, error) {
	var startMsg rawHostMessage
	decoder := json.NewDecoder(input)
	if err := decoder.Decode(&startMsg); err != nil {
		return startPayload{}, nil, fmt.Errorf("read start message: %w", err)
	}
	if startMsg.Type != "start" {
		return startPayload{}, nil, fmt.Errorf("first host message must be start, got %s", startMsg.Type)
	}

	var start startPayload
	if err := json.Unmarshal(startMsg.Payload, &start); err != nil {
		return startPayload{}, nil, fmt.Errorf("decode start payload: %w", err)
	}
	return start, decoder, nil
}

func runFactory(pluginID string, factory func() sdk.Plugin, start startPayload, decoder *json.Decoder) error {
	proc := &runnerProcess{
		logger:       newRunnerLogger(pluginID),
		encoder:      json.NewEncoder(os.Stdout),
		pending:      make(map[string]chan hostResponsePayload),
		aiTools:      make(map[string]sdk.AIToolDefinition),
		aiNamespaces: make(map[string][]string),
	}
	eventCh := make(chan event.Event, 16)
	stopCh := make(chan struct{}, 1)
	go proc.readHostMessages(decoder, eventCh, stopCh)

	plugin := factory()
	if err := plugin.Start(context.Background(), sdk.Env{
		Logger:        proc.logger,
		Messenger:     proc,
		BotAPI:        proc,
		AITools:       proc,
		Config:        runnerConfigReader{raw: start.Config},
		PluginCatalog: runnerCatalog{items: start.Catalog},
		App:           start.App,
	}); err != nil {
		return fmt.Errorf("start plugin %s: %w", pluginID, err)
	}

	if err := proc.sendPluginMessage("ready", readyPayload{Message: "plugin runner ready"}); err != nil {
		return err
	}

	for {
		select {
		case evt := <-eventCh:
			if err := plugin.HandleEvent(context.Background(), evt); err != nil {
				proc.logger.Error("外部插件处理事件失败", "event_kind", evt.Kind, "error", err)
			}
		case <-stopCh:
			return plugin.Stop(context.Background())
		}
	}
}

func (p *runnerProcess) SendText(_ context.Context, target message.Target, text string) error {
	return p.sendPluginMessage("send_text", sendTextPayload{
		Target: target,
		Text:   text,
	})
}

func (p *runnerProcess) SendSegments(_ context.Context, target message.Target, segs []message.Segment) error {
	return p.sendPluginMessage("send_segments", sendSegmentsPayload{
		Target:   target,
		Segments: segs,
	})
}

func (p *runnerProcess) ReplyText(_ context.Context, target message.Target, replyTo string, text string) error {
	return p.sendPluginMessage("reply_text", replyTextPayload{
		Target:  target,
		ReplyTo: replyTo,
		Text:    text,
	})
}

func (p *runnerProcess) GetStrangerInfo(ctx context.Context, connectionID, userID string) (*sdk.UserInfo, error) {
	var result sdk.UserInfo
	if err := p.callHost(ctx, CallBotGetStrangerInfo, getStrangerInfoPayload{
		ConnectionID: connectionID,
		UserID:       userID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *runnerProcess) GetGroupInfo(ctx context.Context, connectionID, groupID string) (*sdk.GroupInfo, error) {
	var result sdk.GroupInfo
	if err := p.callHost(ctx, CallBotGetGroupInfo, getGroupInfoPayload{
		ConnectionID: connectionID,
		GroupID:      groupID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *runnerProcess) GetGroupMemberList(ctx context.Context, connectionID, groupID string) ([]sdk.GroupMemberInfo, error) {
	var result []sdk.GroupMemberInfo
	if err := p.callHost(ctx, CallBotGetGroupMembers, getGroupMemberListPayload{
		ConnectionID: connectionID,
		GroupID:      groupID,
	}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *runnerProcess) GetGroupMemberInfo(ctx context.Context, connectionID, groupID, userID string) (*sdk.GroupMemberInfo, error) {
	var result sdk.GroupMemberInfo
	if err := p.callHost(ctx, CallBotGetGroupMember, getGroupMemberInfoPayload{
		ConnectionID: connectionID,
		GroupID:      groupID,
		UserID:       userID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *runnerProcess) GetMessage(ctx context.Context, connectionID, messageID string) (*sdk.MessageDetail, error) {
	var result sdk.MessageDetail
	if err := p.callHost(ctx, CallBotGetMessage, getMessagePayload{
		ConnectionID: connectionID,
		MessageID:    messageID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *runnerProcess) GetForwardMessage(ctx context.Context, connectionID, forwardID string) (*sdk.ForwardMessage, error) {
	var result sdk.ForwardMessage
	if err := p.callHost(ctx, CallBotGetForwardMessage, getForwardMessagePayload{
		ConnectionID: connectionID,
		ForwardID:    forwardID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *runnerProcess) DeleteMessage(ctx context.Context, connectionID, messageID string) error {
	return p.callHost(ctx, CallBotDeleteMessage, deleteMessagePayload{
		ConnectionID: connectionID,
		MessageID:    messageID,
	}, nil)
}

func (p *runnerProcess) ResolveMedia(ctx context.Context, connectionID, segmentType, file string) (*sdk.ResolvedMedia, error) {
	var result sdk.ResolvedMedia
	if err := p.callHost(ctx, CallBotResolveMedia, resolveMediaPayload{
		ConnectionID: connectionID,
		SegmentType:  segmentType,
		File:         file,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *runnerProcess) GetLoginInfo(ctx context.Context, connectionID string) (*sdk.LoginInfo, error) {
	var result sdk.LoginInfo
	if err := p.callHost(ctx, CallBotGetLoginInfo, getLoginInfoPayload{
		ConnectionID: connectionID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *runnerProcess) GetStatus(ctx context.Context, connectionID string) (*sdk.BotStatus, error) {
	var result sdk.BotStatus
	if err := p.callHost(ctx, CallBotGetStatus, getStatusPayload{
		ConnectionID: connectionID,
	}, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *runnerProcess) SendGroupForward(ctx context.Context, connectionID, groupID string, nodes []message.ForwardNode, opts message.ForwardOptions) error {
	return p.callHost(ctx, CallBotSendGroupForward, sendGroupForwardPayload{
		ConnectionID: connectionID,
		GroupID:      groupID,
		Nodes:        nodes,
		Options:      opts,
	}, nil)
}

func (p *runnerProcess) sendPluginMessage(msgType string, payload any) error {
	raw, err := marshalPayload(payload)
	if err != nil {
		return err
	}

	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.encoder.Encode(pluginMessage{
		Type:    msgType,
		Payload: raw,
	})
}

func (p *runnerProcess) RegisterTools(namespace string, tools []sdk.AIToolDefinition) error {
	namespace = strings.TrimSpace(namespace)
	namespaceKey := runnerAIToolNamespaceKey(namespace)

	definitions := make([]aiToolDefinitionPayload, 0, len(tools))
	localDefs := make(map[string]sdk.AIToolDefinition, len(tools))
	for _, item := range tools {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return fmt.Errorf("AI tool name is required")
		}
		if item.Handle == nil {
			return fmt.Errorf("AI tool %s handler is required", name)
		}
		if _, exists := localDefs[name]; exists {
			return fmt.Errorf("duplicate AI tool name: %s", name)
		}
		schema := cloneAnyMap(item.InputSchema)
		definitions = append(definitions, aiToolDefinitionPayload{
			Name:        name,
			Description: strings.TrimSpace(item.Description),
			InputSchema: schema,
		})
		localDefs[name] = sdk.AIToolDefinition{
			Name:        name,
			Description: strings.TrimSpace(item.Description),
			InputSchema: schema,
			Available:   item.Available,
			Handle:      item.Handle,
		}
	}

	requestID := strconv.FormatUint(atomic.AddUint64(&p.seq, 1), 10)
	waitCh := make(chan hostResponsePayload, 1)
	p.pendingMu.Lock()
	p.pending[requestID] = waitCh
	p.pendingMu.Unlock()
	defer func() {
		p.pendingMu.Lock()
		delete(p.pending, requestID)
		p.pendingMu.Unlock()
	}()

	if err := p.sendPluginMessage("ai_tools_register", registerAIToolsPayload{
		ID:        requestID,
		Namespace: namespace,
		Tools:     definitions,
	}); err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var resp hostResponsePayload
	select {
	case resp = <-waitCh:
	case <-waitCtx.Done():
		return waitCtx.Err()
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}

	p.aiToolMu.Lock()
	if oldNames, exists := p.aiNamespaces[namespaceKey]; exists {
		for _, name := range oldNames {
			delete(p.aiTools, name)
		}
	}
	names := make([]string, 0, len(localDefs))
	for name, item := range localDefs {
		p.aiTools[name] = item
		names = append(names, name)
	}
	p.aiNamespaces[namespaceKey] = names
	p.aiToolMu.Unlock()
	return nil
}

func (p *runnerProcess) UnregisterTools(namespace string) {
	namespace = strings.TrimSpace(namespace)
	namespaceKey := runnerAIToolNamespaceKey(namespace)

	p.aiToolMu.Lock()
	if names, exists := p.aiNamespaces[namespaceKey]; exists {
		for _, name := range names {
			delete(p.aiTools, name)
		}
		delete(p.aiNamespaces, namespaceKey)
	}
	p.aiToolMu.Unlock()

	if err := p.sendPluginMessage("ai_tools_unregister", unregisterAIToolsPayload{
		Namespace: namespace,
	}); err != nil {
		p.logger.Error("注销远程 AI tools 失败", "namespace", namespace, "error", err)
	}
}

func (p *runnerProcess) callHost(ctx context.Context, method string, payload any, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}

	callID := strconv.FormatUint(atomic.AddUint64(&p.seq, 1), 10)
	waitCh := make(chan hostResponsePayload, 1)

	p.pendingMu.Lock()
	p.pending[callID] = waitCh
	p.pendingMu.Unlock()
	defer func() {
		p.pendingMu.Lock()
		delete(p.pending, callID)
		p.pendingMu.Unlock()
	}()

	rawPayload, err := marshalPayload(payload)
	if err != nil {
		return err
	}
	if err := p.sendPluginMessage("call", callPayload{
		ID:      callID,
		Method:  method,
		Payload: rawPayload,
	}); err != nil {
		return err
	}

	select {
	case resp := <-waitCh:
		if resp.Error != "" {
			return errors.New(resp.Error)
		}
		if out != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, out); err != nil {
				return fmt.Errorf("解析宿主响应失败: %w", err)
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *runnerProcess) readHostMessages(decoder *json.Decoder, eventCh chan<- event.Event, stopCh chan<- struct{}) {
	defer func() {
		select {
		case stopCh <- struct{}{}:
		default:
		}
	}()

	for {
		var msg rawHostMessage
		if err := decoder.Decode(&msg); err != nil {
			if err != io.EOF {
				p.logger.Error("读取宿主消息失败", "error", err)
			}
			return
		}

		switch msg.Type {
		case "event":
			var payload eventPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				p.logger.Error("解析事件消息失败", "error", err)
				continue
			}
			eventCh <- payload.Event
		case "response":
			var payload hostResponsePayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				p.logger.Error("解析宿主响应失败", "error", err)
				continue
			}
			p.pendingMu.Lock()
			waitCh := p.pending[payload.ID]
			p.pendingMu.Unlock()
			if waitCh != nil {
				waitCh <- payload
			}
		case "ai_tool_call":
			var payload aiToolCallPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				p.logger.Error("解析 ai_tool_call 消息失败", "error", err)
				continue
			}
			go p.handleAIToolCall(payload)
		case "stop":
			return
		default:
			p.logger.Warn("收到未知宿主消息", "type", msg.Type)
		}
	}
}

func (r runnerConfigReader) Unmarshal(target any) error {
	v := viper.New()
	for k, val := range r.raw {
		v.Set(k, val)
	}
	return v.Unmarshal(target)
}

func (r runnerConfigReader) Raw() map[string]any {
	if r.raw == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(r.raw))
	for k, v := range r.raw {
		out[k] = v
	}
	return out
}

func (c runnerCatalog) ListPlugins() []sdk.PluginInfo {
	out := make([]sdk.PluginInfo, 0, len(c.items))
	out = append(out, c.items...)
	return out
}

func newRunnerLogger(pluginID string) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})).With("plugin", pluginID, "mode", "external_runner")
}

type runnerAIToolContext struct {
	event     event.Event
	target    message.Target
	replyTo   string
	scheduled *scheduledSendPayload
}

func (c *runnerAIToolContext) Event() event.Event {
	return c.event
}

func (c *runnerAIToolContext) Target() message.Target {
	return c.target
}

func (c *runnerAIToolContext) ReplyTo() string {
	return c.replyTo
}

func (c *runnerAIToolContext) ScheduleCurrentSend(text string, reply bool) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("scheduled text is empty")
	}
	if c.scheduled != nil {
		return fmt.Errorf("scheduled message already exists")
	}
	c.scheduled = &scheduledSendPayload{
		Text:  text,
		Reply: reply,
	}
	return nil
}

func (p *runnerProcess) handleAIToolCall(payload aiToolCallPayload) {
	p.aiToolMu.RLock()
	tool, exists := p.aiTools[strings.TrimSpace(payload.ToolName)]
	p.aiToolMu.RUnlock()
	if !exists {
		if err := p.sendAIToolResult(payload.ID, nil, fmt.Errorf("unknown AI tool: %s", payload.ToolName), nil); err != nil {
			p.logger.Error("发送 ai_tool_result 失败", "tool", payload.ToolName, "error", err)
		}
		return
	}

	toolCtx := &runnerAIToolContext{
		event:   payload.Context.Event,
		target:  payload.Context.Target,
		replyTo: payload.Context.ReplyTo,
	}
	var (
		result any
		err    error
	)
	if tool.Available != nil && !tool.Available(payload.Context.Event) {
		err = fmt.Errorf("AI tool %s is unavailable for current event", payload.ToolName)
	} else {
		result, err = tool.Handle(context.Background(), toolCtx, payload.Arguments)
	}
	if sendErr := p.sendAIToolResult(payload.ID, result, err, toolCtx.scheduled); sendErr != nil {
		p.logger.Error("发送 ai_tool_result 失败", "tool", payload.ToolName, "error", sendErr)
	}
}

func (p *runnerProcess) sendAIToolResult(id string, result any, err error, scheduled *scheduledSendPayload) error {
	payload := aiToolResultPayload{ID: id}
	if err != nil {
		payload.Error = err.Error()
	} else {
		raw, marshalErr := marshalPayload(result)
		if marshalErr != nil {
			payload.Error = marshalErr.Error()
		} else {
			payload.Result = raw
			if scheduled != nil {
				copyScheduled := *scheduled
				payload.Scheduled = &copyScheduled
			}
		}
	}
	return p.sendPluginMessage("ai_tool_result", payload)
}

func runnerAIToolNamespaceKey(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return "default"
	}
	return namespace
}
