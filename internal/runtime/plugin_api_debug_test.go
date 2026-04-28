package runtime

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

type debugActionClient struct {
	id               string
	sendRequests     []adapter.SendMessageRequest
	forwardRequests  []adapter.SendGroupForwardRequest
	deletedMessageID string
	messageDetail    *adapter.MessageDetail
	resolvedMedia    *adapter.ResolvedMedia
	loginInfo        *adapter.LoginInfo
	status           *adapter.BotStatus
	strangerInfo     *adapter.UserInfo
	groupInfo        *adapter.GroupInfo
	groupMembers     []adapter.GroupMemberInfo
	groupMemberInfo  *adapter.GroupMemberInfo
	forwardMessage   *adapter.ForwardMessage
}

func (c *debugActionClient) ID() string { return c.id }

func (c *debugActionClient) SendMessage(_ context.Context, req adapter.SendMessageRequest) (*adapter.SendMessageResult, error) {
	c.sendRequests = append(c.sendRequests, req)
	return &adapter.SendMessageResult{MessageID: "sent-1"}, nil
}

func (c *debugActionClient) SendGroupForwardMessage(_ context.Context, req adapter.SendGroupForwardRequest) (*adapter.SendMessageResult, error) {
	c.forwardRequests = append(c.forwardRequests, req)
	return &adapter.SendMessageResult{MessageID: "forward-1"}, nil
}

func (c *debugActionClient) DeleteMessage(_ context.Context, messageID string) error {
	c.deletedMessageID = messageID
	return nil
}

func (c *debugActionClient) GetMessage(context.Context, string) (*adapter.MessageDetail, error) {
	return c.messageDetail, nil
}

func (c *debugActionClient) ResolveMedia(context.Context, string, string) (*adapter.ResolvedMedia, error) {
	return c.resolvedMedia, nil
}

func (c *debugActionClient) GetLoginInfo(context.Context) (*adapter.LoginInfo, error) {
	return c.loginInfo, nil
}

func (c *debugActionClient) GetStatus(context.Context) (*adapter.BotStatus, error) {
	return c.status, nil
}

func (c *debugActionClient) GetStrangerInfo(context.Context, string) (*adapter.UserInfo, error) {
	return c.strangerInfo, nil
}

func (c *debugActionClient) GetGroupInfo(context.Context, string) (*adapter.GroupInfo, error) {
	return c.groupInfo, nil
}

func (c *debugActionClient) GetGroupMemberList(context.Context, string) ([]adapter.GroupMemberInfo, error) {
	return c.groupMembers, nil
}

func (c *debugActionClient) GetGroupMemberInfo(context.Context, string, string) (*adapter.GroupMemberInfo, error) {
	return c.groupMemberInfo, nil
}

func (c *debugActionClient) GetForwardMessage(context.Context, string) (*adapter.ForwardMessage, error) {
	return c.forwardMessage, nil
}

func newPluginDebugTestService(t *testing.T) *Service {
	t.Helper()

	service, err := New(testRuntimeConfig(), filepath.Join(t.TempDir(), "config.example.yml"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return service
}

func TestServiceDebugFrameworkPluginAPI_SupportsExtendedHostMethods(t *testing.T) {
	service := newPluginDebugTestService(t)
	client := &debugActionClient{
		id: "conn-1",
		messageDetail: &adapter.MessageDetail{
			Time:        time.Unix(1710000000, 0),
			MessageType: "group",
			MessageID:   "msg-1",
			UserID:      "user-1",
			GroupID:     "group-1",
			RawMessage:  "hello",
			Message:     json.RawMessage(`[{"type":"text","data":{"text":"hello"}}]`),
			Sender:      map[string]any{"nickname": "Alice"},
		},
		resolvedMedia: &adapter.ResolvedMedia{
			URL:      "https://example.com/demo.jpg",
			FileName: "demo.jpg",
			FileSize: 2048,
		},
		loginInfo: &adapter.LoginInfo{
			UserID:   "bot-1",
			Nickname: "Go-bot",
		},
		status: &adapter.BotStatus{
			Online: true,
			Good:   true,
			Stat:   map[string]any{"packet_received": 42},
		},
		strangerInfo: &adapter.UserInfo{
			UserID:   "user-1",
			Nickname: "Alice",
		},
		groupInfo: &adapter.GroupInfo{
			GroupID:        "group-1",
			GroupName:      "Test Group",
			MemberCount:    3,
			MaxMemberCount: 200,
		},
		groupMembers: []adapter.GroupMemberInfo{
			{GroupID: "group-1", UserID: "user-1", Nickname: "Alice"},
		},
		groupMemberInfo: &adapter.GroupMemberInfo{
			GroupID:  "group-1",
			UserID:   "user-1",
			Nickname: "Alice",
			Role:     "admin",
		},
		forwardMessage: &adapter.ForwardMessage{
			ID: "forward-1",
			Nodes: []adapter.ForwardMessageNode{{
				UserID:   "user-1",
				Nickname: "Alice",
				Content:  []message.Segment{message.Text("hello")},
			}},
		},
	}
	service.messenger.Replace([]adapter.ActionClient{client}, client.id)

	cases := []struct {
		name   string
		req    PluginAPIDebugRequest
		assert func(t *testing.T, result PluginAPIDebugResult)
	}{
		{
			name: "get_group_info",
			req: PluginAPIDebugRequest{
				Method: "bot.get_group_info",
				Payload: map[string]any{
					"connection_id": "conn-1",
					"group_id":      "group-1",
				},
			},
			assert: func(t *testing.T, result PluginAPIDebugResult) {
				item, ok := result.Result.(*sdk.GroupInfo)
				if !ok || item == nil || item.GroupName != "Test Group" {
					t.Fatalf("Result = %#v, want Test Group group info", result.Result)
				}
			},
		},
		{
			name: "get_group_member_info",
			req: PluginAPIDebugRequest{
				Method: "bot.get_group_member_info",
				Payload: map[string]any{
					"connection_id": "conn-1",
					"group_id":      "group-1",
					"user_id":       "user-1",
				},
			},
			assert: func(t *testing.T, result PluginAPIDebugResult) {
				item, ok := result.Result.(*sdk.GroupMemberInfo)
				if !ok || item == nil || item.UserID != "user-1" {
					t.Fatalf("Result = %#v, want user-1 group member info", result.Result)
				}
			},
		},
		{
			name: "get_message",
			req: PluginAPIDebugRequest{
				Method: "bot.get_message",
				Payload: map[string]any{
					"connection_id": "conn-1",
					"message_id":    "msg-1",
				},
			},
			assert: func(t *testing.T, result PluginAPIDebugResult) {
				item, ok := result.Result.(*sdk.MessageDetail)
				if !ok || item == nil || item.MessageID != "msg-1" {
					t.Fatalf("Result = %#v, want msg-1 message detail", result.Result)
				}
			},
		},
		{
			name: "get_forward_message",
			req: PluginAPIDebugRequest{
				Method: "bot.get_forward_message",
				Payload: map[string]any{
					"connection_id": "conn-1",
					"forward_id":    "forward-1",
				},
			},
			assert: func(t *testing.T, result PluginAPIDebugResult) {
				item, ok := result.Result.(*sdk.ForwardMessage)
				if !ok || item == nil || item.ID != "forward-1" || len(item.Nodes) != 1 {
					t.Fatalf("Result = %#v, want forward-1 message", result.Result)
				}
			},
		},
		{
			name: "resolve_media",
			req: PluginAPIDebugRequest{
				Method: "bot.resolve_media",
				Payload: map[string]any{
					"connection_id": "conn-1",
					"segment_type":  "image",
					"file":          "demo.jpg",
				},
			},
			assert: func(t *testing.T, result PluginAPIDebugResult) {
				item, ok := result.Result.(*sdk.ResolvedMedia)
				if !ok || item == nil || item.FileName != "demo.jpg" {
					t.Fatalf("Result = %#v, want resolved media info", result.Result)
				}
			},
		},
		{
			name: "get_login_info",
			req: PluginAPIDebugRequest{
				Method: "bot.get_login_info",
				Payload: map[string]any{
					"connection_id": "conn-1",
				},
			},
			assert: func(t *testing.T, result PluginAPIDebugResult) {
				item, ok := result.Result.(*sdk.LoginInfo)
				if !ok || item == nil || item.UserID != "bot-1" {
					t.Fatalf("Result = %#v, want bot login info", result.Result)
				}
			},
		},
		{
			name: "get_status",
			req: PluginAPIDebugRequest{
				Method: "bot.get_status",
				Payload: map[string]any{
					"connection_id": "conn-1",
				},
			},
			assert: func(t *testing.T, result PluginAPIDebugResult) {
				item, ok := result.Result.(*sdk.BotStatus)
				if !ok || item == nil || !item.Online {
					t.Fatalf("Result = %#v, want online=true", result.Result)
				}
			},
		},
		{
			name: "delete_message",
			req: PluginAPIDebugRequest{
				Method: "bot.delete_message",
				Payload: map[string]any{
					"connection_id": "conn-1",
					"message_id":    "msg-1",
				},
			},
			assert: func(t *testing.T, result PluginAPIDebugResult) {
				if client.deletedMessageID != "msg-1" {
					t.Fatalf("deletedMessageID = %q, want msg-1", client.deletedMessageID)
				}
				item, ok := result.Result.(map[string]any)
				if !ok || item["deleted"] != true {
					t.Fatalf("Result = %#v, want deleted=true", result.Result)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := service.DebugFrameworkPluginAPI(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("DebugFrameworkPluginAPI() error = %v", err)
			}
			if !result.Accepted || result.Error != "" {
				t.Fatalf("result = %+v, want accepted success result", result)
			}
			tc.assert(t, result)
		})
	}
}

func TestServiceDebugFrameworkPluginAPI_SupportsMessengerSendText(t *testing.T) {
	service := newPluginDebugTestService(t)
	client := &debugActionClient{id: "conn-1"}
	service.messenger.Replace([]adapter.ActionClient{client}, client.id)

	result, err := service.DebugFrameworkPluginAPI(context.Background(), PluginAPIDebugRequest{
		Method: "messenger.send_text",
		Payload: map[string]any{
			"target": map[string]any{
				"connection_id": "conn-1",
				"chat_type":     "private",
				"user_id":       "user-1",
			},
			"text": "hello",
		},
	})
	if err != nil {
		t.Fatalf("DebugFrameworkPluginAPI() error = %v", err)
	}
	if !result.Accepted || result.Error != "" {
		t.Fatalf("result = %+v, want accepted success result", result)
	}
	if len(client.sendRequests) != 1 {
		t.Fatalf("sendRequests len = %d, want 1", len(client.sendRequests))
	}
	req := client.sendRequests[0]
	if req.ChatType != "private" || req.UserID != "user-1" {
		t.Fatalf("send request = %+v, want private target", req)
	}
	if len(req.Segments) != 1 || req.Segments[0].Type != "text" || req.Segments[0].Data["text"] != "hello" {
		t.Fatalf("segments = %+v, want single text segment", req.Segments)
	}
}
