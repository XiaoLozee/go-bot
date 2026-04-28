package externalexec

import (
	"encoding/json"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

const (
	CallBotGetStrangerInfo   = "bot.get_stranger_info"
	CallBotGetGroupInfo      = "bot.get_group_info"
	CallBotGetGroupMembers   = "bot.get_group_member_list"
	CallBotGetGroupMember    = "bot.get_group_member_info"
	CallBotGetMessage        = "bot.get_message"
	CallBotGetForwardMessage = "bot.get_forward_message"
	CallBotDeleteMessage     = "bot.delete_message"
	CallBotResolveMedia      = "bot.resolve_media"
	CallBotGetLoginInfo      = "bot.get_login_info"
	CallBotGetStatus         = "bot.get_status"
	CallBotSendGroupForward  = "bot.send_group_forward"
)

type hostMessage struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

type rawHostMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type startPayload struct {
	Plugin  sdk.Manifest     `json:"plugin"`
	Config  map[string]any   `json:"config,omitempty"`
	Catalog []sdk.PluginInfo `json:"catalog,omitempty"`
	App     sdk.AppInfo      `json:"app,omitempty"`
}

type eventPayload struct {
	Event event.Event `json:"event"`
}

type hostResponsePayload struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type pluginMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type readyPayload struct {
	Message string `json:"message,omitempty"`
}

type logPayload struct {
	Level   string `json:"level,omitempty"`
	Message string `json:"message"`
}

type sendTextPayload struct {
	Target message.Target `json:"target"`
	Text   string         `json:"text"`
}

type replyTextPayload struct {
	Target  message.Target `json:"target"`
	ReplyTo string         `json:"reply_to"`
	Text    string         `json:"text"`
}

type sendSegmentsPayload struct {
	Target   message.Target    `json:"target"`
	Segments []message.Segment `json:"segments"`
}

type callPayload struct {
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type aiToolDefinitionPayload struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type registerAIToolsPayload struct {
	ID        string                    `json:"id,omitempty"`
	Namespace string                    `json:"namespace,omitempty"`
	Tools     []aiToolDefinitionPayload `json:"tools,omitempty"`
}

type unregisterAIToolsPayload struct {
	ID        string `json:"id,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type aiToolContextPayload struct {
	Event   event.Event    `json:"event"`
	Target  message.Target `json:"target"`
	ReplyTo string         `json:"reply_to,omitempty"`
}

type scheduledSendPayload struct {
	Text  string `json:"text"`
	Reply bool   `json:"reply,omitempty"`
}

type aiToolCallPayload struct {
	ID        string               `json:"id"`
	ToolName  string               `json:"tool_name"`
	Arguments json.RawMessage      `json:"arguments,omitempty"`
	Context   aiToolContextPayload `json:"context"`
}

type aiToolResultPayload struct {
	ID        string                `json:"id"`
	Result    json.RawMessage       `json:"result,omitempty"`
	Error     string                `json:"error,omitempty"`
	Scheduled *scheduledSendPayload `json:"scheduled,omitempty"`
}

type getStrangerInfoPayload struct {
	ConnectionID string `json:"connection_id"`
	UserID       string `json:"user_id"`
}

type getGroupInfoPayload struct {
	ConnectionID string `json:"connection_id"`
	GroupID      string `json:"group_id"`
}

type getGroupMemberListPayload struct {
	ConnectionID string `json:"connection_id"`
	GroupID      string `json:"group_id"`
}

type getGroupMemberInfoPayload struct {
	ConnectionID string `json:"connection_id"`
	GroupID      string `json:"group_id"`
	UserID       string `json:"user_id"`
}

type getMessagePayload struct {
	ConnectionID string `json:"connection_id"`
	MessageID    string `json:"message_id"`
}

type getForwardMessagePayload struct {
	ConnectionID string `json:"connection_id"`
	ForwardID    string `json:"forward_id"`
}

type deleteMessagePayload struct {
	ConnectionID string `json:"connection_id"`
	MessageID    string `json:"message_id"`
}

type resolveMediaPayload struct {
	ConnectionID string `json:"connection_id"`
	SegmentType  string `json:"segment_type"`
	File         string `json:"file"`
}

type getLoginInfoPayload struct {
	ConnectionID string `json:"connection_id"`
}

type getStatusPayload struct {
	ConnectionID string `json:"connection_id"`
}

type sendGroupForwardPayload struct {
	ConnectionID string                 `json:"connection_id"`
	GroupID      string                 `json:"group_id"`
	Nodes        []message.ForwardNode  `json:"nodes"`
	Options      message.ForwardOptions `json:"options,omitempty"`
}

func marshalPayload(payload any) (json.RawMessage, error) {
	if payload == nil {
		return nil, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}
