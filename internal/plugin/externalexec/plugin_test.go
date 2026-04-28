package externalexec

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

const helperProcessEnv = "GO_BOT_EXTERNAL_EXEC_HELPER"
const helperProcessModeEnv = "GO_BOT_EXTERNAL_EXEC_HELPER_MODE"

type helperHostMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type messengerCall struct {
	Target message.Target
	Text   string
}

type scheduledCall struct {
	Text  string
	Reply bool
}

type botAPISpy struct{}

type messengerSpy struct {
	mu    sync.Mutex
	texts []messengerCall
}

type aiToolRegistrarSpy struct {
	mu        sync.Mutex
	providers map[string][]sdk.AIToolDefinition
}

type aiToolContextSpy struct {
	evt       event.Event
	target    message.Target
	replyTo   string
	scheduled *scheduledCall
}

func (b *botAPISpy) GetStrangerInfo(context.Context, string, string) (*sdk.UserInfo, error) {
	return &sdk.UserInfo{
		UserID:   "user-1",
		Nickname: "Alice",
	}, nil
}

func (b *botAPISpy) GetGroupInfo(context.Context, string, string) (*sdk.GroupInfo, error) {
	return &sdk.GroupInfo{
		GroupID:        "group-1",
		GroupName:      "Test Group",
		MemberCount:    3,
		MaxMemberCount: 200,
	}, nil
}

func (b *botAPISpy) GetGroupMemberList(context.Context, string, string) ([]sdk.GroupMemberInfo, error) {
	return nil, nil
}

func (b *botAPISpy) GetGroupMemberInfo(context.Context, string, string, string) (*sdk.GroupMemberInfo, error) {
	return &sdk.GroupMemberInfo{
		GroupID:  "group-1",
		UserID:   "user-1",
		Nickname: "Alice",
	}, nil
}

func (b *botAPISpy) GetMessage(context.Context, string, string) (*sdk.MessageDetail, error) {
	return &sdk.MessageDetail{
		MessageID:   "msg-1",
		MessageType: "private",
		UserID:      "user-1",
		RawMessage:  "hello",
	}, nil
}

func (b *botAPISpy) GetForwardMessage(context.Context, string, string) (*sdk.ForwardMessage, error) {
	return &sdk.ForwardMessage{
		ID: "forward-1",
		Nodes: []sdk.ForwardMessageNode{{
			UserID:   "user-1",
			Nickname: "Alice",
			Content:  []message.Segment{message.Text("hello")},
		}},
	}, nil
}

func (b *botAPISpy) DeleteMessage(context.Context, string, string) error {
	return nil
}

func (b *botAPISpy) ResolveMedia(context.Context, string, string, string) (*sdk.ResolvedMedia, error) {
	return &sdk.ResolvedMedia{URL: "https://example.com/demo.jpg"}, nil
}

func (b *botAPISpy) GetLoginInfo(context.Context, string) (*sdk.LoginInfo, error) {
	return &sdk.LoginInfo{UserID: "bot-1", Nickname: "Bot"}, nil
}

func (b *botAPISpy) GetStatus(context.Context, string) (*sdk.BotStatus, error) {
	return &sdk.BotStatus{Online: true, Good: true}, nil
}

func (b *botAPISpy) SendGroupForward(context.Context, string, string, []message.ForwardNode, message.ForwardOptions) error {
	return nil
}

func (s *messengerSpy) SendText(_ context.Context, target message.Target, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.texts = append(s.texts, messengerCall{Target: target, Text: text})
	return nil
}

func (s *messengerSpy) SendSegments(context.Context, message.Target, []message.Segment) error {
	return nil
}

func (s *messengerSpy) ReplyText(context.Context, message.Target, string, string) error {
	return nil
}

func (s *messengerSpy) waitForText(timeout time.Duration) (messengerCall, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if len(s.texts) > 0 {
			call := s.texts[0]
			s.mu.Unlock()
			return call, true
		}
		s.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	return messengerCall{}, false
}

func (r *aiToolRegistrarSpy) RegisterTools(namespace string, tools []sdk.AIToolDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.providers == nil {
		r.providers = make(map[string][]sdk.AIToolDefinition)
	}
	copied := make([]sdk.AIToolDefinition, 0, len(tools))
	copied = append(copied, tools...)
	r.providers[namespace] = copied
	return nil
}

func (r *aiToolRegistrarSpy) UnregisterTools(namespace string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, namespace)
}

func (r *aiToolRegistrarSpy) waitForTool(namespace, toolName string, timeout time.Duration) (sdk.AIToolDefinition, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		tools := append([]sdk.AIToolDefinition(nil), r.providers[namespace]...)
		r.mu.Unlock()
		for _, item := range tools {
			if item.Name == toolName {
				return item, true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return sdk.AIToolDefinition{}, false
}

func (c *aiToolContextSpy) Event() event.Event {
	return c.evt
}

func (c *aiToolContextSpy) Target() message.Target {
	return c.target
}

func (c *aiToolContextSpy) ReplyTo() string {
	return c.replyTo
}

func (c *aiToolContextSpy) ScheduleCurrentSend(text string, reply bool) error {
	if c.scheduled != nil {
		return context.DeadlineExceeded
	}
	c.scheduled = &scheduledCall{Text: text, Reply: reply}
	return nil
}

func TestPluginStartHandleEventStop(t *testing.T) {
	t.Setenv(helperProcessEnv, "1")

	plugin := New(Descriptor{
		Manifest: sdk.Manifest{
			ID:       "ext_demo",
			Name:     "External Demo",
			Version:  "0.1.0",
			Kind:     KindExternalExec,
			Entry:    os.Args[0],
			Args:     []string{"-test.run=^TestExternalExecHelperProcess$"},
			Protocol: ProtocolStdioJSONRP,
		},
		WorkDir: t.TempDir(),
	})

	spy := &messengerSpy{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := plugin.Start(context.Background(), sdk.Env{
		Logger:    logger,
		Messenger: spy,
		BotAPI:    &botAPISpy{},
		App: sdk.AppInfo{
			Name:        "go-bot",
			Environment: "test",
			OwnerQQ:     "123456789",
		},
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := plugin.HandleEvent(context.Background(), event.Event{
		ConnectionID: "conn-1",
		ChatType:     "private",
		UserID:       "user-1",
		RawText:      "ping",
	}); err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}

	call, ok := spy.waitForText(2 * time.Second)
	if !ok {
		t.Fatalf("waitForText() timed out")
	}
	if call.Text != "echo:ping|Alice" {
		t.Fatalf("message text = %q, want %q", call.Text, "echo:ping|Alice")
	}
	if call.Target.ConnectionID != "conn-1" || call.Target.ChatType != "private" || call.Target.UserID != "user-1" {
		t.Fatalf("message target = %+v, want conn-1/private/user-1", call.Target)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := plugin.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if err := plugin.HandleEvent(context.Background(), event.Event{RawText: "after-stop"}); err == nil {
		t.Fatalf("HandleEvent() after Stop() error = nil, want plugin not running")
	}
}

func TestPluginRuntimeStatusTracksUnexpectedExit(t *testing.T) {
	t.Setenv(helperProcessEnv, "1")
	t.Setenv(helperProcessModeEnv, "exit_after_ready")

	plugin, ok := New(Descriptor{
		Manifest: sdk.Manifest{
			ID:       "ext_crash",
			Name:     "External Crash",
			Version:  "0.1.0",
			Kind:     KindExternalExec,
			Entry:    os.Args[0],
			Args:     []string{"-test.run=^TestExternalExecHelperProcess$"},
			Protocol: ProtocolStdioJSONRP,
		},
		WorkDir: t.TempDir(),
	}).(*Plugin)
	if !ok {
		t.Fatalf("type assertion to *Plugin failed")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := plugin.Start(context.Background(), sdk.Env{Logger: logger}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	status, ok := waitForRuntimeStopped(plugin, 2*time.Second)
	if !ok {
		t.Fatalf("waitForRuntimeStopped() timed out")
	}
	if status.Running {
		t.Fatalf("status.Running = true, want false")
	}
	if status.ExitCode == nil || *status.ExitCode == 0 {
		t.Fatalf("exit code = %#v, want non-zero", status.ExitCode)
	}
	if !strings.Contains(status.LastError, "external_exec 进程退出") {
		t.Fatalf("last error = %q, want external exit message", status.LastError)
	}
	if len(status.RecentLogs) == 0 {
		t.Fatalf("recent logs = empty, want lifecycle logs")
	}

	foundReady := false
	for _, item := range status.RecentLogs {
		if strings.Contains(item.Message, "已就绪") {
			foundReady = true
			break
		}
	}
	if !foundReady {
		t.Fatalf("recent logs = %+v, want ready lifecycle log", status.RecentLogs)
	}
}

func TestPluginStartIncludesRecentLogsWhenExitBeforeReady(t *testing.T) {
	t.Setenv(helperProcessEnv, "1")
	t.Setenv(helperProcessModeEnv, "log_then_exit_before_ready")

	plugin, ok := New(Descriptor{
		Manifest: sdk.Manifest{
			ID:       "ext_pre_ready_crash",
			Name:     "External Pre Ready Crash",
			Version:  "0.1.0",
			Kind:     KindExternalExec,
			Entry:    os.Args[0],
			Args:     []string{"-test.run=^TestExternalExecHelperProcess$"},
			Protocol: ProtocolStdioJSONRP,
		},
		WorkDir: t.TempDir(),
	}).(*Plugin)
	if !ok {
		t.Fatalf("type assertion to *Plugin failed")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := plugin.Start(context.Background(), sdk.Env{Logger: logger})
	if err == nil {
		t.Fatalf("Start() error = nil, want pre-ready failure")
	}
	if !strings.Contains(err.Error(), "recent logs:") {
		t.Fatalf("Start() error = %v, want recent logs summary", err)
	}
	if !strings.Contains(err.Error(), "plugin start failed: boom before ready") {
		t.Fatalf("Start() error = %v, want plugin log content", err)
	}
	if !strings.Contains(err.Error(), "traceback marker before ready") {
		t.Fatalf("Start() error = %v, want stderr content", err)
	}
}

func TestPluginRegistersAndInvokesRemoteAITools(t *testing.T) {
	t.Setenv(helperProcessEnv, "1")
	t.Setenv(helperProcessModeEnv, "register_ai_tools")

	plugin := New(Descriptor{
		Manifest: sdk.Manifest{
			ID:       "ext_ai",
			Name:     "External AI",
			Version:  "0.1.0",
			Kind:     KindExternalExec,
			Entry:    os.Args[0],
			Args:     []string{"-test.run=^TestExternalExecHelperProcess$"},
			Protocol: ProtocolStdioJSONRP,
		},
		WorkDir: t.TempDir(),
	})

	registrar := &aiToolRegistrarSpy{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := plugin.Start(context.Background(), sdk.Env{
		Logger:  logger,
		BotAPI:  &botAPISpy{},
		AITools: registrar,
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() {
		_ = plugin.Stop(context.Background())
	}()

	tool, ok := registrar.waitForTool("demo", "remote_echo", 2*time.Second)
	if !ok {
		t.Fatalf("waitForTool() timed out")
	}

	toolCtx := &aiToolContextSpy{
		evt: event.Event{
			ID:           "evt-1",
			ConnectionID: "conn-1",
			Kind:         "message",
			ChatType:     "group",
			UserID:       "user-1",
			GroupID:      "group-1",
			MessageID:    "msg-1",
		},
		target: message.Target{
			ConnectionID: "conn-1",
			ChatType:     "group",
			GroupID:      "group-1",
		},
		replyTo: "msg-1",
	}
	result, err := tool.Handle(context.Background(), toolCtx, mustRawMessage(t, map[string]any{"text": "hello"}))
	if err != nil {
		t.Fatalf("tool.Handle() error = %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("tool result type = %T, want map[string]any", result)
	}
	if resultMap["echo"] != "hello" {
		t.Fatalf("tool result echo = %#v, want hello", resultMap["echo"])
	}
	if resultMap["reply_to"] != "msg-1" {
		t.Fatalf("tool result reply_to = %#v, want msg-1", resultMap["reply_to"])
	}
	if toolCtx.scheduled == nil {
		t.Fatalf("scheduled call = nil, want value")
	}
	if toolCtx.scheduled.Text != "remote scheduled" || !toolCtx.scheduled.Reply {
		t.Fatalf("scheduled call = %+v, want remote scheduled reply", toolCtx.scheduled)
	}
}

func TestExternalExecHelperProcess(t *testing.T) {
	if os.Getenv(helperProcessEnv) != "1" {
		return
	}

	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	var startMsg helperHostMessage
	if err := decoder.Decode(&startMsg); err != nil {
		t.Fatalf("decode start message error = %v", err)
	}
	if startMsg.Type != "start" {
		t.Fatalf("start message type = %q, want start", startMsg.Type)
	}

	var start startPayload
	if err := json.Unmarshal(startMsg.Payload, &start); err != nil {
		t.Fatalf("unmarshal start payload error = %v", err)
	}
	if start.Plugin.ID == "" {
		t.Fatalf("start plugin id = empty, want non-empty")
	}
	if start.Plugin.Kind != KindExternalExec {
		t.Fatalf("start plugin kind = %q, want %q", start.Plugin.Kind, KindExternalExec)
	}
	if start.App.OwnerQQ != "" && start.App.OwnerQQ != "123456789" {
		t.Fatalf("start owner qq = %q, want 123456789", start.App.OwnerQQ)
	}

	if os.Getenv(helperProcessModeEnv) == "log_then_exit_before_ready" {
		if err := encoder.Encode(pluginMessage{
			Type: "log",
			Payload: mustRawMessage(t, logPayload{
				Level:   "error",
				Message: "plugin start failed: boom before ready",
			}),
		}); err != nil {
			t.Fatalf("encode log message error = %v", err)
		}
		_, _ = os.Stderr.WriteString("traceback marker before ready\n")
		os.Exit(9)
	}

	if err := encoder.Encode(pluginMessage{
		Type:    "ready",
		Payload: mustRawMessage(t, readyPayload{Message: "helper-ready"}),
	}); err != nil {
		t.Fatalf("encode ready message error = %v", err)
	}
	if os.Getenv(helperProcessModeEnv) == "exit_after_ready" {
		os.Exit(7)
	}
	if os.Getenv(helperProcessModeEnv) == "register_ai_tools" {
		if err := encoder.Encode(pluginMessage{
			Type: "ai_tools_register",
			Payload: mustRawMessage(t, registerAIToolsPayload{
				ID:        "register-1",
				Namespace: "demo",
				Tools: []aiToolDefinitionPayload{{
					Name:        "remote_echo",
					Description: "Echo remote payload",
					InputSchema: map[string]any{"type": "object"},
				}},
			}),
		}); err != nil {
			t.Fatalf("encode ai_tools_register error = %v", err)
		}

		var responseMsg helperHostMessage
		if err := decoder.Decode(&responseMsg); err != nil {
			t.Fatalf("decode ai_tools_register response error = %v", err)
		}
		if responseMsg.Type != "response" {
			t.Fatalf("register response type = %q, want response", responseMsg.Type)
		}
		var response hostResponsePayload
		if err := json.Unmarshal(responseMsg.Payload, &response); err != nil {
			t.Fatalf("unmarshal ai_tools_register response error = %v", err)
		}
		if response.Error != "" {
			t.Fatalf("ai_tools_register response error = %q, want empty", response.Error)
		}
	}

	for {
		var msg helperHostMessage
		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				return
			}
			t.Fatalf("decode host message error = %v", err)
		}

		switch msg.Type {
		case "event":
			var payload eventPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("unmarshal event payload error = %v", err)
			}
			if err := encoder.Encode(pluginMessage{
				Type: "call",
				Payload: mustRawMessage(t, callPayload{
					ID:     "call-1",
					Method: CallBotGetStrangerInfo,
					Payload: mustRawMessage(t, getStrangerInfoPayload{
						ConnectionID: payload.Event.ConnectionID,
						UserID:       payload.Event.UserID,
					}),
				}),
			}); err != nil {
				t.Fatalf("encode call error = %v", err)
			}

			var responseMsg helperHostMessage
			if err := decoder.Decode(&responseMsg); err != nil {
				t.Fatalf("decode response message error = %v", err)
			}
			if responseMsg.Type != "response" {
				t.Fatalf("response message type = %q, want response", responseMsg.Type)
			}
			var response hostResponsePayload
			if err := json.Unmarshal(responseMsg.Payload, &response); err != nil {
				t.Fatalf("unmarshal response payload error = %v", err)
			}
			if response.Error != "" {
				t.Fatalf("host response error = %q, want empty", response.Error)
			}
			var info sdk.UserInfo
			if err := json.Unmarshal(response.Result, &info); err != nil {
				t.Fatalf("unmarshal user info error = %v", err)
			}
			if err := encoder.Encode(pluginMessage{
				Type: "send_text",
				Payload: mustRawMessage(t, sendTextPayload{
					Target: message.Target{
						ConnectionID: payload.Event.ConnectionID,
						ChatType:     payload.Event.ChatType,
						UserID:       payload.Event.UserID,
						GroupID:      payload.Event.GroupID,
					},
					Text: "echo:" + payload.Event.RawText + "|" + info.Nickname,
				}),
			}); err != nil {
				t.Fatalf("encode send_text error = %v", err)
			}
		case "ai_tool_call":
			var payload aiToolCallPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				t.Fatalf("unmarshal ai_tool_call payload error = %v", err)
			}
			if payload.ToolName != "remote_echo" {
				t.Fatalf("tool name = %q, want remote_echo", payload.ToolName)
			}
			var args map[string]any
			if len(payload.Arguments) > 0 {
				if err := json.Unmarshal(payload.Arguments, &args); err != nil {
					t.Fatalf("unmarshal ai_tool_call args error = %v", err)
				}
			}
			if err := encoder.Encode(pluginMessage{
				Type: "ai_tool_result",
				Payload: mustRawMessage(t, aiToolResultPayload{
					ID: payload.ID,
					Result: mustRawMessage(t, map[string]any{
						"echo":     args["text"],
						"reply_to": payload.Context.ReplyTo,
					}),
					Scheduled: &scheduledSendPayload{
						Text:  "remote scheduled",
						Reply: true,
					},
				}),
			}); err != nil {
				t.Fatalf("encode ai_tool_result error = %v", err)
			}
		case "stop":
			return
		}
	}
}

func mustRawMessage(t *testing.T, payload any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return data
}

func waitForRuntimeStopped(plugin *Plugin, timeout time.Duration) (sdk.RuntimeStatus, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status := plugin.RuntimeStatus()
		if !status.Running {
			return status, true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return sdk.RuntimeStatus{}, false
}

func TestResolvePythonCommand_PrefersUV(t *testing.T) {
	manifest := sdk.Manifest{Entry: "main.py", Args: []string{"--demo"}}
	launcher, args, env, err := resolvePythonCommand(manifest, t.TempDir(), func(name string) (string, error) {
		if name == "uv" {
			return "/usr/bin/uv", nil
		}
		return "", os.ErrNotExist
	})
	if err != nil {
		t.Fatalf("resolvePythonCommand() error = %v", err)
	}
	if launcher != "/usr/bin/uv" {
		t.Fatalf("launcher = %q, want /usr/bin/uv", launcher)
	}
	if strings.Join(args, " ") != "run python -X utf8 main.py --demo" {
		t.Fatalf("args = %#v", args)
	}
	if len(env) != 0 {
		t.Fatalf("env = %#v, want empty", env)
	}
}

func TestResolvePythonCommand_FallsBackWithoutUV(t *testing.T) {
	manifest := sdk.Manifest{Entry: "main.py", Args: []string{"--demo"}}
	launcher, args, _, err := resolvePythonCommand(manifest, t.TempDir(), func(name string) (string, error) {
		switch name {
		case "python3":
			return "/usr/bin/python3", nil
		case "py":
			return "py", nil
		default:
			return "", os.ErrNotExist
		}
	})
	if err != nil {
		t.Fatalf("resolvePythonCommand() error = %v", err)
	}
	if runtime.GOOS == "windows" {
		if launcher != "py" {
			t.Fatalf("launcher = %q, want py", launcher)
		}
		if strings.Join(args, " ") != "-3 -X utf8 main.py --demo" {
			t.Fatalf("args = %#v", args)
		}
		return
	}
	if launcher != "/usr/bin/python3" {
		t.Fatalf("launcher = %q, want /usr/bin/python3", launcher)
	}
	if strings.Join(args, " ") != "-X utf8 main.py --demo" {
		t.Fatalf("args = %#v", args)
	}
}

func TestResolvePythonCommand_UsesConfiguredVenv(t *testing.T) {
	root := t.TempDir()
	venvRoot := filepath.Join(root, "data", "plugin-envs", "demo")
	binDir := filepath.Join(venvRoot, "bin")
	pythonPath := filepath.Join(binDir, "python")
	if runtime.GOOS == "windows" {
		binDir = filepath.Join(venvRoot, "Scripts")
		pythonPath = filepath.Join(binDir, "python.exe")
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	if err := os.WriteFile(pythonPath, []byte(""), 0o755); err != nil {
		t.Fatalf("WriteFile(python) error = %v", err)
	}

	manifest := sdk.Manifest{
		ID:        "demo",
		Entry:     "main.py",
		Args:      []string{"--demo"},
		PythonEnv: venvRoot,
	}
	launcher, args, env, err := resolvePythonCommand(manifest, filepath.Join(root, "plugins", "demo"), func(string) (string, error) {
		return "", os.ErrNotExist
	})
	if err != nil {
		t.Fatalf("resolvePythonCommand() error = %v", err)
	}
	if launcher != pythonPath {
		t.Fatalf("launcher = %q, want %q", launcher, pythonPath)
	}
	if strings.Join(args, " ") != "-X utf8 main.py --demo" {
		t.Fatalf("args = %#v", args)
	}
	if !containsEnvValue(env, "VIRTUAL_ENV="+venvRoot) {
		t.Fatalf("env = %#v, want VIRTUAL_ENV", env)
	}
}

func TestResolvePythonCommonPath_FindsParentCommon(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "menu_hint")
	commonDir := filepath.Join(root, "_common")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(plugin) error = %v", err)
	}
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(common) error = %v", err)
	}
	if err := writePythonCommonRuntime(commonDir); err != nil {
		t.Fatalf("writePythonCommonRuntime() error = %v", err)
	}

	got, err := resolvePythonCommonPath(pluginDir)
	if err != nil {
		t.Fatalf("resolvePythonCommonPath() error = %v", err)
	}
	if got != commonDir {
		t.Fatalf("common path = %q, want %q", got, commonDir)
	}
}

func TestResolvePythonCommonPath_FindsHostPluginsCommonFromAncestor(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "data", "plugins", "menu_hint")
	commonDir := filepath.Join(root, "plugins", "_common")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(plugin) error = %v", err)
	}
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(common) error = %v", err)
	}
	if err := writePythonCommonRuntime(commonDir); err != nil {
		t.Fatalf("writePythonCommonRuntime() error = %v", err)
	}

	got, err := resolvePythonCommonPath(pluginDir)
	if err != nil {
		t.Fatalf("resolvePythonCommonPath() error = %v", err)
	}
	if got != commonDir {
		t.Fatalf("common path = %q, want %q", got, commonDir)
	}
}

func TestResolvePythonCommonPath_SkipsIncompletePluginLocalCommon(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugins", "menu_hint")
	localCommonDir := filepath.Join(pluginDir, "_common")
	hostCommonDir := filepath.Join(root, "plugins", "_common")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(plugin) error = %v", err)
	}
	if err := os.MkdirAll(localCommonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(local common) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(localCommonDir, "gobot_runtime.py"), []byte("# runtime"), 0o644); err != nil {
		t.Fatalf("WriteFile(local runtime) error = %v", err)
	}
	if err := os.MkdirAll(hostCommonDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(host common) error = %v", err)
	}
	if err := writePythonCommonRuntime(hostCommonDir); err != nil {
		t.Fatalf("writePythonCommonRuntime() error = %v", err)
	}

	got, err := resolvePythonCommonPath(pluginDir)
	if err != nil {
		t.Fatalf("resolvePythonCommonPath() error = %v", err)
	}
	if got != hostCommonDir {
		t.Fatalf("common path = %q, want %q", got, hostCommonDir)
	}
}

func writePythonCommonRuntime(commonDir string) error {
	if err := os.WriteFile(filepath.Join(commonDir, "gobot_runtime.py"), []byte("# runtime"), 0o644); err != nil {
		return err
	}
	packageDir := filepath.Join(commonDir, "gobot_plugin")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(packageDir, "__init__.py"), []byte("# package init"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(packageDir, "models.py"), []byte("# models"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(packageDir, "runtime.py"), []byte("# package runtime"), 0o644); err != nil {
		return err
	}
	return nil
}

func containsEnvValue(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
