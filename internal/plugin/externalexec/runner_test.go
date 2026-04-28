package externalexec

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
	"github.com/XiaoLozee/go-bot/internal/plugin/builtin/testplugin"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

const (
	runnerHelperEnv   = "GOBOT_EXTERNAL_RUNNER_HELPER"
	runnerPluginIDEnv = "GOBOT_EXTERNAL_RUNNER_PLUGIN_ID"
)

type runnerMenuHintPlugin struct {
	messenger     sdk.Messenger
	pluginCatalog sdk.PluginCatalog
	headerText    string
}

type runnerAIToolPlugin struct {
	botAPI  sdk.BotAPI
	aiTools sdk.AIToolRegistrar
}

func newRunnerMenuHintPlugin() sdk.Plugin {
	return &runnerMenuHintPlugin{}
}

func (p *runnerMenuHintPlugin) Manifest() sdk.Manifest {
	return sdk.Manifest{ID: "menu_hint", Kind: KindExternalExec}
}

func (p *runnerMenuHintPlugin) Start(_ context.Context, env sdk.Env) error {
	p.messenger = env.Messenger
	p.pluginCatalog = env.PluginCatalog
	p.headerText = "✨ Go-bot 菜单"
	if env.Config != nil {
		if raw := env.Config.Raw(); raw != nil {
			if header, ok := raw["header_text"].(string); ok && strings.TrimSpace(header) != "" {
				p.headerText = strings.TrimSpace(header)
			}
		}
	}
	return nil
}

func (p *runnerMenuHintPlugin) Stop(context.Context) error {
	return nil
}

func (p *runnerMenuHintPlugin) HandleEvent(ctx context.Context, evt event.Event) error {
	if evt.Kind != "message" || evt.ChatType != "group" || strings.TrimSpace(evt.RawText) != "菜单" {
		return nil
	}

	var items []string
	if p.pluginCatalog != nil {
		for _, item := range p.pluginCatalog.ListPlugins() {
			items = append(items, item.ID)
		}
	}
	return p.messenger.SendText(ctx, message.Target{
		ConnectionID: evt.ConnectionID,
		ChatType:     "group",
		GroupID:      evt.GroupID,
	}, p.headerText+"\n"+strings.Join(items, ","))
}

func newRunnerAIToolPlugin() sdk.Plugin {
	return &runnerAIToolPlugin{}
}

func (p *runnerAIToolPlugin) Manifest() sdk.Manifest {
	return sdk.Manifest{ID: "ai_tool", Kind: KindExternalExec}
}

func (p *runnerAIToolPlugin) Start(_ context.Context, env sdk.Env) error {
	p.botAPI = env.BotAPI
	p.aiTools = env.AITools
	if env.AITools == nil {
		return fmt.Errorf("missing AI tool registrar")
	}
	return env.AITools.RegisterTools("demo", []sdk.AIToolDefinition{{
		Name:        "runner_echo",
		Description: "Echo context from runner plugin",
		InputSchema: map[string]any{"type": "object"},
		Handle: func(ctx context.Context, toolCtx sdk.AIToolContext, args json.RawMessage) (any, error) {
			info, err := p.botAPI.GetStrangerInfo(ctx, toolCtx.Target().ConnectionID, toolCtx.Event().UserID)
			if err != nil {
				return nil, err
			}
			if err := toolCtx.ScheduleCurrentSend("runner scheduled", true); err != nil {
				return nil, err
			}
			var payload map[string]any
			if len(args) > 0 {
				if err := json.Unmarshal(args, &payload); err != nil {
					return nil, err
				}
			}
			return map[string]any{
				"nickname":  info.Nickname,
				"reply_to":  toolCtx.ReplyTo(),
				"chat_type": toolCtx.Target().ChatType,
				"payload":   payload,
			}, nil
		},
	}})
}

func (p *runnerAIToolPlugin) Stop(context.Context) error {
	return nil
}

func (p *runnerAIToolPlugin) HandleEvent(context.Context, event.Event) error {
	return nil
}

func TestRunFactory_MenuHintLifecycle(t *testing.T) {
	cmd, stdin, decoder, stderr := startRunnerHelper(t, "menu_hint")

	if err := json.NewEncoder(stdin).Encode(hostMessage{
		Type: "start",
		Payload: startPayload{
			Plugin: sdk.Manifest{ID: "menu_hint", Kind: KindExternalExec},
			Config: map[string]any{
				"auto_generate": true,
				"header_text":   "✨ 外部菜单",
			},
			Catalog: []sdk.PluginInfo{
				{ID: "menu_hint", Name: "菜单提示", Enabled: true},
				{ID: "video_parser", Name: "短视频解析", Enabled: false},
			},
		},
	}); err != nil {
		t.Fatalf("encode start error = %v", err)
	}

	msg := decodePluginMessage(t, decoder)
	if msg.Type != "ready" {
		t.Fatalf("first message type = %q, want ready", msg.Type)
	}

	if err := json.NewEncoder(stdin).Encode(hostMessage{
		Type: "event",
		Payload: eventPayload{
			Event: event.Event{
				ConnectionID: "conn-1",
				Kind:         "message",
				ChatType:     "group",
				GroupID:      "group-1",
				RawText:      "菜单",
			},
		},
	}); err != nil {
		t.Fatalf("encode event error = %v", err)
	}

	msg = decodePluginMessage(t, decoder)
	if msg.Type != "send_text" {
		t.Fatalf("event response type = %q, want send_text", msg.Type)
	}

	var payload sendTextPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal send_text payload error = %v", err)
	}
	if payload.Target.GroupID != "group-1" {
		t.Fatalf("target group = %q, want group-1", payload.Target.GroupID)
	}
	if !strings.Contains(payload.Text, "外部菜单") || !strings.Contains(payload.Text, "menu_hint") {
		t.Fatalf("menu text = %q, want generated menu content", payload.Text)
	}

	stopRunnerHelper(t, cmd, stdin, stderr)
}

func TestRunFactory_TestPluginBotAPICall(t *testing.T) {
	cmd, stdin, decoder, stderr := startRunnerHelper(t, "test")

	if err := json.NewEncoder(stdin).Encode(hostMessage{
		Type: "start",
		Payload: startPayload{
			Plugin: sdk.Manifest{ID: "test", Kind: KindExternalExec},
		},
	}); err != nil {
		t.Fatalf("encode start error = %v", err)
	}

	msg := decodePluginMessage(t, decoder)
	if msg.Type != "ready" {
		t.Fatalf("first message type = %q, want ready", msg.Type)
	}

	if err := json.NewEncoder(stdin).Encode(hostMessage{
		Type: "event",
		Payload: eventPayload{
			Event: event.Event{
				ConnectionID: "conn-1",
				Kind:         "message",
				ChatType:     "private",
				UserID:       "user-1",
				RawText:      "测试",
			},
		},
	}); err != nil {
		t.Fatalf("encode event error = %v", err)
	}

	msg = decodePluginMessage(t, decoder)
	if msg.Type != "send_text" {
		t.Fatalf("first event response type = %q, want send_text", msg.Type)
	}
	var textPayload sendTextPayload
	if err := json.Unmarshal(msg.Payload, &textPayload); err != nil {
		t.Fatalf("unmarshal first send_text payload error = %v", err)
	}
	if textPayload.Text != "测试消息发送" {
		t.Fatalf("first send_text text = %q, want 测试消息发送", textPayload.Text)
	}

	msg = decodePluginMessage(t, decoder)
	if msg.Type != "call" {
		t.Fatalf("second event response type = %q, want call", msg.Type)
	}
	var call callPayload
	if err := json.Unmarshal(msg.Payload, &call); err != nil {
		t.Fatalf("unmarshal call payload error = %v", err)
	}
	if call.Method != CallBotGetStrangerInfo {
		t.Fatalf("call method = %q, want %q", call.Method, CallBotGetStrangerInfo)
	}

	if err := json.NewEncoder(stdin).Encode(hostMessage{
		Type: "response",
		Payload: hostResponsePayload{
			ID:     call.ID,
			Result: mustRawMessage(t, sdk.UserInfo{UserID: "user-1", Nickname: "Alice"}),
		},
	}); err != nil {
		t.Fatalf("encode response error = %v", err)
	}

	msg = decodePluginMessage(t, decoder)
	if msg.Type != "send_text" {
		t.Fatalf("third event response type = %q, want send_text", msg.Type)
	}
	if err := json.Unmarshal(msg.Payload, &textPayload); err != nil {
		t.Fatalf("unmarshal second send_text payload error = %v", err)
	}
	if textPayload.Text != "Alice" {
		t.Fatalf("second send_text text = %q, want Alice", textPayload.Text)
	}

	stopRunnerHelper(t, cmd, stdin, stderr)
}

func TestRunFactory_RemoteAIToolLifecycle(t *testing.T) {
	cmd, stdin, decoder, stderr := startRunnerHelper(t, "ai_tool")

	if err := json.NewEncoder(stdin).Encode(hostMessage{
		Type: "start",
		Payload: startPayload{
			Plugin: sdk.Manifest{ID: "ai_tool", Kind: KindExternalExec},
		},
	}); err != nil {
		t.Fatalf("encode start error = %v", err)
	}

	msg := decodePluginMessage(t, decoder)
	if msg.Type != "ai_tools_register" {
		t.Fatalf("first message type = %q, want ai_tools_register", msg.Type)
	}
	var registerPayload registerAIToolsPayload
	if err := json.Unmarshal(msg.Payload, &registerPayload); err != nil {
		t.Fatalf("unmarshal ai_tools_register payload error = %v", err)
	}
	if registerPayload.Namespace != "demo" {
		t.Fatalf("register namespace = %q, want demo", registerPayload.Namespace)
	}
	if len(registerPayload.Tools) != 1 || registerPayload.Tools[0].Name != "runner_echo" {
		t.Fatalf("register tools = %+v, want runner_echo", registerPayload.Tools)
	}

	if err := json.NewEncoder(stdin).Encode(hostMessage{
		Type: "response",
		Payload: hostResponsePayload{
			ID: registerPayload.ID,
		},
	}); err != nil {
		t.Fatalf("encode register response error = %v", err)
	}

	msg = decodePluginMessage(t, decoder)
	if msg.Type != "ready" {
		t.Fatalf("second message type = %q, want ready", msg.Type)
	}

	if err := json.NewEncoder(stdin).Encode(hostMessage{
		Type: "ai_tool_call",
		Payload: aiToolCallPayload{
			ID:       "tool-1",
			ToolName: "runner_echo",
			Arguments: mustRawMessage(t, map[string]any{
				"text": "hello",
			}),
			Context: aiToolContextPayload{
				Event: event.Event{
					ID:           "evt-1",
					ConnectionID: "conn-1",
					Kind:         "message",
					ChatType:     "group",
					UserID:       "user-1",
					GroupID:      "group-1",
				},
				Target: message.Target{
					ConnectionID: "conn-1",
					ChatType:     "group",
					GroupID:      "group-1",
				},
				ReplyTo: "msg-1",
			},
		},
	}); err != nil {
		t.Fatalf("encode ai_tool_call error = %v", err)
	}

	msg = decodePluginMessage(t, decoder)
	if msg.Type != "call" {
		t.Fatalf("tool bridge message type = %q, want call", msg.Type)
	}
	var call callPayload
	if err := json.Unmarshal(msg.Payload, &call); err != nil {
		t.Fatalf("unmarshal tool bridge call error = %v", err)
	}
	if call.Method != CallBotGetStrangerInfo {
		t.Fatalf("call method = %q, want %q", call.Method, CallBotGetStrangerInfo)
	}

	if err := json.NewEncoder(stdin).Encode(hostMessage{
		Type: "response",
		Payload: hostResponsePayload{
			ID:     call.ID,
			Result: mustRawMessage(t, sdk.UserInfo{UserID: "user-1", Nickname: "Alice"}),
		},
	}); err != nil {
		t.Fatalf("encode tool bridge response error = %v", err)
	}

	msg = decodePluginMessage(t, decoder)
	if msg.Type != "ai_tool_result" {
		t.Fatalf("final tool message type = %q, want ai_tool_result", msg.Type)
	}
	var result aiToolResultPayload
	if err := json.Unmarshal(msg.Payload, &result); err != nil {
		t.Fatalf("unmarshal ai_tool_result error = %v", err)
	}
	if result.Error != "" {
		t.Fatalf("ai_tool_result error = %q, want empty", result.Error)
	}
	if result.Scheduled == nil || result.Scheduled.Text != "runner scheduled" || !result.Scheduled.Reply {
		t.Fatalf("scheduled payload = %+v, want runner scheduled reply", result.Scheduled)
	}
	var resultMap map[string]any
	if err := json.Unmarshal(result.Result, &resultMap); err != nil {
		t.Fatalf("unmarshal tool result map error = %v", err)
	}
	if resultMap["nickname"] != "Alice" {
		t.Fatalf("tool result nickname = %#v, want Alice", resultMap["nickname"])
	}
	if resultMap["reply_to"] != "msg-1" {
		t.Fatalf("tool result reply_to = %#v, want msg-1", resultMap["reply_to"])
	}

	stopRunnerHelper(t, cmd, stdin, stderr)
}

func TestRunFactoryHelperProcess(t *testing.T) {
	if os.Getenv(runnerHelperEnv) != "1" {
		return
	}
	if err := runHelperPlugin(os.Getenv(runnerPluginIDEnv)); err != nil {
		t.Fatalf("runHelperPlugin() error = %v", err)
	}
	os.Exit(0)
}

func runHelperPlugin(pluginID string) error {
	switch pluginID {
	case "menu_hint":
		return RunFactory(pluginID, newRunnerMenuHintPlugin)
	case "ai_tool":
		return RunFactory(pluginID, newRunnerAIToolPlugin)
	case "test":
		return RunFactory(pluginID, testplugin.New)
	default:
		return fmt.Errorf("unsupported helper plugin %q", pluginID)
	}
}

func startRunnerHelper(t *testing.T, pluginID string) (*exec.Cmd, ioWriteCloser, *json.Decoder, *bytes.Buffer) {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^TestRunFactoryHelperProcess$")
	cmd.Env = append(os.Environ(),
		runnerHelperEnv+"=1",
		runnerPluginIDEnv+"="+pluginID,
	)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe() error = %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe() error = %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() error = %v", err)
	}
	return cmd, stdin, json.NewDecoder(stdout), &stderr
}

func stopRunnerHelper(t *testing.T, cmd *exec.Cmd, stdin ioWriteCloser, stderr *bytes.Buffer) {
	t.Helper()

	if err := json.NewEncoder(stdin).Encode(hostMessage{Type: "stop"}); err != nil {
		t.Fatalf("encode stop error = %v", err)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("cmd.Wait() error = %v, stderr=%s", err, stderr.String())
	}
}

func decodePluginMessage(t *testing.T, decoder *json.Decoder) pluginMessage {
	t.Helper()

	var msg pluginMessage
	if err := decoder.Decode(&msg); err != nil {
		t.Fatalf("decoder.Decode() error = %v", err)
	}
	return msg
}

type ioWriteCloser interface {
	Write([]byte) (int, error)
	Close() error
}
