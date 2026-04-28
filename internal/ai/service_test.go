package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

type stubGenerator struct {
	text      string
	err       error
	last      []chatMessage
	history   [][]chatMessage
	lastTools []toolSpec
	turns     []turnResult
	calls     int
}

func (s *stubGenerator) RunTurn(_ context.Context, messages []chatMessage, tools []toolSpec, _ config.AIConfig) (turnResult, error) {
	s.calls++
	s.last = append([]chatMessage(nil), messages...)
	s.history = append(s.history, append([]chatMessage(nil), messages...))
	s.lastTools = append([]toolSpec(nil), tools...)
	if s.err != nil {
		return turnResult{}, s.err
	}
	if index := s.calls - 1; index >= 0 && index < len(s.turns) {
		return s.turns[index], nil
	}
	return turnResult{Text: s.text}, nil
}

func TestBuildPromptMessages_PreservesAssistantReasoningContent(t *testing.T) {
	messages := buildPromptMessages(testAIConfig(), promptContext{
		session: SessionState{
			Recent: []ConversationMessage{
				{
					Role:             "assistant",
					Text:             "上一轮回复",
					ReasoningContent: "上一轮思考",
				},
				{
					Role:   "user",
					UserID: "20001",
					Text:   "现在继续聊",
				},
			},
		},
		plan: ReplyPlan{ReplyMode: "direct"},
	})
	found := false
	for _, item := range messages {
		if item.Role != "assistant" {
			continue
		}
		found = true
		if got := strings.TrimSpace(item.ReasoningContent); got != "上一轮思考" {
			t.Fatalf("assistant reasoning_content = %q, want %q", got, "上一轮思考")
		}
	}
	if !found {
		t.Fatalf("assistant history message not found in prompt")
	}
}

func TestRunToolLoop_PreservesReasoningContentAcrossToolHop(t *testing.T) {
	gen := &stubGenerator{
		turns: []turnResult{
			{
				ReasoningContent: "先查一下资料",
				ToolCalls: []toolCall{{
					ID:        "tool-1",
					Name:      "lookup",
					Arguments: json.RawMessage(`{}`),
				}},
			},
			{
				Text: "done",
			},
		},
	}
	service := &Service{}
	_, err := service.runToolLoop(
		context.Background(),
		gen,
		testAIConfig(),
		[]chatMessage{{Role: "user", Content: "hello"}},
		[]toolDefinition{{
			Spec: toolSpec{Name: "lookup"},
			Handler: func(ctx context.Context, exec *toolExecutionContext, args json.RawMessage) (any, error) {
				return map[string]any{"ok": true}, nil
			},
		}},
		&toolExecutionContext{},
	)
	if err != nil {
		t.Fatalf("runToolLoop() error = %v", err)
	}
	if len(gen.history) != 2 {
		t.Fatalf("generator history calls = %d, want 2", len(gen.history))
	}
	found := false
	for _, item := range gen.history[1] {
		if item.Role != "assistant" {
			continue
		}
		found = true
		if got := strings.TrimSpace(item.ReasoningContent); got != "先查一下资料" {
			t.Fatalf("assistant reasoning_content = %q, want %q", got, "先查一下资料")
		}
	}
	if !found {
		t.Fatalf("assistant tool-call message not found in second hop conversation")
	}
}

func TestBuildOpenAIWireMessages_PreservesAssistantReasoningContent(t *testing.T) {
	wire := buildOpenAIWireMessages([]chatMessage{{
		Role:             "assistant",
		Content:          "answer",
		ReasoningContent: "thinking",
	}})
	if len(wire) != 1 {
		t.Fatalf("wire messages = %d, want 1", len(wire))
	}
	if got := strings.TrimSpace(wire[0].ReasoningContent); got != "thinking" {
		t.Fatalf("wire reasoning_content = %q, want %q", got, "thinking")
	}
}

func TestApplyOpenAIThinkingControlUsesOpenAIFormat(t *testing.T) {
	payload := openAIChatRequest{}
	sent := applyOpenAIThinkingControl(&payload, config.AIReplyConfig{
		ThinkingMode:   "high",
		ThinkingFormat: "openai",
	})

	if !sent {
		t.Fatal("applyOpenAIThinkingControl() sent = false, want true")
	}
	if payload.Thinking == nil || payload.Thinking.Type != "enabled" {
		t.Fatalf("Thinking = %+v, want enabled", payload.Thinking)
	}
	if payload.ReasoningEffort != "high" {
		t.Fatalf("ReasoningEffort = %q, want high", payload.ReasoningEffort)
	}
	if payload.OutputConfig != nil {
		t.Fatalf("OutputConfig = %+v, want nil for OpenAI format", payload.OutputConfig)
	}
}

func TestApplyOpenAIThinkingControlUsesAnthropicFormat(t *testing.T) {
	payload := openAIChatRequest{}
	sent := applyOpenAIThinkingControl(&payload, config.AIReplyConfig{
		ThinkingMode:   "xhigh",
		ThinkingFormat: "anthropic",
	})

	if !sent {
		t.Fatal("applyOpenAIThinkingControl() sent = false, want true")
	}
	if payload.Thinking == nil || payload.Thinking.Type != "enabled" {
		t.Fatalf("Thinking = %+v, want enabled", payload.Thinking)
	}
	if payload.OutputConfig == nil || payload.OutputConfig.Effort != "max" {
		t.Fatalf("OutputConfig = %+v, want max", payload.OutputConfig)
	}
	if payload.ReasoningEffort != "" {
		t.Fatalf("ReasoningEffort = %q, want empty for Anthropic format", payload.ReasoningEffort)
	}
}

func TestApplyOpenAIThinkingControlAutoOmitsThinking(t *testing.T) {
	payload := openAIChatRequest{}
	sent := applyOpenAIThinkingControl(&payload, config.AIReplyConfig{
		ThinkingMode:   "auto",
		ThinkingFormat: "openai",
	})

	if sent {
		t.Fatal("applyOpenAIThinkingControl() sent = true, want false for auto")
	}
	if payload.Thinking != nil || payload.ReasoningEffort != "" || payload.OutputConfig != nil {
		t.Fatalf("auto thinking should omit controls, got thinking=%+v reasoning=%q output=%+v", payload.Thinking, payload.ReasoningEffort, payload.OutputConfig)
	}
}

func TestApplyOpenAIThinkingControlCanDisableThinking(t *testing.T) {
	payload := openAIChatRequest{}
	applyOpenAIThinkingControl(&payload, config.AIReplyConfig{
		ThinkingMode:   "disabled",
		ThinkingEffort: "max",
	})

	if payload.Thinking == nil || payload.Thinking.Type != "disabled" {
		t.Fatalf("Thinking = %+v, want disabled", payload.Thinking)
	}
	if payload.ReasoningEffort != "" || payload.OutputConfig != nil {
		t.Fatalf("thinking effort controls should be omitted when disabled, got reasoning=%q output=%+v", payload.ReasoningEffort, payload.OutputConfig)
	}
}

func TestApplyOpenAIThinkingControlKeepsDisabledThinking(t *testing.T) {
	payload := openAIChatRequest{}
	applyOpenAIThinkingControl(&payload, config.AIReplyConfig{
		ThinkingMode:   "disabled",
		ThinkingEffort: "high",
		ThinkingFormat: "openai",
	})

	if payload.Thinking == nil || payload.Thinking.Type != "disabled" {
		t.Fatalf("Thinking = %+v, want disabled", payload.Thinking)
	}
	if payload.ReasoningEffort != "" || payload.OutputConfig != nil {
		t.Fatalf("thinking effort controls should be omitted when disabled, got reasoning=%q output=%+v", payload.ReasoningEffort, payload.OutputConfig)
	}
}

func TestChatCompletionRetriesWithoutThinkingWhenProviderRejectsThinking(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if attempts == 1 {
			if _, ok := payload["thinking"]; !ok {
				t.Errorf("first request missing thinking control")
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"unknown parameter: thinking"}}`)
			return
		}
		if _, ok := payload["thinking"]; ok {
			t.Errorf("fallback request still includes thinking control: %#v", payload["thinking"])
		}
		if _, ok := payload["reasoning_effort"]; ok {
			t.Errorf("fallback request still includes reasoning_effort: %#v", payload["reasoning_effort"])
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"fallback answer"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := &openAICompatibleClient{httpClient: server.Client(), baseURL: server.URL, model: "test-model"}
	result, err := client.chatCompletion(
		context.Background(),
		[]chatMessage{{Role: "user", Content: "hello"}},
		nil,
		config.AIProviderConfig{},
		160,
		&config.AIReplyConfig{ThinkingMode: "high", ThinkingFormat: "openai"},
	)
	if err != nil {
		t.Fatalf("chatCompletion() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if result.Text != "fallback answer" {
		t.Fatalf("result.Text = %q, want fallback answer", result.Text)
	}
}

func TestChatCompletionRetriesWithoutThinkingWhenThinkingConsumesResponse(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if attempts == 1 {
			if _, ok := payload["thinking"]; !ok {
				t.Errorf("first request missing thinking control")
			}
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"","reasoning_content":"still thinking"},"finish_reason":"length"}]}`)
			return
		}
		thinking, ok := payload["thinking"].(map[string]any)
		if !ok {
			t.Errorf("fallback request missing disabled thinking control: %#v", payload["thinking"])
		} else if thinking["type"] != "disabled" {
			t.Errorf("fallback thinking type = %#v, want disabled", thinking["type"])
		}
		if _, ok := payload["reasoning_effort"]; ok {
			t.Errorf("fallback request still includes reasoning_effort: %#v", payload["reasoning_effort"])
		}
		if _, ok := payload["output_config"]; ok {
			t.Errorf("fallback request still includes output_config: %#v", payload["output_config"])
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"final answer"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := &openAICompatibleClient{httpClient: server.Client(), baseURL: server.URL, model: "test-model"}
	result, err := client.chatCompletion(
		context.Background(),
		[]chatMessage{{Role: "user", Content: "hello"}},
		nil,
		config.AIProviderConfig{},
		160,
		&config.AIReplyConfig{ThinkingMode: "high", ThinkingFormat: "openai"},
	)
	if err != nil {
		t.Fatalf("chatCompletion() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if result.Text != "final answer" {
		t.Fatalf("result.Text = %q, want final answer", result.Text)
	}
}

func TestChatCompletionAutoRetriesWithDisabledWhenProviderDefaultThinkingConsumesResponse(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if attempts == 1 {
			if _, ok := payload["thinking"]; ok {
				t.Errorf("auto request should omit thinking control: %#v", payload["thinking"])
			}
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"","reasoning_content":"provider default thinking"},"finish_reason":"length"}]}`)
			return
		}
		thinking, ok := payload["thinking"].(map[string]any)
		if !ok {
			t.Errorf("fallback request missing disabled thinking control: %#v", payload["thinking"])
		} else if thinking["type"] != "disabled" {
			t.Errorf("fallback thinking type = %#v, want disabled", thinking["type"])
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"auto fallback answer"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	client := &openAICompatibleClient{httpClient: server.Client(), baseURL: server.URL, model: "test-model"}
	result, err := client.chatCompletion(
		context.Background(),
		[]chatMessage{{Role: "user", Content: "hello"}},
		nil,
		config.AIProviderConfig{},
		160,
		&config.AIReplyConfig{ThinkingMode: "auto", ThinkingFormat: "openai"},
	)
	if err != nil {
		t.Fatalf("chatCompletion() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if result.Text != "auto fallback answer" {
		t.Fatalf("result.Text = %q, want auto fallback answer", result.Text)
	}
}

func TestInjectToolGuidanceMessageListsAvailableTools(t *testing.T) {
	messages := []chatMessage{
		{Role: "system", Content: "base prompt"},
		{Role: "user", Content: "draw a cat"},
	}
	tools := []toolDefinition{
		{Spec: toolSpec{Name: "generate_image", Description: "Generate an image from a prompt."}},
		{Spec: toolSpec{Name: "send_group_msg", Description: "Send a group message."}},
	}

	out := injectToolGuidanceMessage(messages, tools)
	if len(out) != 3 {
		t.Fatalf("messages = %d, want 3", len(out))
	}
	if out[1].Role != "system" {
		t.Fatalf("guidance role = %q, want system", out[1].Role)
	}
	if out[2].Role != "user" || out[2].Content != "draw a cat" {
		t.Fatalf("last message = %+v, want original user message", out[2])
	}
	content := fmt.Sprint(out[1].Content)
	for _, want := range []string{"当前可用工具", "generate_image", "Generate an image", "send_group_msg", "应优先调用匹配工具"} {
		if !strings.Contains(content, want) {
			t.Fatalf("guidance content should contain %q, got %q", want, content)
		}
	}
}

func TestParseOpenAIToolCalls_RepairsCommonMalformedArguments(t *testing.T) {
	calls, err := parseOpenAIToolCalls([]openAIWireToolCall{
		{
			ID: "call-loose-object",
			Function: openAIWireToolFunction{
				Name:      "generate_image",
				Arguments: `"prompt": "cat", n: 1,`,
			},
		},
		{
			ID: "call-fenced-json",
			Function: openAIWireToolFunction{
				Name: "generate_image",
				Arguments: "```json\n" +
					`{"prompt": "dog", "size": "1024x1024",}` +
					"\n```",
			},
		},
		{
			ID: "call-loose-prompt",
			Function: openAIWireToolFunction{
				Name:      "generate_image",
				Arguments: `prompt: cherry blossom cat, n: 1`,
			},
		},
		{
			ID: "call-loose-message-id",
			Function: openAIWireToolFunction{
				Name:      "get_message_detail",
				Arguments: `message_id: msg-123`,
			},
		},
		{
			ID: "call-bare-message-id",
			Function: openAIWireToolFunction{
				Name:      "get_message_detail",
				Arguments: `msg-456`,
			},
		},
		{
			ID: "call-numeric-message-id",
			Function: openAIWireToolFunction{
				Name:      "get_message_detail",
				Arguments: `{"message_id": 298445476}`,
			},
		},
	})
	if err != nil {
		t.Fatalf("parseOpenAIToolCalls() error = %v", err)
	}
	if len(calls) != 6 {
		t.Fatalf("calls count = %d, want 6", len(calls))
	}

	var first map[string]any
	if err := json.Unmarshal(calls[0].Arguments, &first); err != nil {
		t.Fatalf("unmarshal first arguments error = %v, raw=%s", err, string(calls[0].Arguments))
	}
	if first["prompt"] != "cat" || first["n"].(float64) != 1 {
		t.Fatalf("first arguments = %+v, want repaired prompt and n", first)
	}

	var second map[string]any
	if err := json.Unmarshal(calls[1].Arguments, &second); err != nil {
		t.Fatalf("unmarshal second arguments error = %v, raw=%s", err, string(calls[1].Arguments))
	}
	if second["prompt"] != "dog" || second["size"] != "1024x1024" {
		t.Fatalf("second arguments = %+v, want repaired fenced JSON", second)
	}

	var third map[string]any
	if err := json.Unmarshal(calls[2].Arguments, &third); err != nil {
		t.Fatalf("unmarshal third arguments error = %v, raw=%s", err, string(calls[2].Arguments))
	}
	if third["prompt"] != "cherry blossom cat" {
		t.Fatalf("third arguments = %+v, want prompt fallback", third)
	}

	var fourth map[string]any
	if err := json.Unmarshal(calls[3].Arguments, &fourth); err != nil {
		t.Fatalf("unmarshal fourth arguments error = %v, raw=%s", err, string(calls[3].Arguments))
	}
	if fourth["message_id"] != "msg-123" {
		t.Fatalf("fourth arguments = %+v, want repaired message_id", fourth)
	}

	var fifth map[string]any
	if err := json.Unmarshal(calls[4].Arguments, &fifth); err != nil {
		t.Fatalf("unmarshal fifth arguments error = %v, raw=%s", err, string(calls[4].Arguments))
	}
	if fifth["message_id"] != "msg-456" {
		t.Fatalf("fifth arguments = %+v, want bare message_id fallback", fifth)
	}

	var sixth map[string]any
	if err := json.Unmarshal(calls[5].Arguments, &sixth); err != nil {
		t.Fatalf("unmarshal sixth arguments error = %v, raw=%s", err, string(calls[5].Arguments))
	}
	if sixth["message_id"] != "298445476" {
		t.Fatalf("sixth arguments = %+v, want numeric message_id coerced to string", sixth)
	}
}

type sentRecord struct {
	target  message.Target
	replyTo string
	text    string
}

type segmentRecord struct {
	target   message.Target
	segments []message.Segment
}

type forwardRecord struct {
	connectionID string
	groupID      string
	nodes        []message.ForwardNode
	options      message.ForwardOptions
}

type stubMessenger struct {
	replies         []sentRecord
	sends           []sentRecord
	segments        []segmentRecord
	forwards        []forwardRecord
	strangerInfo    *sdk.UserInfo
	groupInfo       *sdk.GroupInfo
	groupMembers    []sdk.GroupMemberInfo
	groupMemberInfo *sdk.GroupMemberInfo
	loginInfo       *sdk.LoginInfo
	status          *sdk.BotStatus
}

func (m *stubMessenger) SendText(_ context.Context, target message.Target, text string) error {
	m.sends = append(m.sends, sentRecord{target: target, text: text})
	return nil
}

func (m *stubMessenger) SendSegments(_ context.Context, target message.Target, segments []message.Segment) error {
	m.segments = append(m.segments, segmentRecord{target: target, segments: append([]message.Segment(nil), segments...)})
	return nil
}

func (m *stubMessenger) ReplyText(_ context.Context, target message.Target, replyTo string, text string) error {
	m.replies = append(m.replies, sentRecord{target: target, replyTo: replyTo, text: text})
	return nil
}

func (m *stubMessenger) SendGroupForward(_ context.Context, connectionID, groupID string, nodes []message.ForwardNode, opts message.ForwardOptions) error {
	m.forwards = append(m.forwards, forwardRecord{
		connectionID: connectionID,
		groupID:      groupID,
		nodes:        append([]message.ForwardNode(nil), nodes...),
		options:      opts,
	})
	return nil
}

func (m *stubMessenger) ResolveMedia(_ context.Context, _, _, file string) (*adapter.ResolvedMedia, error) {
	return nil, fmt.Errorf("unexpected ResolveMedia call: %s", file)
}

func (m *stubMessenger) GetStrangerInfo(_ context.Context, _, _ string) (*sdk.UserInfo, error) {
	if m.strangerInfo == nil {
		return nil, fmt.Errorf("unexpected GetStrangerInfo call")
	}
	return m.strangerInfo, nil
}

func (m *stubMessenger) GetGroupInfo(_ context.Context, _, _ string) (*sdk.GroupInfo, error) {
	if m.groupInfo == nil {
		return nil, fmt.Errorf("unexpected GetGroupInfo call")
	}
	return m.groupInfo, nil
}

func (m *stubMessenger) GetGroupMemberInfo(_ context.Context, _, _, _ string) (*sdk.GroupMemberInfo, error) {
	if m.groupMemberInfo == nil {
		return nil, fmt.Errorf("unexpected GetGroupMemberInfo call")
	}
	return m.groupMemberInfo, nil
}

func (m *stubMessenger) GetGroupMemberList(_ context.Context, _, _ string) ([]sdk.GroupMemberInfo, error) {
	if m.groupMembers == nil {
		return nil, fmt.Errorf("unexpected GetGroupMemberList call")
	}
	return append([]sdk.GroupMemberInfo(nil), m.groupMembers...), nil
}

func (m *stubMessenger) GetLoginInfo(_ context.Context, _ string) (*sdk.LoginInfo, error) {
	if m.loginInfo == nil {
		return nil, fmt.Errorf("unexpected GetLoginInfo call")
	}
	return m.loginInfo, nil
}

func (m *stubMessenger) GetStatus(_ context.Context, _ string) (*sdk.BotStatus, error) {
	if m.status == nil {
		return nil, fmt.Errorf("unexpected GetStatus call")
	}
	return m.status, nil
}

type stubVisionGenerator struct {
	text       string
	err        error
	lastPrompt string
	lastImages []visionImageInput
}

func (s *stubVisionGenerator) Describe(_ context.Context, prompt string, images []visionImageInput, _ config.AIProviderConfig, _ int) (string, error) {
	s.lastPrompt = prompt
	s.lastImages = append([]visionImageInput(nil), images...)
	if s.err != nil {
		return "", s.err
	}
	return s.text, nil
}

type stubCLICommandRunner struct {
	result       cliCommandResult
	err          error
	lastCommand  string
	lastArgs     []string
	lastMaxBytes int
	calls        int
}

func (s *stubCLICommandRunner) Run(_ context.Context, name string, args []string, maxOutputBytes int) (cliCommandResult, error) {
	s.calls++
	s.lastCommand = name
	s.lastArgs = append([]string(nil), args...)
	s.lastMaxBytes = maxOutputBytes
	if s.err != nil {
		return cliCommandResult{}, s.err
	}
	return s.result, nil
}

func TestServiceMessageStore_LazilyReconnectsAfterInitialStorageFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	storage := testStorageConfig()
	storage.SQLite.Path = t.TempDir()

	service, err := NewService(testAIConfig(), storage, logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if service.store != nil {
		t.Fatalf("service.store is initialized, want initial storage failure")
	}

	storage.SQLite.Path = ":memory:"
	service.storageCfg = storage
	store, err := service.messageStore(context.Background())
	if err != nil {
		t.Fatalf("messageStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if store == nil || !service.Snapshot().StoreReady {
		t.Fatalf("store = %#v, StoreReady = %v; want lazy reconnect", store, service.Snapshot().StoreReady)
	}
	if got := service.Snapshot().LastDecisionReason; got != "AI 存储连接已恢复" {
		t.Fatalf("LastDecisionReason = %q, want recovery message", got)
	}
}

func TestServiceHandleEvent_RepliesWhenMentioned(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gen := &stubGenerator{text: "收到，这里是 AI 回复"}
	service.generator = gen

	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-1",
		Timestamp:    time.Unix(1710000000, 0),
		Segments: []message.Segment{
			message.At("123456789"),
			message.Text(" 你在吗？"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	})

	if len(messenger.replies) != 1 {
		t.Fatalf("reply count = %d, want 1", len(messenger.replies))
	}
	if got := messenger.replies[0].text; got != "收到，这里是 AI 回复" {
		t.Fatalf("reply text = %q, want AI response", got)
	}
	if len(gen.last) == 0 {
		t.Fatalf("generator received no prompt messages")
	}
	if snapshot := service.Snapshot(); !snapshot.Ready || snapshot.LastReplyAt.IsZero() {
		t.Fatalf("snapshot = %+v, want ready snapshot with last reply time", snapshot)
	}
}

func TestServiceHandleEvent_AmbientChatCanJoinGroupConversation(t *testing.T) {
	cfg := testAIConfig()
	cfg.Reply.CooldownSeconds = 0
	cfg.Proactive = config.AIProactiveConfig{
		Enabled:             true,
		MinIntervalSeconds:  30,
		DailyLimitPerGroup:  10,
		Probability:         1,
		MinRecentMessages:   1,
		RecentWindowSeconds: 600,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.generator = &stubGenerator{text: "我也觉得这个说法挺有意思"}

	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-ambient-1",
		Timestamp:    time.Unix(1710000000, 0),
		Segments: []message.Segment{
			message.Text("这个梗突然就合理了"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	})

	if len(messenger.replies) != 0 {
		t.Fatalf("reply count = %d, want 0", len(messenger.replies))
	}
	if len(messenger.sends) != 1 {
		t.Fatalf("send count = %d, want 1", len(messenger.sends))
	}
	if got := messenger.sends[0].text; got != "我也觉得这个说法挺有意思" {
		t.Fatalf("send text = %q, want ambient response", got)
	}
}

func TestServiceHandleEvent_AmbientChatEmptyResponseDoesNotSetError(t *testing.T) {
	cfg := testAIConfig()
	cfg.Reply.CooldownSeconds = 0
	cfg.Proactive = config.AIProactiveConfig{
		Enabled:             true,
		MinIntervalSeconds:  30,
		DailyLimitPerGroup:  10,
		Probability:         1,
		MinRecentMessages:   1,
		RecentWindowSeconds: 600,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.generator = &stubGenerator{text: ""}

	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-ambient-empty-1",
		Timestamp:    time.Unix(1710000000, 0),
		Segments: []message.Segment{
			message.Text("这个梗突然就合理了"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	})

	if len(messenger.replies) != 0 || len(messenger.sends) != 0 {
		t.Fatalf("messages sent replies=%d sends=%d, want none", len(messenger.replies), len(messenger.sends))
	}
	if snapshot := service.Snapshot(); snapshot.LastError == "AI 返回内容为空" {
		t.Fatalf("LastError = %q, want no empty-response error for ambient chat", snapshot.LastError)
	}
}

func TestSplitOutboundMessages_SplitsCasualOnly(t *testing.T) {
	cfg := testAIConfig()
	cfg.Reply.Split = config.AIReplySplitConfig{
		Enabled:    true,
		OnlyCasual: true,
		MaxChars:   12,
		MaxParts:   3,
		DelayMS:    0,
	}
	text := "我懂你意思。这个点确实有点好笑！先这样吧。"

	casual := splitOutboundMessages(text, ReplyPlan{ReplyMode: "banter"}, cfg)
	if len(casual) != 3 {
		t.Fatalf("casual split count = %d, want 3: %#v", len(casual), casual)
	}
	direct := splitOutboundMessages(text, ReplyPlan{ReplyMode: "direct_answer"}, cfg)
	if len(direct) != 1 || direct[0] != text {
		t.Fatalf("direct split = %#v, want original single message", direct)
	}
	cqText := "[CQ:image,file=test.png]这个点确实有点好笑！先这样吧。"
	cq := splitOutboundMessages(cqText, ReplyPlan{ReplyMode: "banter"}, cfg)
	if len(cq) != 1 || cq[0] != cqText {
		t.Fatalf("CQ split = %#v, want original single message", cq)
	}
}

func TestServiceHandleEvent_IncludesEnabledPromptSkills(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gen := &stubGenerator{text: "技能上下文已注入"}
	service.generator = gen
	service.SetPromptSkills([]PromptSkill{{
		ID:          "demo-skill",
		Name:        "Demo Skill",
		Description: "Prompt skill for testing.",
		Content:     "# Demo Skill\n\nUse this skill when the user asks for review.",
	}})

	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "private",
		ConnectionID: "napcat-main",
		UserID:       "20002",
		MessageID:    "msg-skill-1",
		Timestamp:    time.Unix(1710000001, 0),
		Segments: []message.Segment{
			message.Text("帮我看一下这段代码"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	})

	found := false
	for _, msg := range gen.last {
		content, ok := msg.Content.(string)
		if msg.Role == "system" && ok && strings.Contains(content, "Demo Skill") && strings.Contains(content, "Use this skill") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("prompt messages = %+v, want injected prompt skill system message", gen.last)
	}
}

func TestServiceHandleEvent_ExecutesRegisteredExternalTool(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if err := service.RegisterTools("test.profile", []sdk.AIToolDefinition{{
		Name:        "lookup_sender_label",
		Description: "Resolve a synthetic sender label for tests.",
		InputSchema: emptyToolInputSchema(),
		Handle: func(_ context.Context, toolCtx sdk.AIToolContext, _ json.RawMessage) (any, error) {
			evt := toolCtx.Event()
			return map[string]any{"label": "sender-" + evt.UserID}, nil
		},
	}}); err != nil {
		t.Fatalf("RegisterTools() error = %v", err)
	}
	gen := &stubGenerator{turns: []turnResult{
		{
			ToolCalls: []toolCall{{
				ID:        "call-external-tool",
				Name:      "lookup_sender_label",
				Arguments: json.RawMessage(`{}`),
			}},
		},
		{Text: "你好，sender-20002。"},
	}}
	service.generator = gen

	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "private",
		ConnectionID: "napcat-main",
		UserID:       "20002",
		MessageID:    "msg-external-tool-1",
		Timestamp:    time.Unix(1710000003, 0),
		Segments: []message.Segment{
			message.Text("你知道我是谁吗？"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	})

	if gen.calls != 2 {
		t.Fatalf("generator calls = %d, want 2", gen.calls)
	}
	if len(messenger.sends) != 1 {
		t.Fatalf("send count = %d, want 1", len(messenger.sends))
	}
	if got := messenger.sends[0].text; got != "你好，sender-20002。" {
		t.Fatalf("send text = %q, want external tool-aware final reply", got)
	}
	foundToolSpec := false
	for _, tool := range gen.lastTools {
		if tool.Name == "lookup_sender_label" {
			foundToolSpec = true
			break
		}
	}
	if !foundToolSpec {
		t.Fatalf("last tools = %+v, want external tool registered", gen.lastTools)
	}
}

func TestServiceHandleEvent_AllowsEmptyReplyAfterExternalToolSentToChat(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if err := service.RegisterTools("test.image", []sdk.AIToolDefinition{{
		Name:        "generate_image",
		Description: "Generate and send an image in the current chat.",
		InputSchema: emptyToolInputSchema(),
		Handle: func(_ context.Context, _ sdk.AIToolContext, _ json.RawMessage) (any, error) {
			return map[string]any{
				"sent_to_chat": true,
				"image_urls":   []string{"https://example.com/image.png"},
			}, nil
		},
	}}); err != nil {
		t.Fatalf("RegisterTools() error = %v", err)
	}
	gen := &stubGenerator{turns: []turnResult{
		{
			ToolCalls: []toolCall{{
				ID:        "call-generate-image",
				Name:      "generate_image",
				Arguments: json.RawMessage(`{"prompt":"cat"}`),
			}},
		},
		{},
	}}
	service.generator = gen

	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "private",
		ConnectionID: "napcat-main",
		UserID:       "20002",
		MessageID:    "msg-image-tool-1",
		Timestamp:    time.Unix(1710000003, 0),
		Segments: []message.Segment{
			message.Text("画一只猫"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	})

	if gen.calls != 2 {
		t.Fatalf("generator calls = %d, want 2", gen.calls)
	}
	if len(messenger.sends) != 0 || len(messenger.replies) != 0 || len(messenger.segments) != 0 {
		t.Fatalf("messenger sends=%d replies=%d segments=%d, want none from core", len(messenger.sends), len(messenger.replies), len(messenger.segments))
	}
	snapshot := service.Snapshot()
	if snapshot.LastError != "" {
		t.Fatalf("LastError = %q, want empty after tool already sent to chat", snapshot.LastError)
	}
	if snapshot.LastDecisionReason != "工具已发送消息，跳过空文本回复" {
		t.Fatalf("LastDecisionReason = %q, want outbound-tool skip reason", snapshot.LastDecisionReason)
	}
}

func TestServiceRegisterTools_RejectsDuplicateToolName(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if err := service.RegisterTools("dup.provider", []sdk.AIToolDefinition{{
		Name:        "send_message_current",
		Description: "conflict tool",
		InputSchema: emptyToolInputSchema(),
		Handle: func(_ context.Context, _ sdk.AIToolContext, _ json.RawMessage) (any, error) {
			return nil, nil
		},
	}}); err == nil {
		t.Fatalf("RegisterTools() error = nil, want duplicate-name rejection")
	}
}

func TestServiceHandleEvent_ExecutesToolCallLoopAndUsesToolResult(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{
		strangerInfo: &sdk.UserInfo{UserID: "20002", Nickname: "Alice"},
	}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gen := &stubGenerator{turns: []turnResult{
		{
			ToolCalls: []toolCall{{
				ID:        "call-user-info",
				Name:      "get_current_user_info",
				Arguments: json.RawMessage(`{}`),
			}},
		},
		{Text: "你好，Alice。"},
	}}
	service.generator = gen

	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "private",
		ConnectionID: "napcat-main",
		UserID:       "20002",
		MessageID:    "msg-tool-1",
		Timestamp:    time.Unix(1710000003, 0),
		Segments: []message.Segment{
			message.Text("你是谁？"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	})

	if gen.calls != 2 {
		t.Fatalf("generator calls = %d, want 2", gen.calls)
	}
	if len(messenger.sends) != 1 {
		t.Fatalf("send count = %d, want 1", len(messenger.sends))
	}
	if got := messenger.sends[0].text; got != "你好，Alice。" {
		t.Fatalf("send text = %q, want tool-aware final reply", got)
	}
	foundToolResult := false
	for _, item := range gen.last {
		content, ok := item.Content.(string)
		if !ok {
			continue
		}
		if item.Role == "tool" && strings.Contains(content, "Alice") {
			foundToolResult = true
			break
		}
	}
	if !foundToolResult {
		t.Fatalf("second turn messages = %+v, want tool result injected", gen.last)
	}
}

func TestServiceHandleEvent_SendsViaSendMessageCurrentTool(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gen := &stubGenerator{turns: []turnResult{
		{
			ToolCalls: []toolCall{{
				ID:   "call-send-current",
				Name: "send_message_current",
				Arguments: json.RawMessage(`{
					"text": "工具发送的回复",
					"reply": true
				}`),
			}},
		},
		{Text: "done"},
	}}
	service.generator = gen

	evt := event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-tool-send",
		Timestamp:    time.Unix(1710000004, 0),
		Segments: []message.Segment{
			message.At("123456789"),
			message.Text("帮我发一句话"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	}
	service.HandleEvent(context.Background(), evt)

	if gen.calls != 2 {
		t.Fatalf("generator calls = %d, want 2", gen.calls)
	}
	if len(messenger.replies) != 1 {
		t.Fatalf("reply count = %d, want 1", len(messenger.replies))
	}
	if len(messenger.sends) != 0 {
		t.Fatalf("send count = %d, want 0 when reply tool path is used", len(messenger.sends))
	}
	if got := messenger.replies[0].text; got != "工具发送的回复" {
		t.Fatalf("reply text = %q, want scheduled tool message", got)
	}

	scopeKey := buildScopeKey(evt)
	session := service.sessions[scopeKey]
	if session == nil || len(session.Recent) == 0 {
		t.Fatalf("session = %+v, want assistant reply recorded", session)
	}
	last := session.Recent[len(session.Recent)-1]
	if last.Role != "assistant" || last.Text != "工具发送的回复" {
		t.Fatalf("last session item = %+v, want scheduled tool reply persisted", last)
	}
}

func TestServiceHandleEvent_SendsViaSendImageCurrentTool(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gen := &stubGenerator{turns: []turnResult{
		{
			ToolCalls: []toolCall{{
				ID:   "call-send-image-current",
				Name: "send_image_current",
				Arguments: json.RawMessage(`{
					"file": "https://example.com/stickers/happy.gif",
					"caption": "这个表情很合适",
					"reply": true
				}`),
			}},
		},
		{Text: "done"},
	}}
	service.generator = gen

	evt := event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-tool-send-image",
		Timestamp:    time.Unix(1710000005, 0),
		Segments: []message.Segment{
			message.At("123456789"),
			message.Text("来个表情"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	}
	service.HandleEvent(context.Background(), evt)

	if gen.calls != 2 {
		t.Fatalf("generator calls = %d, want 2", gen.calls)
	}
	if len(messenger.segments) != 1 {
		t.Fatalf("segment send count = %d, want 1", len(messenger.segments))
	}
	if len(messenger.replies) != 0 || len(messenger.sends) != 0 {
		t.Fatalf("text sends replies=%d sends=%d, want none", len(messenger.replies), len(messenger.sends))
	}
	got := messenger.segments[0].segments
	if len(got) != 3 {
		t.Fatalf("segments = %+v, want reply + image + caption", got)
	}
	if got[0].Type != "reply" || got[0].Data["id"] != "msg-tool-send-image" {
		t.Fatalf("first segment = %+v, want reply segment", got[0])
	}
	if got[1].Type != "image" || got[1].Data["file"] != "https://example.com/stickers/happy.gif" {
		t.Fatalf("second segment = %+v, want image segment", got[1])
	}
	if got[2].Type != "text" || got[2].Data["text"] != "这个表情很合适" {
		t.Fatalf("third segment = %+v, want caption text", got[2])
	}
	scopeKey := buildScopeKey(evt)
	session := service.sessions[scopeKey]
	if session == nil || len(session.Recent) == 0 {
		t.Fatalf("session = %+v, want assistant image send recorded", session)
	}
	last := session.Recent[len(session.Recent)-1]
	if last.Role != "assistant" || last.Text != "这个表情很合适" {
		t.Fatalf("last session item = %+v, want caption persisted", last)
	}
}

func TestServiceBuiltinTool_SendVideoAndFileCurrent(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	evt := event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-media-tools",
		Timestamp:    time.Unix(1710000006, 0),
		Meta:         map[string]string{"self_id": "123456789"},
	}

	videoTool := findToolDefinitionForTest(t, service, evt, "send_video_current")
	videoExec := &toolExecutionContext{
		service: service,
		event:   evt,
		target:  message.Target{ConnectionID: evt.ConnectionID, ChatType: evt.ChatType, GroupID: evt.GroupID, UserID: evt.UserID},
		replyTo: evt.MessageID,
	}
	_, err = videoTool.Handler(context.Background(), videoExec, json.RawMessage(`{
		"file": "https://example.com/demo.mp4",
		"caption": "视频来了",
		"reply": true
	}`))
	if err != nil {
		t.Fatalf("send_video_current error = %v", err)
	}
	if videoExec.scheduled == nil || len(videoExec.scheduled.Segments) != 3 {
		t.Fatalf("video scheduled = %+v, want reply + video + caption", videoExec.scheduled)
	}
	if got := videoExec.scheduled.Segments[1]; got.Type != "video" || got.Data["file"] != "https://example.com/demo.mp4" {
		t.Fatalf("video segment = %+v, want video", got)
	}

	fileTool := findToolDefinitionForTest(t, service, evt, "send_file_current")
	fileExec := &toolExecutionContext{
		service: service,
		event:   evt,
		target:  message.Target{ConnectionID: evt.ConnectionID, ChatType: evt.ChatType, GroupID: evt.GroupID, UserID: evt.UserID},
		replyTo: evt.MessageID,
	}
	_, err = fileTool.Handler(context.Background(), fileExec, json.RawMessage(`{
		"file": "https://example.com/report.txt",
		"name": "report.txt",
		"caption": "文件在这",
		"reply": false
	}`))
	if err != nil {
		t.Fatalf("send_file_current error = %v", err)
	}
	if fileExec.scheduled == nil || len(fileExec.scheduled.Segments) != 2 {
		t.Fatalf("file scheduled = %+v, want caption + file", fileExec.scheduled)
	}
	if got := fileExec.scheduled.Segments[1]; got.Type != "file" || got.Data["file"] != "https://example.com/report.txt" || got.Data["name"] != "report.txt" {
		t.Fatalf("file segment = %+v, want file with name", got)
	}
}

func TestServiceHandleEvent_SendsViaGroupForwardCurrentTool(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gen := &stubGenerator{turns: []turnResult{
		{
			ToolCalls: []toolCall{{
				ID:   "call-send-forward-current",
				Name: "send_group_forward_current",
				Arguments: json.RawMessage(`{
					"nodes": [
						{"nickname": "罗纸酱", "text": "第一段整理"},
						{"nickname": "罗纸酱", "text": "第二段配图", "image_file": "https://example.com/a.png"}
					],
					"prompt": "AI 整理了 2 条内容",
					"summary": "查看整理结果",
					"source": "Go-bot"
				}`),
			}},
		},
		{Text: "done"},
	}}
	service.generator = gen

	evt := event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-tool-send-forward",
		Timestamp:    time.Unix(1710000007, 0),
		Segments: []message.Segment{
			message.At("123456789"),
			message.Text("整理成合并转发"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	}
	service.HandleEvent(context.Background(), evt)

	if gen.calls != 2 {
		t.Fatalf("generator calls = %d, want 2", gen.calls)
	}
	if len(messenger.forwards) != 1 {
		t.Fatalf("forward count = %d, want 1", len(messenger.forwards))
	}
	forward := messenger.forwards[0]
	if forward.connectionID != "napcat-main" || forward.groupID != "10001" {
		t.Fatalf("forward target = %+v, want current group", forward)
	}
	if forward.options.Prompt != "AI 整理了 2 条内容" || forward.options.Summary != "查看整理结果" || forward.options.Source != "Go-bot" {
		t.Fatalf("forward options = %+v, want configured options", forward.options)
	}
	if len(forward.nodes) != 2 {
		t.Fatalf("forward nodes = %+v, want 2 nodes", forward.nodes)
	}
	if got := forward.nodes[1].Content[1]; got.Type != "image" || got.Data["file"] != "https://example.com/a.png" {
		t.Fatalf("second node media = %+v, want image", got)
	}
}

func TestServiceSkillCatalog_IncludesBuiltinChineseDisplay(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	var core SkillView
	for _, item := range service.SkillCatalog() {
		if item.ProviderID == "builtin.core" {
			core = item
			break
		}
	}
	if core.ProviderID == "" {
		t.Fatalf("SkillCatalog() missing builtin.core: %+v", service.SkillCatalog())
	}

	var foundHistory bool
	var foundCLI bool
	var foundImage bool
	var foundVideo bool
	var foundFile bool
	var foundForward bool
	var foundTime bool
	var foundContext bool
	for _, tool := range core.Tools {
		switch tool.Name {
		case "send_image_current":
			foundImage = true
			if tool.DisplayName != "发送当前会话图片" {
				t.Fatalf("Image DisplayName = %q, want Chinese builtin display name", tool.DisplayName)
			}
		case "send_video_current":
			foundVideo = true
			if tool.DisplayName != "发送当前会话视频" {
				t.Fatalf("Video DisplayName = %q, want Chinese builtin display name", tool.DisplayName)
			}
		case "send_file_current":
			foundFile = true
			if tool.DisplayName != "发送当前会话文件" {
				t.Fatalf("File DisplayName = %q, want Chinese builtin display name", tool.DisplayName)
			}
		case "send_group_forward_current":
			foundForward = true
			if tool.Availability != "仅群聊" {
				t.Fatalf("Forward Availability = %q, want 仅群聊", tool.Availability)
			}
		case "get_current_time":
			foundTime = true
			if tool.Availability != "本地时间" {
				t.Fatalf("Time Availability = %q, want 本地时间", tool.Availability)
			}
		case "get_current_conversation_context":
			foundContext = true
			if tool.DisplayName != "读取当前会话上下文" {
				t.Fatalf("Context DisplayName = %q, want Chinese builtin display name", tool.DisplayName)
			}
		case "search_message_history":
			foundHistory = true
			if tool.DisplayName != "检索当前会话历史" {
				t.Fatalf("DisplayName = %q, want Chinese builtin display name", tool.DisplayName)
			}
			if !strings.Contains(tool.DisplayDescription, "本地已入库") {
				t.Fatalf("DisplayDescription = %q, want Chinese builtin description", tool.DisplayDescription)
			}
		case "run_cli_command":
			foundCLI = true
			if tool.DisplayName != "执行白名单 CLI" {
				t.Fatalf("CLI DisplayName = %q, want Chinese builtin display name", tool.DisplayName)
			}
			if tool.Availability != "需先启用" {
				t.Fatalf("CLI Availability = %q, want 需先启用", tool.Availability)
			}
		}
	}
	if !foundHistory {
		t.Fatalf("builtin core tools = %+v, want search_message_history", core.Tools)
	}
	if !foundCLI {
		t.Fatalf("builtin core tools = %+v, want run_cli_command", core.Tools)
	}
	if !foundImage {
		t.Fatalf("builtin core tools = %+v, want send_image_current", core.Tools)
	}
	if !foundVideo {
		t.Fatalf("builtin core tools = %+v, want send_video_current", core.Tools)
	}
	if !foundFile {
		t.Fatalf("builtin core tools = %+v, want send_file_current", core.Tools)
	}
	if !foundForward {
		t.Fatalf("builtin core tools = %+v, want send_group_forward_current", core.Tools)
	}
	if !foundTime {
		t.Fatalf("builtin core tools = %+v, want get_current_time", core.Tools)
	}
	if !foundContext {
		t.Fatalf("builtin core tools = %+v, want get_current_conversation_context", core.Tools)
	}
}

func TestServiceBuiltinTool_RunCLICommandDisabled(t *testing.T) {
	cfg := testAIConfig()
	cfg.CLI.Enabled = false
	cfg.CLI.AllowedCommands = []string{"git"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	evt := event.Event{
		Kind:         "message",
		ChatType:     "private",
		ConnectionID: "napcat-main",
		UserID:       "20002",
		MessageID:    "msg-trigger-cli-disabled",
		Timestamp:    time.Unix(1710000190, 0),
		Meta:         map[string]string{"self_id": "123456789"},
	}
	for _, item := range service.buildToolDefinitions(evt) {
		if item.Spec.Name == "run_cli_command" {
			t.Fatalf("run_cli_command should be absent when ai.cli.enabled=false")
		}
	}
}

func TestServiceBuiltinTool_CurrentConversationContext(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	evt := event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-context",
		Timestamp:    time.Unix(1710000190, 0),
		Segments: []message.Segment{
			message.Reply("msg-parent"),
			message.At("123456789"),
			message.Text("看这个"),
			message.Image("https://example.com/a.png"),
		},
		Meta: map[string]string{
			"self_id":         "123456789",
			"sender_nickname": "Alice",
		},
	}
	tool := findToolDefinitionForTest(t, service, evt, "get_current_conversation_context")
	result, err := tool.Handler(context.Background(), &toolExecutionContext{service: service, event: evt}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("tool.Handler() error = %v", err)
	}
	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if payload["chat_type"] != "group" || payload["group_id"] != "10001" || payload["user_id"] != "20002" {
		t.Fatalf("payload = %+v, want current group context", payload)
	}
	if payload["reply_to_message_id"] != "msg-parent" {
		t.Fatalf("reply_to_message_id = %#v, want msg-parent", payload["reply_to_message_id"])
	}
	if payload["mentioned_bot"] != true || payload["has_image"] != true {
		t.Fatalf("payload = %+v, want mentioned_bot and has_image", payload)
	}
}

func TestServiceBuiltinTool_RunCLICommandUsesWhitelistAndRunner(t *testing.T) {
	cfg := testAIConfig()
	cfg.CLI.Enabled = true
	cfg.CLI.AllowedCommands = []string{"git"}
	cfg.CLI.TimeoutSeconds = 12
	cfg.CLI.MaxOutputBytes = 4096
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	cliRunner := &stubCLICommandRunner{result: cliCommandResult{
		Command:   "git",
		Args:      []string{"status", "--short"},
		ExitCode:  0,
		Stdout:    "M internal/ai/tools.go",
		Truncated: false,
	}}
	service.cliRunner = cliRunner

	evt := event.Event{
		Kind:         "message",
		ChatType:     "private",
		ConnectionID: "napcat-main",
		UserID:       "20002",
		MessageID:    "msg-trigger-cli-ok",
		Timestamp:    time.Unix(1710000191, 0),
		Meta:         map[string]string{"self_id": "123456789"},
	}
	tool := findToolDefinitionForTest(t, service, evt, "run_cli_command")
	result, err := tool.Handler(context.Background(), &toolExecutionContext{service: service, event: evt}, json.RawMessage(`{"command":"git","args":["status","--short"]}`))
	if err != nil {
		t.Fatalf("tool.Handler() error = %v", err)
	}
	if cliRunner.calls != 1 {
		t.Fatalf("cliRunner calls = %d, want 1", cliRunner.calls)
	}
	if cliRunner.lastCommand != "git" {
		t.Fatalf("cliRunner lastCommand = %q, want git", cliRunner.lastCommand)
	}
	if cliRunner.lastMaxBytes != 4096 {
		t.Fatalf("cliRunner lastMaxBytes = %d, want 4096", cliRunner.lastMaxBytes)
	}

	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if payload["exit_code"] != 0 {
		t.Fatalf("exit_code = %#v, want 0", payload["exit_code"])
	}
	if payload["stdout"] != "M internal/ai/tools.go" {
		t.Fatalf("stdout = %#v, want stub output", payload["stdout"])
	}
	if payload["timeout_sec"] != 12 {
		t.Fatalf("timeout_sec = %#v, want 12", payload["timeout_sec"])
	}
}

func TestServiceBuiltinTool_RunCLICommandRejectsNonWhitelistedCommand(t *testing.T) {
	cfg := testAIConfig()
	cfg.CLI.Enabled = true
	cfg.CLI.AllowedCommands = []string{"git"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	evt := event.Event{
		Kind:         "message",
		ChatType:     "private",
		ConnectionID: "napcat-main",
		UserID:       "20002",
		MessageID:    "msg-trigger-cli-blocked",
		Timestamp:    time.Unix(1710000192, 0),
		Meta:         map[string]string{"self_id": "123456789"},
	}
	tool := findToolDefinitionForTest(t, service, evt, "run_cli_command")
	_, err = tool.Handler(context.Background(), &toolExecutionContext{service: service, event: evt}, json.RawMessage(`{"command":"go","args":["version"]}`))
	if err == nil || !strings.Contains(err.Error(), "不在 ai.cli.allowed_commands 白名单中") {
		t.Fatalf("tool.Handler() error = %v, want whitelist rejection", err)
	}
}

func TestServiceBuiltinTool_SearchMessageHistory(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	store := service.store
	if store == nil {
		t.Fatalf("service.store = %#v, want initialized store", service.store)
	}

	baseTime := time.Unix(1710000200, 0)
	items := []MessageLog{
		{
			MessageID:      "msg-history-1",
			ConnectionID:   "napcat-main",
			ChatType:       "private",
			UserID:         "20002",
			SenderRole:     "user",
			SenderName:     "Alice",
			SenderNickname: "Alice",
			TextContent:    "今天继续聊春樱配色和卡片布局",
			HasText:        true,
			MessageStatus:  "normal",
			OccurredAt:     baseTime,
			CreatedAt:      baseTime,
		},
		{
			MessageID:      "msg-history-2",
			ConnectionID:   "napcat-main",
			ChatType:       "private",
			UserID:         "30003",
			SenderRole:     "user",
			SenderName:     "Bob",
			SenderNickname: "Bob",
			TextContent:    "春樱主题同步给别的会话",
			HasText:        true,
			MessageStatus:  "normal",
			OccurredAt:     baseTime.Add(1 * time.Minute),
			CreatedAt:      baseTime.Add(1 * time.Minute),
		},
	}
	for _, item := range items {
		if err := store.AppendMessageLog(context.Background(), item); err != nil {
			t.Fatalf("AppendMessageLog(%s) error = %v", item.MessageID, err)
		}
	}

	evt := event.Event{
		Kind:         "message",
		ChatType:     "private",
		ConnectionID: "napcat-main",
		UserID:       "20002",
		MessageID:    "msg-trigger-history",
		Timestamp:    baseTime.Add(2 * time.Minute),
		Meta:         map[string]string{"self_id": "123456789"},
	}
	tool := findToolDefinitionForTest(t, service, evt, "search_message_history")
	result, err := tool.Handler(context.Background(), &toolExecutionContext{service: service, event: evt}, json.RawMessage(`{"query":"春樱","limit":5}`))
	if err != nil {
		t.Fatalf("tool.Handler() error = %v", err)
	}

	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if payload["returned"] != 1 {
		t.Fatalf("returned = %#v, want 1", payload["returned"])
	}
	itemsResult, ok := payload["items"].([]map[string]any)
	if !ok || len(itemsResult) != 1 {
		t.Fatalf("items = %#v, want one scoped history result", payload["items"])
	}
	if itemsResult[0]["message_id"] != "msg-history-1" {
		t.Fatalf("message_id = %#v, want msg-history-1", itemsResult[0]["message_id"])
	}
}

func TestServiceBuiltinTool_ListCurrentGroupMembers(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{
		groupMembers: []sdk.GroupMemberInfo{
			{GroupID: "10001", UserID: "20002", Nickname: "Sakura", Card: "春樱酱", Role: "member"},
			{GroupID: "10001", UserID: "30003", Nickname: "Alice", Card: "UI 设计师", Role: "admin"},
		},
	}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	evt := event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-trigger-members",
		Timestamp:    time.Unix(1710000300, 0),
		Meta:         map[string]string{"self_id": "123456789"},
	}
	tool := findToolDefinitionForTest(t, service, evt, "list_current_group_members")
	result, err := tool.Handler(context.Background(), &toolExecutionContext{service: service, event: evt}, json.RawMessage(`{"keyword":"樱","limit":5}`))
	if err != nil {
		t.Fatalf("tool.Handler() error = %v", err)
	}

	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if payload["returned"] != 1 {
		t.Fatalf("returned = %#v, want 1", payload["returned"])
	}
	members, ok := payload["members"].([]map[string]any)
	if !ok || len(members) != 1 {
		t.Fatalf("members = %#v, want one matched member", payload["members"])
	}
	if members[0]["display_name"] != "春樱酱" {
		t.Fatalf("display_name = %#v, want 春樱酱", members[0]["display_name"])
	}
}

func TestServiceBuiltinTool_GetConnectionStatus(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{
		status: &sdk.BotStatus{
			Online: true,
			Good:   true,
			Stat:   map[string]any{"latency_ms": 42},
		},
	}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	evt := event.Event{
		Kind:         "message",
		ChatType:     "private",
		ConnectionID: "napcat-main",
		UserID:       "20002",
		MessageID:    "msg-trigger-status",
		Timestamp:    time.Unix(1710000400, 0),
		Meta:         map[string]string{"self_id": "123456789"},
	}
	tool := findToolDefinitionForTest(t, service, evt, "get_connection_status")
	result, err := tool.Handler(context.Background(), &toolExecutionContext{service: service, event: evt}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("tool.Handler() error = %v", err)
	}

	payload, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	if payload["online"] != true || payload["good"] != true {
		t.Fatalf("payload = %#v, want online + good connection status", payload)
	}
}

func TestServiceHandleEvent_IgnoresImageWhenVisionDisabled(t *testing.T) {
	cfg := testAIConfig()
	cfg.Vision.Enabled = false
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gen := &stubGenerator{text: "收到"}
	service.generator = gen

	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-image-disabled",
		Timestamp:    time.Unix(1710000001, 0),
		Segments: []message.Segment{
			message.At("123456789"),
			{Type: "image", Data: map[string]any{"url": "https://example.com/cat.png"}},
		},
		Meta: map[string]string{"self_id": "123456789"},
	})

	if len(messenger.replies) != 0 {
		t.Fatalf("reply count = %d, want 0", len(messenger.replies))
	}
	if len(gen.last) != 0 {
		t.Fatalf("generator prompt count = %d, want 0 when image vision disabled", len(gen.last))
	}
}

func TestServiceHandleEvent_PersistsMessageLogWhenDisabled(t *testing.T) {
	cfg := testAIConfig()
	cfg.Enabled = false
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if snapshot := service.Snapshot(); !snapshot.StoreReady {
		t.Fatalf("snapshot = %+v, want store ready even when AI disabled", snapshot)
	}

	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "disabled-msg-1",
		Timestamp:    time.Unix(1710000020, 0),
		Segments: []message.Segment{
			message.Text("只是记录，不触发回复"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	})

	items, err := service.ListMessageLogs(context.Background(), MessageLogQuery{ChatType: "group", Limit: 10})
	if err != nil {
		t.Fatalf("ListMessageLogs() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("message log len = %d, want 1", len(items))
	}
	if got := items[0].MessageID; got != "disabled-msg-1" {
		t.Fatalf("message id = %q, want persisted original message id", got)
	}
	if len(messenger.replies) != 0 || len(messenger.sends) != 0 {
		t.Fatalf("messenger activity = replies:%d sends:%d, want 0 when AI disabled", len(messenger.replies), len(messenger.sends))
	}
}

func TestServiceHandleEvent_UsesVisionResultWhenEnabled(t *testing.T) {
	cfg := testAIConfig()
	cfg.Vision.Enabled = true
	cfg.Vision.Mode = "same_as_chat"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gen := &stubGenerator{text: "看起来像一张猫猫表情包。"}
	vision := &stubVisionGenerator{text: "一张猫咪表情包，画面里有一只猫，语气像在无奈吐槽。"}
	service.generator = gen
	service.visionGenerator = vision

	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-image-enabled",
		Timestamp:    time.Unix(1710000002, 0),
		Segments: []message.Segment{
			message.At("123456789"),
			{Type: "image", Data: map[string]any{"url": "https://example.com/cat.png"}},
		},
		Meta: map[string]string{"self_id": "123456789"},
	})

	if len(messenger.replies) != 1 {
		t.Fatalf("reply count = %d, want 1", len(messenger.replies))
	}
	if len(vision.lastImages) != 1 || vision.lastImages[0].URL != "https://example.com/cat.png" {
		t.Fatalf("vision images = %+v, want one remote image", vision.lastImages)
	}

	foundVisionText := false
	for _, item := range gen.last {
		content, ok := item.Content.(string)
		if !ok {
			continue
		}
		if strings.Contains(content, "图片识别：一张猫咪表情包") {
			foundVisionText = true
			break
		}
	}
	if !foundVisionText {
		t.Fatalf("generator prompt = %+v, want image understanding text injected", gen.last)
	}
	snapshot := service.Snapshot()
	if !snapshot.VisionEnabled || snapshot.VisionMode != "same_as_chat" {
		t.Fatalf("snapshot vision flags = %+v, want enabled same_as_chat", snapshot)
	}
	if got := snapshot.LastVisionSummary; !strings.Contains(got, "猫咪表情包") {
		t.Fatalf("LastVisionSummary = %q, want vision summary", got)
	}
	if snapshot.LastVisionAt.IsZero() {
		t.Fatalf("LastVisionAt is zero, want vision timestamp")
	}
}

func TestServiceHandleEvent_PromotesPreferenceMemory(t *testing.T) {
	cfg := testAIConfig()
	cfg.Reply.ReplyOnAt = true
	cfg.Reply.ReplyOnBotName = false
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.generator = &stubGenerator{text: "ok"}

	for i := 0; i < 2; i++ {
		service.HandleEvent(context.Background(), event.Event{
			Kind:         "message",
			ChatType:     "group",
			ConnectionID: "napcat-main",
			GroupID:      "10001",
			UserID:       "20002",
			MessageID:    "msg-memory",
			Timestamp:    time.Unix(1710000100+int64(i), 0),
			Segments: []message.Segment{
				message.Text("我喜欢东方Project"),
			},
			Meta: map[string]string{"self_id": "123456789"},
		})
	}

	if got := len(service.candidateMemories); got != 1 {
		t.Fatalf("candidate memory count = %d, want 1", got)
	}
	if got := len(service.longTermMemories); got != 1 {
		t.Fatalf("long term memory count = %d, want 1", got)
	}
	for _, item := range service.longTermMemories {
		if item.EvidenceCount != 2 {
			t.Fatalf("evidence_count = %d, want 2", item.EvidenceCount)
		}
	}

	debugView := service.DebugView(4)
	if len(debugView.Sessions) != 1 {
		t.Fatalf("debug session count = %d, want 1", len(debugView.Sessions))
	}
	if len(debugView.CandidateMemories) != 1 {
		t.Fatalf("debug candidate count = %d, want 1", len(debugView.CandidateMemories))
	}
	if len(debugView.LongTermMemories) != 1 {
		t.Fatalf("debug long term count = %d, want 1", len(debugView.LongTermMemories))
	}
	if debugView.Sessions[0].RecentCount != 2 {
		t.Fatalf("debug recent_count = %d, want 2", debugView.Sessions[0].RecentCount)
	}
}

func TestServiceImportRecentEvents_UpsertsSessionAndLogs(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	evt := event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-sync",
		Timestamp:    time.Unix(1710000200, 0),
		Segments: []message.Segment{
			message.Text("离线期间的消息"),
		},
		Meta: map[string]string{
			"self_id":         "123456789",
			"sender_nickname": "Alice",
		},
	}

	for i := 0; i < 2; i++ {
		imported, err := service.ImportRecentEvents(context.Background(), []event.Event{evt})
		if err != nil {
			t.Fatalf("ImportRecentEvents() error = %v", err)
		}
		if imported != 1 {
			t.Fatalf("ImportRecentEvents() imported = %d, want 1", imported)
		}
	}

	items, err := service.ListMessageLogs(context.Background(), MessageLogQuery{ChatType: "group", GroupID: "10001", Limit: 10})
	if err != nil {
		t.Fatalf("ListMessageLogs() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListMessageLogs() len = %d, want 1", len(items))
	}
	if items[0].MessageID != "msg-sync" || items[0].TextContent != "离线期间的消息" {
		t.Fatalf("message log = %+v, want synced message", items[0])
	}
	if items[0].SenderNickname != "Alice" {
		t.Fatalf("sender nickname = %q, want Alice", items[0].SenderNickname)
	}

	debugView := service.DebugView(4)
	if len(debugView.Sessions) != 1 {
		t.Fatalf("debug session count = %d, want 1", len(debugView.Sessions))
	}
	if debugView.Sessions[0].RecentCount != 1 {
		t.Fatalf("debug recent_count = %d, want 1", debugView.Sessions[0].RecentCount)
	}
}

func TestServiceRememberMessageDisplay_UpdatesStoredDisplayNames(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	imported, err := service.ImportRecentEvents(context.Background(), []event.Event{{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-rename",
		Timestamp:    time.Unix(1710000250, 0),
		Segments: []message.Segment{
			message.Text("改名前的消息"),
		},
		Meta: map[string]string{
			"self_id":         "123456789",
			"sender_nickname": "Alice",
		},
	}})
	if err != nil {
		t.Fatalf("ImportRecentEvents() error = %v", err)
	}
	if imported != 1 {
		t.Fatalf("ImportRecentEvents() imported = %d, want 1", imported)
	}

	if err := service.RememberMessageDisplay(context.Background(), MessageLog{
		ConnectionID:   "napcat-main",
		ChatType:       "group",
		GroupID:        "10001",
		GroupName:      "春日研究会",
		UserID:         "20002",
		SenderRole:     "user",
		SenderName:     "Sakura",
		SenderNickname: "Sakura",
		OccurredAt:     time.Unix(1710000300, 0),
	}); err != nil {
		t.Fatalf("RememberMessageDisplay() error = %v", err)
	}

	items, err := service.ListMessageLogs(context.Background(), MessageLogQuery{ChatType: "group", GroupID: "10001", Limit: 10})
	if err != nil {
		t.Fatalf("ListMessageLogs() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListMessageLogs() len = %d, want 1", len(items))
	}
	if items[0].GroupName != "春日研究会" {
		t.Fatalf("group name = %q, want 春日研究会", items[0].GroupName)
	}
	if items[0].SenderName != "Sakura" {
		t.Fatalf("sender name = %q, want Sakura", items[0].SenderName)
	}
	if items[0].SenderNickname != "Sakura" {
		t.Fatalf("sender nickname = %q, want Sakura", items[0].SenderNickname)
	}

	detail, err := service.GetMessageDetail(context.Background(), "msg-rename")
	if err != nil {
		t.Fatalf("GetMessageDetail() error = %v", err)
	}
	if detail.Message.GroupName != "春日研究会" {
		t.Fatalf("detail group name = %q, want 春日研究会", detail.Message.GroupName)
	}
	if detail.Message.SenderName != "Sakura" {
		t.Fatalf("detail sender name = %q, want Sakura", detail.Message.SenderName)
	}
}

func TestServiceHandleEvent_BuildsProfilesAndRelations(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gen := &stubGenerator{text: "已收到"}
	service.generator = gen

	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "30003",
		MessageID:    "msg-profile-1",
		Timestamp:    time.Unix(1710000300, 0),
		Segments: []message.Segment{
			message.Text("哈哈这个梗也太草了"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	})
	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-profile-2",
		Timestamp:    time.Unix(1710000302, 0),
		Segments: []message.Segment{
			message.Text("我喜欢东方Project"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	})
	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-profile-3",
		Timestamp:    time.Unix(1710000305, 0),
		Segments: []message.Segment{
			message.At("123456789"),
			message.Text("你怎么看这个梗？"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	})

	if len(messenger.replies) != 1 {
		t.Fatalf("reply count = %d, want 1", len(messenger.replies))
	}

	snapshot := service.Snapshot()
	if snapshot.GroupProfileCount != 1 {
		t.Fatalf("group profile count = %d, want 1", snapshot.GroupProfileCount)
	}
	if snapshot.UserProfileCount < 2 {
		t.Fatalf("user profile count = %d, want at least 2", snapshot.UserProfileCount)
	}
	if snapshot.RelationEdgeCount < 1 {
		t.Fatalf("relation edge count = %d, want at least 1", snapshot.RelationEdgeCount)
	}

	debugView := service.DebugView(8)
	if len(debugView.GroupProfiles) == 0 {
		t.Fatalf("group profiles = %+v, want non-empty", debugView.GroupProfiles)
	}
	if len(debugView.UserProfiles) < 2 {
		t.Fatalf("user profiles count = %d, want at least 2", len(debugView.UserProfiles))
	}
	if len(debugView.RelationEdges) == 0 {
		t.Fatalf("relation edges = %+v, want non-empty", debugView.RelationEdges)
	}

	foundGroupProfile := false
	foundUserProfile := false
	foundRelationHint := false
	for _, item := range gen.last {
		content, ok := item.Content.(string)
		if !ok {
			continue
		}
		if strings.Contains(content, "当前群风格：") {
			foundGroupProfile = true
		}
		if strings.Contains(content, "画像：") {
			foundUserProfile = true
		}
		if strings.Contains(content, "近期互动对象：") {
			foundRelationHint = true
		}
	}
	if !foundGroupProfile || !foundUserProfile || !foundRelationHint {
		t.Fatalf("generator prompt = %+v, want group/user/relation context", gen.last)
	}
}

func TestUpdateRelationEdgesLocked_DerivesExpandedRelationTypes(t *testing.T) {
	groupID := "10001"
	baseAt := time.Unix(1710000400, 0)
	service := &Service{
		groupProfiles:    map[string]*GroupProfile{},
		userProfiles:     map[string]*UserInGroupProfile{},
		relationEdges:    map[string]*RelationEdge{},
		longTermMemories: map[string]*LongTermMemory{},
	}
	session := &SessionState{Scope: "group:" + groupID, GroupID: groupID}

	recordUserMessage := func(userID, messageID string, at time.Time, segments ...message.Segment) {
		evt := event.Event{
			Kind:      "message",
			ChatType:  "group",
			GroupID:   groupID,
			UserID:    userID,
			MessageID: messageID,
			Timestamp: at,
			Segments:  segments,
			Meta:      map[string]string{"self_id": "123456789"},
		}
		session.Recent = append(session.Recent, ConversationMessage{
			Role:      "user",
			UserID:    userID,
			Text:      cleanEventText(evt),
			MessageID: messageID,
			At:        at,
		})
		service.updateProfilesLocked(evt, session)
	}

	recordUserMessage("10001", "msg-topic-a", baseAt, message.Text("原神这个活动怎么打"))
	recordUserMessage("20002", "msg-topic-b", baseAt.Add(2*time.Second), message.Reply("msg-topic-a"), message.Text("我也在打原神"))
	recordUserMessage("30003", "msg-banter-a", baseAt.Add(4*time.Second), message.Text("哈哈这个梗太草了"))
	recordUserMessage("40004", "msg-banter-b", baseAt.Add(6*time.Second), message.Text("草，笑死"))
	recordUserMessage("50005", "msg-pref-a", baseAt.Add(8*time.Second), message.Text("我喜欢东方Project"))
	recordUserMessage("60006", "msg-pref-b", baseAt.Add(10*time.Second), message.Text("我也喜欢东方Project"))

	assertRelationEdgeForTest(t, service.relationEdges, groupID, "10001", "20002", "reply")
	assertRelationEdgeForTest(t, service.relationEdges, groupID, "10001", "20002", "co_topic")
	assertRelationEdgeForTest(t, service.relationEdges, groupID, "30003", "40004", "banter")
	assertRelationEdgeForTest(t, service.relationEdges, groupID, "50005", "60006", "shared_preference")
}

func TestRelationAnalysisRuntimeConfig_ExtendsSlowAnalysisBudget(t *testing.T) {
	cfg := testAIConfig()
	cfg.Provider.TimeoutMS = 30000
	cfg.Reply.MaxOutputTokens = 128

	got := relationAnalysisRuntimeConfig(cfg)
	if got.Provider.TimeoutMS != relationAnalysisTimeoutMS {
		t.Fatalf("TimeoutMS = %d, want %d", got.Provider.TimeoutMS, relationAnalysisTimeoutMS)
	}
	if got.Reply.MaxOutputTokens != relationAnalysisMaxTokens {
		t.Fatalf("MaxOutputTokens = %d, want %d", got.Reply.MaxOutputTokens, relationAnalysisMaxTokens)
	}

	cfg.Provider.TimeoutMS = relationAnalysisTimeoutMS + 1000
	cfg.Reply.MaxOutputTokens = relationAnalysisMaxTokens + 100
	got = relationAnalysisRuntimeConfig(cfg)
	if got.Provider.TimeoutMS != relationAnalysisTimeoutMS+1000 {
		t.Fatalf("TimeoutMS = %d, want caller override to be preserved", got.Provider.TimeoutMS)
	}
	if got.Reply.MaxOutputTokens != relationAnalysisMaxTokens+100 {
		t.Fatalf("MaxOutputTokens = %d, want caller override to be preserved", got.Reply.MaxOutputTokens)
	}
}

func TestGenerateRelationAnalysis_UsesLLMWithRelationGraph(t *testing.T) {
	cfg := testAIConfig()
	gen := &stubGenerator{text: "# 群友关系与性格分析\n\nLLM report."}
	service := &Service{
		cfg:       cfg,
		generator: gen,
		userProfiles: map[string]*UserInGroupProfile{
			userProfileKey("10001", "20002"): {
				GroupID:          "10001",
				UserID:           "20002",
				DisplayName:      "Alice",
				TopicPreferences: []string{"东方Project"},
				StyleTags:        []string{"高互动"},
				TrustScore:       0.78,
			},
			userProfileKey("10001", "30003"): {
				GroupID:          "10001",
				UserID:           "30003",
				DisplayName:      "Bob",
				TopicPreferences: []string{"东方Project"},
				StyleTags:        []string{"轻松玩梗"},
				TrustScore:       0.64,
			},
		},
		relationEdges: map[string]*RelationEdge{
			relationEdgeKey("10001", "20002", "30003", "shared_preference"): {
				ID:                relationEdgeKey("10001", "20002", "30003", "shared_preference"),
				GroupID:           "10001",
				NodeA:             "20002",
				NodeB:             "30003",
				RelationType:      "shared_preference",
				Strength:          0.58,
				EvidenceCount:     2,
				LastInteractionAt: time.Unix(1710000500, 0),
			},
		},
		longTermMemories: map[string]*LongTermMemory{
			"mem-pref-1": {
				ID:            "mem-pref-1",
				GroupID:       "10001",
				SubjectID:     "20002",
				MemoryType:    "preference",
				Content:       "Alice likes Touhou music.",
				Confidence:    0.8,
				EvidenceCount: 3,
			},
		},
	}

	result, err := service.GenerateRelationAnalysis(context.Background(), "10001", false)
	if err != nil {
		t.Fatalf("GenerateRelationAnalysis() error = %v", err)
	}
	if result.Markdown == "" || !strings.Contains(result.Markdown, "LLM report") {
		t.Fatalf("markdown = %q, want LLM report", result.Markdown)
	}
	if result.UserCount != 2 || result.EdgeCount != 1 || result.MemoryCount != 1 {
		t.Fatalf("result counts = users %d edges %d memories %d, want 2/1/1", result.UserCount, result.EdgeCount, result.MemoryCount)
	}
	if gen.calls != 1 {
		t.Fatalf("generator calls = %d, want 1", gen.calls)
	}
	foundPayload := false
	for _, item := range gen.last {
		content, ok := item.Content.(string)
		if !ok {
			continue
		}
		if strings.Contains(content, "shared_preference") && strings.Contains(content, "共同偏好") {
			foundPayload = true
			break
		}
	}
	if !foundPayload {
		t.Fatalf("generator prompt = %+v, want relation payload with labels", gen.last)
	}
	cached, err := service.GenerateRelationAnalysis(context.Background(), "10001", false)
	if err != nil {
		t.Fatalf("cached GenerateRelationAnalysis() error = %v", err)
	}
	if !cached.CacheHit || cached.InputHash == "" {
		t.Fatalf("cached result = %+v, want cache hit with input hash", cached)
	}
	if gen.calls != 1 {
		t.Fatalf("generator calls after cached analysis = %d, want 1", gen.calls)
	}
	_, err = service.GenerateRelationAnalysis(context.Background(), "10001", true)
	if err != nil {
		t.Fatalf("forced GenerateRelationAnalysis() error = %v", err)
	}
	if gen.calls != 2 {
		t.Fatalf("generator calls after forced analysis = %d, want 2", gen.calls)
	}
}

func assertRelationEdgeForTest(t *testing.T, edges map[string]*RelationEdge, groupID, left, right, relationType string) {
	t.Helper()
	nodeA, nodeB := normalizeRelationNodes(left, right)
	edge := edges[relationEdgeKey(groupID, nodeA, nodeB, relationType)]
	if edge == nil {
		t.Fatalf("relation edge %s/%s/%s not found in %+v", nodeA, nodeB, relationType, edges)
	}
	if edge.EvidenceCount <= 0 || edge.Strength <= 0 {
		t.Fatalf("relation edge = %+v, want positive evidence and strength", edge)
	}
}

func TestServiceHandleEvent_UsesGroupPolicyOverride(t *testing.T) {
	cfg := testAIConfig()
	cfg.Reply.ReplyOnAt = true
	cfg.Vision.Enabled = true
	cfg.GroupPolicies = []config.AIGroupPolicyConfig{
		{
			GroupID:         "10001",
			Name:            "轻松群",
			ReplyEnabled:    true,
			ReplyOnAt:       false,
			ReplyOnBotName:  true,
			ReplyOnQuote:    false,
			CooldownSeconds: 3,
			MaxContextMsgs:  20,
			MaxOutputTokens: 200,
			VisionEnabled:   false,
			PromptOverride:  "这是一个更轻松、允许玩梗的群。",
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	messenger := &stubMessenger{}
	service, err := NewService(cfg, testStorageConfig(), logger, messenger)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	gen := &stubGenerator{text: "群策略生效"}
	service.generator = gen

	service.HandleEvent(context.Background(), event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-group-policy",
		Timestamp:    time.Unix(1710000400, 0),
		Segments: []message.Segment{
			message.Text("罗纸酱，这是什么梗？"),
			{Type: "image", Data: map[string]any{"url": "https://example.com/meme.png"}},
		},
		Meta: map[string]string{"self_id": "123456789"},
	})

	if len(messenger.replies) != 1 {
		t.Fatalf("reply count = %d, want 1", len(messenger.replies))
	}
	foundGroupPrompt := false
	foundBotNameCueMode := false
	for _, item := range gen.last {
		content, ok := item.Content.(string)
		if !ok {
			continue
		}
		if strings.Contains(content, "当前群附加设定") && strings.Contains(content, "允许玩梗") {
			foundGroupPrompt = true
		}
		if strings.Contains(content, "回复模式：direct_answer") {
			foundBotNameCueMode = true
		}
		if strings.Contains(content, "图片识别：") {
			t.Fatalf("group policy disabled vision, but prompt contains vision text: %+v", gen.last)
		}
	}
	if !foundGroupPrompt {
		t.Fatalf("generator prompt = %+v, want group prompt override", gen.last)
	}
	if !foundBotNameCueMode {
		t.Fatalf("generator prompt = %+v, want direct mode from bot name cue trigger", gen.last)
	}
}

func TestServiceEvaluateGateLocked_BotNameCue(t *testing.T) {
	cfg := testAIConfig()
	cfg.Reply.ReplyOnAt = false
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	evt := event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-gate-bot-name",
		Timestamp:    time.Unix(1710000401, 0),
		Segments:     []message.Segment{message.Text("罗纸酱，帮我看看这个")},
		Meta:         map[string]string{"self_id": "123456789"},
	}
	result := service.evaluateGateLocked(cfg, evt, cleanEventText(evt), nil)
	if !result.ShouldReply {
		t.Fatalf("ShouldReply = false, want true; result = %+v", result)
	}
	if result.Mode != "direct_answer" {
		t.Fatalf("Mode = %q, want direct_answer", result.Mode)
	}
	if !strings.Contains(result.Reason, "机器人昵称") {
		t.Fatalf("Reason = %q, want bot name cue", result.Reason)
	}

	evt.Segments = []message.Segment{message.Text("今天大家聊什么？")}
	result = service.evaluateGateLocked(cfg, evt, cleanEventText(evt), nil)
	if result.ShouldReply {
		t.Fatalf("ShouldReply = true, want false when bot name is absent; result = %+v", result)
	}
}

func TestServiceEvaluateGateLocked_QuoteCue(t *testing.T) {
	cfg := testAIConfig()
	cfg.Reply.ReplyOnAt = false
	cfg.Reply.ReplyOnBotName = false
	cfg.Reply.ReplyOnQuote = true
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	evt := event.Event{
		Kind:         "message",
		ChatType:     "group",
		ConnectionID: "napcat-main",
		GroupID:      "10001",
		UserID:       "20002",
		MessageID:    "msg-gate-quote",
		Timestamp:    time.Unix(1710000402, 0),
		Segments: []message.Segment{
			message.Reply("bot-msg-1"),
			message.Text("接着上条继续说"),
		},
		Meta: map[string]string{"self_id": "123456789"},
	}
	result := service.evaluateGateLocked(cfg, evt, cleanEventText(evt), &SessionState{
		LastBotAction: &BotAction{Accepted: true, MessageID: "bot-msg-1", At: time.Unix(1710000401, 0)},
	})
	if !result.ShouldReply {
		t.Fatalf("ShouldReply = false, want true; result = %+v", result)
	}
	if result.Mode != "direct_answer" {
		t.Fatalf("Mode = %q, want direct_answer", result.Mode)
	}
	if !strings.Contains(result.Reason, "引用") {
		t.Fatalf("Reason = %q, want quote cue", result.Reason)
	}
}

func TestServiceManageMemories(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	now := time.Unix(1710000200, 0).UTC()
	candidate := &CandidateMemory{
		ID:            "candidate-manual",
		Scope:         "user_in_group",
		MemoryType:    "preference",
		Subtype:       "interest",
		SubjectID:     "20002",
		GroupID:       "10001",
		Content:       "用户喜欢 东方Project",
		Confidence:    0.85,
		EvidenceCount: 3,
		SourceMsgIDs:  []string{"msg-1", "msg-2"},
		Status:        "pending",
		TTLDays:       30,
		CreatedAt:     now,
		LastSeenAt:    now,
	}
	service.candidateMemories[candidateMemoryKey(*candidate)] = candidate

	if err := service.PromoteCandidateMemory(context.Background(), candidate.ID); err != nil {
		t.Fatalf("PromoteCandidateMemory() error = %v", err)
	}
	if service.candidateMemories[candidateMemoryKey(*candidate)].Status != "promoted" {
		t.Fatalf("candidate status = %q, want promoted", service.candidateMemories[candidateMemoryKey(*candidate)].Status)
	}
	if len(service.longTermMemories) != 1 {
		t.Fatalf("long term memory count = %d, want 1", len(service.longTermMemories))
	}

	if err := service.DeleteCandidateMemory(context.Background(), candidate.ID); err != nil {
		t.Fatalf("DeleteCandidateMemory() error = %v", err)
	}
	if len(service.candidateMemories) != 0 {
		t.Fatalf("candidate memory count = %d, want 0", len(service.candidateMemories))
	}

	var memoryID string
	for _, item := range service.longTermMemories {
		memoryID = item.ID
	}
	if memoryID == "" {
		t.Fatalf("memoryID is empty")
	}
	if err := service.DeleteLongTermMemory(context.Background(), memoryID); err != nil {
		t.Fatalf("DeleteLongTermMemory() error = %v", err)
	}
	if len(service.longTermMemories) != 0 {
		t.Fatalf("long term memory count = %d, want 0", len(service.longTermMemories))
	}
}

func TestServiceRunReflectionCycle_PromotesAndCleansExpiredMemories(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	stopReflectionLoopForTest(t, service)

	now := time.Now()
	promotable := CandidateMemory{
		ID:            "candidate-promote",
		Scope:         "group:10001",
		MemoryType:    "candidate",
		Subtype:       "preference",
		SubjectID:     "20001",
		GroupID:       "10001",
		Content:       "用户喜欢猫咪表情包",
		Confidence:    0.92,
		EvidenceCount: cfg.Memory.PromoteThreshold,
		Status:        "observed",
		TTLDays:       30,
		CreatedAt:     now.Add(-2 * time.Hour),
		LastSeenAt:    now.Add(-time.Hour),
	}
	expiredCandidate := CandidateMemory{
		ID:            "candidate-expired",
		Scope:         "group:10001",
		MemoryType:    "candidate",
		Subtype:       "meme",
		SubjectID:     "20002",
		GroupID:       "10001",
		Content:       "一次性梗",
		Confidence:    0.4,
		EvidenceCount: 1,
		Status:        "observed",
		TTLDays:       1,
		CreatedAt:     now.Add(-72 * time.Hour),
		LastSeenAt:    now.Add(-48 * time.Hour),
	}
	expiredLongTerm := LongTermMemory{
		ID:            "lt-expired",
		Scope:         "group:10001",
		MemoryType:    "semantic",
		Subtype:       "fact",
		SubjectID:     "20003",
		GroupID:       "10001",
		Content:       "早已过期的旧记忆",
		Confidence:    0.8,
		EvidenceCount: 3,
		TTLDays:       1,
		CreatedAt:     now.Add(-96 * time.Hour),
		UpdatedAt:     now.Add(-72 * time.Hour),
	}

	service.mu.Lock()
	service.candidateMemories[candidateMemoryKey(promotable)] = &promotable
	service.candidateMemories[candidateMemoryKey(expiredCandidate)] = &expiredCandidate
	service.longTermMemories[longTermMemoryKey(expiredLongTerm)] = &expiredLongTerm
	service.mu.Unlock()

	service.runReflectionCycle(context.Background())

	service.mu.RLock()
	defer service.mu.RUnlock()

	promotedCandidate, ok := service.candidateMemories[candidateMemoryKey(promotable)]
	if !ok {
		t.Fatalf("promotable candidate missing after reflection")
	}
	if promotedCandidate.Status != "promoted" {
		t.Fatalf("promoted candidate status = %q, want promoted", promotedCandidate.Status)
	}
	if _, ok := service.longTermMemories[memoryIdentityKey(promotable.Scope, "semantic", promotable.Subtype, promotable.GroupID, promotable.SubjectID, promotable.Content)]; !ok {
		t.Fatalf("expected promoted long term memory to be created")
	}
	if _, ok := service.candidateMemories[candidateMemoryKey(expiredCandidate)]; ok {
		t.Fatalf("expired candidate should be deleted")
	}
	if _, ok := service.longTermMemories[longTermMemoryKey(expiredLongTerm)]; ok {
		t.Fatalf("expired long term memory should be deleted")
	}
	if service.lastReflectionAt.IsZero() {
		t.Fatalf("lastReflectionAt is zero, want reflection timestamp")
	}
	if !strings.Contains(service.lastReflectionSummary, "晋升 1 条候选记忆") {
		t.Fatalf("lastReflectionSummary = %q, want promotion summary", service.lastReflectionSummary)
	}
	if service.lastReflectionError != "" {
		t.Fatalf("lastReflectionError = %q, want empty", service.lastReflectionError)
	}
	if service.lastReflectionStats.PromotedCount != 1 {
		t.Fatalf("PromotedCount = %d, want 1", service.lastReflectionStats.PromotedCount)
	}
	if service.lastReflectionStats.DeletedCandidateCount != 1 || service.lastReflectionStats.DeletedLongTermCount != 1 {
		t.Fatalf("reflection stats = %+v, want deleted candidate/long term counts", service.lastReflectionStats)
	}
}

func TestServiceRunReflectionCycle_MarksConflictingCandidates(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	stopReflectionLoopForTest(t, service)

	now := time.Now().UTC()
	left := CandidateMemory{
		ID:            "candidate-conflict-left",
		Scope:         "group:10001",
		MemoryType:    "preference",
		Subtype:       "interest",
		SubjectID:     "20001",
		GroupID:       "10001",
		Content:       "最喜欢红魔馆",
		Confidence:    0.86,
		EvidenceCount: cfg.Memory.PromoteThreshold + 1,
		Status:        "observed",
		TTLDays:       30,
		CreatedAt:     now.Add(-2 * time.Hour),
		LastSeenAt:    now.Add(-20 * time.Minute),
	}
	right := CandidateMemory{
		ID:            "candidate-conflict-right",
		Scope:         "group:10001",
		MemoryType:    "preference",
		Subtype:       "interest",
		SubjectID:     "20001",
		GroupID:       "10001",
		Content:       "最喜欢守矢神社",
		Confidence:    0.84,
		EvidenceCount: cfg.Memory.PromoteThreshold + 1,
		Status:        "observed",
		TTLDays:       30,
		CreatedAt:     now.Add(-90 * time.Minute),
		LastSeenAt:    now.Add(-10 * time.Minute),
	}

	service.mu.Lock()
	service.candidateMemories[candidateMemoryKey(left)] = &left
	service.candidateMemories[candidateMemoryKey(right)] = &right
	service.mu.Unlock()

	service.runReflectionCycle(context.Background())

	service.mu.RLock()
	leftAfter := service.candidateMemories[candidateMemoryKey(left)]
	rightAfter := service.candidateMemories[candidateMemoryKey(right)]
	_, leftLongTerm := service.longTermMemories[memoryIdentityKey(left.Scope, "semantic", left.Subtype, left.GroupID, left.SubjectID, left.Content)]
	_, rightLongTerm := service.longTermMemories[memoryIdentityKey(right.Scope, "semantic", right.Subtype, right.GroupID, right.SubjectID, right.Content)]
	summary := service.lastReflectionSummary
	service.mu.RUnlock()

	if leftAfter == nil || rightAfter == nil {
		t.Fatalf("conflict candidates missing after reflection")
	}
	if leftAfter.Status != "conflict" || rightAfter.Status != "conflict" {
		t.Fatalf("candidate status = %q / %q, want both conflict", leftAfter.Status, rightAfter.Status)
	}
	if leftLongTerm || rightLongTerm {
		t.Fatalf("conflict candidates should not be promoted to long-term memory")
	}

	snapshot := service.Snapshot()
	if snapshot.LastReflectionStats == nil {
		t.Fatalf("LastReflectionStats = nil, want reflection stats")
	}
	if snapshot.LastReflectionStats.ConflictCandidateCount < 2 {
		t.Fatalf("ConflictCandidateCount = %d, want >= 2", snapshot.LastReflectionStats.ConflictCandidateCount)
	}
	if !strings.Contains(summary, "标记冲突") {
		t.Fatalf("lastReflectionSummary = %q, want conflict summary", summary)
	}
}

func TestServiceRetrieveCandidateMemories_UsesStableCandidatesConservatively(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	stopReflectionLoopForTest(t, service)

	now := time.Now().UTC()
	stableMeme := CandidateMemory{
		ID:            "candidate-stable-meme",
		Scope:         "group:10001",
		MemoryType:    "group_meme",
		Subtype:       "meme",
		GroupID:       "10001",
		Content:       "群梗：红魔馆哈哈",
		Confidence:    0.8,
		EvidenceCount: cfg.Memory.PromoteThreshold + 1,
		Status:        "stable",
		TTLDays:       21,
		CreatedAt:     now.Add(-4 * time.Hour),
		LastSeenAt:    now.Add(-30 * time.Minute),
	}
	conflictPreference := CandidateMemory{
		ID:            "candidate-conflict",
		Scope:         "group:10001",
		MemoryType:    "preference",
		Subtype:       "interest",
		SubjectID:     "20001",
		GroupID:       "10001",
		Content:       "喜欢灵梦",
		Confidence:    0.83,
		EvidenceCount: cfg.Memory.PromoteThreshold + 1,
		Status:        "conflict",
		TTLDays:       30,
		CreatedAt:     now.Add(-3 * time.Hour),
		LastSeenAt:    now.Add(-20 * time.Minute),
	}
	pendingPreference := CandidateMemory{
		ID:            "candidate-pending",
		Scope:         "group:10001",
		MemoryType:    "preference",
		Subtype:       "interest",
		SubjectID:     "20001",
		GroupID:       "10001",
		Content:       "喜欢魔理沙",
		Confidence:    0.55,
		EvidenceCount: 1,
		Status:        "pending",
		TTLDays:       30,
		CreatedAt:     now.Add(-2 * time.Hour),
		LastSeenAt:    now.Add(-10 * time.Minute),
	}

	service.mu.Lock()
	service.candidateMemories[candidateMemoryKey(stableMeme)] = &stableMeme
	service.candidateMemories[candidateMemoryKey(conflictPreference)] = &conflictPreference
	service.candidateMemories[candidateMemoryKey(pendingPreference)] = &pendingPreference
	service.mu.Unlock()

	items := service.retrieveCandidateMemories(event.Event{ChatType: "group", GroupID: "10001", UserID: "20001"}, "红魔馆")
	if len(items) != 1 {
		t.Fatalf("candidate count = %d, want 1 stable candidate", len(items))
	}
	if items[0].ID != stableMeme.ID {
		t.Fatalf("first candidate = %q, want stable meme candidate", items[0].ID)
	}
}

func TestServicePersistInboundState_PersistsPendingCandidateMemory(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	stopReflectionLoopForTest(t, service)

	now := time.Unix(1710000300, 0).UTC()
	evt := event.Event{
		Kind:      "message",
		ChatType:  "group",
		GroupID:   "10001",
		UserID:    "20001",
		MessageID: "msg-preference-1",
		Timestamp: now,
	}
	text := "我喜欢东方Project"
	_, _, session, _, _, _, _, _ := service.prepareReply(evt, text)
	service.persistInboundState(context.Background(), evt, text, text, "", nil, session)

	candidates, err := service.store.LoadCandidateMemories(context.Background())
	if err != nil {
		t.Fatalf("LoadCandidateMemories() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1 pending candidate", len(candidates))
	}
	if candidates[0].Status != "pending" {
		t.Fatalf("candidate status = %q, want pending", candidates[0].Status)
	}
	if candidates[0].EvidenceCount != 1 {
		t.Fatalf("candidate evidence count = %d, want 1", candidates[0].EvidenceCount)
	}
	if candidates[0].Content != "用户喜欢 东方Project" {
		t.Fatalf("candidate content = %q, want captured preference", candidates[0].Content)
	}
}

func TestServiceRetrieveMemories_DoesNotUseGroupScopedMemoryInPrivateChat(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	stopReflectionLoopForTest(t, service)

	now := time.Now().UTC()
	groupMemory := LongTermMemory{
		ID:            "lt-group",
		Scope:         "user_in_group",
		MemoryType:    "semantic",
		Subtype:       "preference",
		SubjectID:     "20001",
		GroupID:       "10001",
		Content:       "用户喜欢群内梗",
		Confidence:    0.9,
		EvidenceCount: 3,
		TTLDays:       180,
		CreatedAt:     now.Add(-time.Hour),
		UpdatedAt:     now,
	}
	privateMemory := LongTermMemory{
		ID:            "lt-private",
		Scope:         "user",
		MemoryType:    "semantic",
		Subtype:       "preference",
		SubjectID:     "20001",
		Content:       "用户喜欢私聊话题",
		Confidence:    0.8,
		EvidenceCount: 2,
		TTLDays:       180,
		CreatedAt:     now.Add(-2 * time.Hour),
		UpdatedAt:     now.Add(-time.Minute),
	}
	groupCandidate := CandidateMemory{
		ID:            "candidate-group",
		Scope:         "user_in_group",
		MemoryType:    "preference",
		Subtype:       "interest",
		SubjectID:     "20001",
		GroupID:       "10001",
		Content:       "用户喜欢群内梗",
		Confidence:    0.82,
		EvidenceCount: cfg.Memory.PromoteThreshold,
		Status:        "stable",
		TTLDays:       30,
		CreatedAt:     now.Add(-time.Hour),
		LastSeenAt:    now,
	}

	service.mu.Lock()
	service.longTermMemories[longTermMemoryKey(groupMemory)] = &groupMemory
	service.longTermMemories[longTermMemoryKey(privateMemory)] = &privateMemory
	service.candidateMemories[candidateMemoryKey(groupCandidate)] = &groupCandidate
	service.mu.Unlock()

	privateEvent := event.Event{ChatType: "private", UserID: "20001"}
	memories := service.retrieveLongTermMemories(privateEvent, "私聊话题")
	if len(memories) != 1 {
		t.Fatalf("private long term memory count = %d, want 1", len(memories))
	}
	if memories[0].ID != privateMemory.ID {
		t.Fatalf("private long term memory ID = %q, want private memory", memories[0].ID)
	}
	candidates := service.retrieveCandidateMemories(privateEvent, "群内梗")
	if len(candidates) != 0 {
		t.Fatalf("private candidate count = %d, want 0 group-scoped candidates", len(candidates))
	}
}

func TestMemoryIdentityKeyIncludesTypeAndSubtype(t *testing.T) {
	left := CandidateMemory{
		Scope:      "user_in_group",
		MemoryType: "preference",
		Subtype:    "interest",
		SubjectID:  "20001",
		GroupID:    "10001",
		Content:    "same content",
	}
	right := CandidateMemory{
		Scope:      "user_in_group",
		MemoryType: "profile",
		Subtype:    "alias",
		SubjectID:  "20001",
		GroupID:    "10001",
		Content:    "same content",
	}
	if candidateMemoryKey(left) == candidateMemoryKey(right) {
		t.Fatalf("candidate memory keys should include memory type and subtype")
	}
}

func TestExtractMemoryCandidateSupportsMultipleUserMemoryTypes(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		memoryType string
		subtype    string
		content    string
	}{
		{
			name:       "positive preference",
			text:       "我喜欢东方Project",
			memoryType: "preference",
			subtype:    "interest",
			content:    "用户喜欢 东方Project",
		},
		{
			name:       "dislike",
			text:       "我讨厌剧透",
			memoryType: "preference",
			subtype:    "dislike",
			content:    "用户不喜欢 剧透",
		},
		{
			name:       "preferred name",
			text:       "以后叫我小罗纸",
			memoryType: "identity",
			subtype:    "preferred_name",
			content:    "用户希望被称呼为 小罗纸",
		},
		{
			name:       "correction",
			text:       "我不是管理员，我是路过的",
			memoryType: "identity",
			subtype:    "correction",
			content:    "用户说明自己是 路过的，不是 管理员",
		},
		{
			name:       "taboo topic",
			text:       "别跟我聊考试",
			memoryType: "boundary",
			subtype:    "taboo_topic",
			content:    "用户不想聊 考试",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item, ok := extractMemoryCandidate(tt.text)
			if !ok {
				t.Fatalf("extractMemoryCandidate() returned false")
			}
			if item.memoryType != tt.memoryType || item.subtype != tt.subtype || item.content != tt.content {
				t.Fatalf("captured memory = %+v, want %s/%s %q", item, tt.memoryType, tt.subtype, tt.content)
			}
		})
	}
}

func TestServiceRetrieveLongTermMemoriesScoresRelevantItems(t *testing.T) {
	cfg := testAIConfig()
	cfg.Memory.MaxPromptLongTerm = 2
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	stopReflectionLoopForTest(t, service)

	now := time.Now().UTC()
	relevantOld := LongTermMemory{
		ID:            "lt-relevant-old",
		Scope:         "user_in_group",
		MemoryType:    "semantic",
		Subtype:       "interest",
		SubjectID:     "20001",
		GroupID:       "10001",
		Content:       "用户喜欢 红魔馆",
		Confidence:    0.9,
		EvidenceCount: 4,
		TTLDays:       180,
		CreatedAt:     now.Add(-72 * time.Hour),
		UpdatedAt:     now.Add(-48 * time.Hour),
	}
	unrelatedNew := LongTermMemory{
		ID:            "lt-unrelated-new",
		Scope:         "user_in_group",
		MemoryType:    "semantic",
		Subtype:       "interest",
		SubjectID:     "20001",
		GroupID:       "10001",
		Content:       "用户喜欢 咖啡",
		Confidence:    0.7,
		EvidenceCount: 2,
		TTLDays:       180,
		CreatedAt:     now.Add(-2 * time.Hour),
		UpdatedAt:     now.Add(-time.Hour),
	}
	alsoRelevant := LongTermMemory{
		ID:            "lt-relevant-new",
		Scope:         "user_in_group",
		MemoryType:    "semantic",
		Subtype:       "interest",
		SubjectID:     "20001",
		GroupID:       "10001",
		Content:       "用户喜欢 东方Project",
		Confidence:    0.75,
		EvidenceCount: 2,
		TTLDays:       180,
		CreatedAt:     now.Add(-3 * time.Hour),
		UpdatedAt:     now.Add(-2 * time.Hour),
	}

	service.mu.Lock()
	service.longTermMemories[longTermMemoryKey(relevantOld)] = &relevantOld
	service.longTermMemories[longTermMemoryKey(unrelatedNew)] = &unrelatedNew
	service.longTermMemories[longTermMemoryKey(alsoRelevant)] = &alsoRelevant
	service.mu.Unlock()

	items := service.retrieveLongTermMemories(event.Event{ChatType: "group", GroupID: "10001", UserID: "20001"}, "红魔馆今天怎么打")
	if len(items) != 2 {
		t.Fatalf("long term memory count = %d, want 2", len(items))
	}
	if items[0].ID != relevantOld.ID {
		t.Fatalf("first long term memory = %q, want relevant old memory", items[0].ID)
	}
}

func TestServiceLoadReflectionSessionsUsesConfiguredRawWindow(t *testing.T) {
	cfg := testAIConfig()
	cfg.Memory.ReflectionRawLimit = 1
	cfg.Memory.ReflectionPerGroupLimit = 1
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	stopReflectionLoopForTest(t, service)

	oldMessage := RawMessageLog{
		MessageID:    "raw-old",
		ConnectionID: "conn-1",
		ChatType:     "group",
		GroupID:      "10001",
		UserID:       "20001",
		ContentText:  "旧消息",
		CreatedAt:    time.Unix(1710000000, 0).UTC(),
	}
	newMessage := RawMessageLog{
		MessageID:    "raw-new",
		ConnectionID: "conn-1",
		ChatType:     "group",
		GroupID:      "10001",
		UserID:       "20002",
		ContentText:  "新消息",
		CreatedAt:    time.Unix(1710000600, 0).UTC(),
	}
	if err := service.store.AppendRawMessage(context.Background(), oldMessage); err != nil {
		t.Fatalf("AppendRawMessage(old) error = %v", err)
	}
	if err := service.store.AppendRawMessage(context.Background(), newMessage); err != nil {
		t.Fatalf("AppendRawMessage(new) error = %v", err)
	}

	sessions, err := service.loadReflectionSessions(context.Background())
	if err != nil {
		t.Fatalf("loadReflectionSessions() error = %v", err)
	}
	if len(sessions) != 1 || len(sessions[0].Recent) != 1 {
		t.Fatalf("sessions = %+v, want one reflected message", sessions)
	}
	if sessions[0].Recent[0].MessageID != newMessage.MessageID {
		t.Fatalf("reflected message = %q, want latest configured raw window message", sessions[0].Recent[0].MessageID)
	}
}

func TestServiceClose_StopsReflectionLoop(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if snapshot := service.Snapshot(); !snapshot.ReflectionRunning {
		t.Fatalf("ReflectionRunning = false, want true before close")
	}
	if err := service.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if snapshot := service.Snapshot(); snapshot.ReflectionRunning {
		t.Fatalf("ReflectionRunning = true, want false after close")
	}
}

func TestServiceRunReflectionOnce_RefreshesGroupProfileFromRawMessages(t *testing.T) {
	cfg := testAIConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service, err := NewService(cfg, testStorageConfig(), logger, &stubMessenger{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })

	store := service.store
	if store == nil {
		t.Fatalf("store is nil, want sqlite store")
	}
	stopReflectionLoopForTest(t, service)

	base := time.Now().Add(-15 * time.Minute)
	rawItems := []RawMessageLog{
		{MessageID: "raw-1", ConnectionID: "napcat-main", ChatType: "group", GroupID: "10001", UserID: "20001", ContentText: "红魔馆哈哈", CreatedAt: base},
		{MessageID: "raw-2", ConnectionID: "napcat-main", ChatType: "group", GroupID: "10001", UserID: "20002", ContentText: "红魔馆哈哈", CreatedAt: base.Add(time.Minute)},
		{MessageID: "raw-3", ConnectionID: "napcat-main", ChatType: "group", GroupID: "10001", UserID: "20003", ContentText: "今天继续红魔馆😀", CreatedAt: base.Add(2 * time.Minute)},
		{MessageID: "raw-4", ConnectionID: "napcat-main", ChatType: "group", GroupID: "10001", UserID: "20004", ContentText: "别刷屏红魔馆", CreatedAt: base.Add(3 * time.Minute)},
	}
	for _, item := range rawItems {
		if err := store.AppendRawMessage(context.Background(), item); err != nil {
			t.Fatalf("AppendRawMessage() error = %v", err)
		}
	}

	summary, err := service.RunReflectionOnce(context.Background())
	if err != nil {
		t.Fatalf("RunReflectionOnce() error = %v", err)
	}
	if !strings.Contains(summary, "更新 1 个群画像") || !strings.Contains(summary, "沉淀 1 条群梗候选") {
		t.Fatalf("summary = %q, want reflected group + meme summary", summary)
	}

	service.mu.RLock()
	profile := service.groupProfiles["10001"]
	var memeCandidate *CandidateMemory
	for _, item := range service.candidateMemories {
		if item != nil && item.GroupID == "10001" && item.MemoryType == "group_meme" {
			copied := *item
			memeCandidate = &copied
			break
		}
	}
	service.mu.RUnlock()
	if profile == nil {
		t.Fatalf("group profile not reflected from raw messages")
	}
	if !containsSubstring(profile.TopicFocus, "红魔馆") {
		t.Fatalf("TopicFocus = %+v, want token containing 红魔馆", profile.TopicFocus)
	}
	if !containsString(profile.ActiveMemes, "红魔馆哈哈") {
		t.Fatalf("ActiveMemes = %+v, want 红魔馆哈哈", profile.ActiveMemes)
	}
	if !containsString(profile.SoftRules, "别刷屏") {
		t.Fatalf("SoftRules = %+v, want 别刷屏", profile.SoftRules)
	}
	if profile.HumorDensity <= 0 {
		t.Fatalf("HumorDensity = %f, want > 0", profile.HumorDensity)
	}
	if !strings.Contains(profile.ReflectionSummary, "红魔馆") {
		t.Fatalf("ReflectionSummary = %q, want reflected summary", profile.ReflectionSummary)
	}
	if memeCandidate == nil {
		t.Fatalf("group meme candidate not created")
	}
	if memeCandidate.Content != "群梗：红魔馆哈哈" || memeCandidate.EvidenceCount < 2 {
		t.Fatalf("memeCandidate = %+v, want reflected group meme candidate", memeCandidate)
	}
	if memeCandidate.Status != "warming" {
		t.Fatalf("memeCandidate.Status = %q, want warming", memeCandidate.Status)
	}
	snapshot := service.Snapshot()
	if snapshot.LastReflectionStats == nil || snapshot.LastReflectionStats.UpdatedGroupCount != 1 || snapshot.LastReflectionStats.ReflectedMemeCount != 1 {
		t.Fatalf("LastReflectionStats = %+v, want reflected group/meme counts", snapshot.LastReflectionStats)
	}
	debugView := service.DebugView(10)
	if len(debugView.GroupObservations) != 1 {
		t.Fatalf("GroupObservations count = %d, want 1", len(debugView.GroupObservations))
	}
	observation := debugView.GroupObservations[0]
	if observation.GroupID != "10001" {
		t.Fatalf("GroupObservation.GroupID = %q, want 10001", observation.GroupID)
	}
	if !strings.Contains(observation.Summary, "红魔馆") {
		t.Fatalf("GroupObservation.Summary = %q, want 红魔馆", observation.Summary)
	}
	if len(observation.CandidateHighlights) == 0 {
		t.Fatalf("CandidateHighlights is empty, want reflected group meme highlight")
	}
	if !containsSubstring(observation.RiskFlags, "观察期") {
		t.Fatalf("RiskFlags = %+v, want observation warning", observation.RiskFlags)
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func findToolDefinitionForTest(t *testing.T, service *Service, evt event.Event, name string) toolDefinition {
	t.Helper()

	for _, item := range service.buildToolDefinitions(evt) {
		if item.Spec.Name == name {
			return item
		}
	}
	t.Fatalf("tool %s not found in buildToolDefinitions(%+v)", name, evt)
	return toolDefinition{}
}

func containsSubstring(items []string, target string) bool {
	for _, item := range items {
		if strings.Contains(item, target) {
			return true
		}
	}
	return false
}

func stopReflectionLoopForTest(t *testing.T, service *Service) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for {
		service.mu.RLock()
		busy := service.reflectionCycleActive
		cancel := service.reflectionCancel
		service.mu.RUnlock()
		if !busy {
			if cancel != nil {
				service.mu.Lock()
				cancel = service.reflectionCancel
				service.reflectionCancel = nil
				service.reflectionRunning = false
				service.mu.Unlock()
				if cancel != nil {
					cancel()
					service.reflectionWG.Wait()
				}
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("reflection cycle still active before manual test run")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func testAIConfig() config.AIConfig {
	return config.AIConfig{
		Enabled: true,
		Provider: config.AIProviderConfig{
			Kind:        "openai_compatible",
			Vendor:      "custom",
			BaseURL:     "http://127.0.0.1:18080/v1",
			Model:       "gpt-test",
			TimeoutMS:   30000,
			Temperature: 0.7,
		},
		Vision: config.AIVisionConfig{
			Enabled: false,
			Mode:    "same_as_chat",
		},
		Reply: config.AIReplyConfig{
			EnabledInGroup:   true,
			EnabledInPrivate: true,
			ReplyOnAt:        true,
			ReplyOnBotName:   true,
			ReplyOnQuote:     false,
			CooldownSeconds:  5,
			MaxContextMsgs:   12,
			MaxOutputTokens:  128,
		},
		Memory: config.AIMemoryConfig{
			Enabled:                 true,
			SessionWindow:           16,
			CandidateEnabled:        true,
			PromoteThreshold:        2,
			MaxPromptLongTerm:       4,
			MaxPromptCandidates:     3,
			ReflectionRawLimit:      768,
			ReflectionPerGroupLimit: 36,
		},
		CLI: config.AICLIConfig{
			Enabled:         false,
			AllowedCommands: nil,
			TimeoutSeconds:  10,
			MaxOutputBytes:  8192,
		},
		Prompt: config.AIPromptConfig{
			BotName:      "罗纸酱",
			SystemPrompt: "你是测试机器人。",
		},
	}
}

func testStorageConfig() config.StorageConfig {
	return config.StorageConfig{
		Engine: "sqlite",
		SQLite: config.SQLiteConfig{Path: ":memory:"},
		Logs: config.LogsConfig{
			Dir:        "./data/logs",
			MaxSizeMB:  10,
			MaxBackups: 3,
			MaxAgeDays: 7,
		},
	}
}
