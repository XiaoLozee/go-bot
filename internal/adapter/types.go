package adapter

import (
	"context"
	"encoding/json"
	"time"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
)

type ConnectionState string

const (
	ConnectionStopped ConnectionState = "stopped"
	ConnectionRunning ConnectionState = "running"
	ConnectionFailed  ConnectionState = "failed"
)

type ConnectionSnapshot struct {
	ID               string          `json:"id"`
	Platform         string          `json:"platform"`
	Enabled          bool            `json:"enabled"`
	IngressType      string          `json:"ingress_type"`
	ActionType       string          `json:"action_type"`
	State            ConnectionState `json:"state"`
	IngressState     ConnectionState `json:"ingress_state"`
	Online           bool            `json:"online"`
	Good             bool            `json:"good"`
	ConnectedClients int             `json:"connected_clients"`
	ObservedEvents   int             `json:"observed_events"`
	LastEventAt      time.Time       `json:"last_event_at,omitempty"`
	SelfID           string          `json:"self_id,omitempty"`
	SelfNickname     string          `json:"self_nickname,omitempty"`
	LastError        string          `json:"last_error,omitempty"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type IngressSnapshot struct {
	ID               string          `json:"id"`
	Type             string          `json:"type"`
	State            ConnectionState `json:"state"`
	Listen           string          `json:"listen,omitempty"`
	Path             string          `json:"path,omitempty"`
	ConnectedClients int             `json:"connected_clients"`
	ObservedEvents   int             `json:"observed_events"`
	LastEventAt      time.Time       `json:"last_event_at,omitempty"`
	LastError        string          `json:"last_error,omitempty"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type EventIngress interface {
	ID() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Events() <-chan event.Event
	Snapshot() IngressSnapshot
}

type ActionClient interface {
	ID() string
	SendMessage(ctx context.Context, req SendMessageRequest) (*SendMessageResult, error)
	SendGroupForwardMessage(ctx context.Context, req SendGroupForwardRequest) (*SendMessageResult, error)
	DeleteMessage(ctx context.Context, messageID string) error
	GetMessage(ctx context.Context, messageID string) (*MessageDetail, error)
	ResolveMedia(ctx context.Context, segmentType, file string) (*ResolvedMedia, error)
	GetLoginInfo(ctx context.Context) (*LoginInfo, error)
	GetStatus(ctx context.Context) (*BotStatus, error)
	GetStrangerInfo(ctx context.Context, userID string) (*UserInfo, error)
	GetGroupMemberList(ctx context.Context, groupID string) ([]GroupMemberInfo, error)
	GetGroupMemberInfo(ctx context.Context, groupID, userID string) (*GroupMemberInfo, error)
}

type SendMessageRequest struct {
	ConnectionID string
	ChatType     string
	UserID       string
	GroupID      string
	Segments     []message.Segment
	AutoEscape   bool
}

type SendMessageResult struct {
	MessageID string `json:"message_id"`
	ForwardID string `json:"forward_id,omitempty"`
}

type SendGroupForwardRequest struct {
	ConnectionID string                 `json:"connection_id"`
	GroupID      string                 `json:"group_id"`
	Nodes        []message.ForwardNode  `json:"nodes"`
	Options      message.ForwardOptions `json:"options"`
}

type RecentMessagesRequest struct {
	ConnectionID string `json:"connection_id,omitempty"`
	ChatType     string `json:"chat_type"`
	GroupID      string `json:"group_id,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Count        int    `json:"count,omitempty"`
}

type MessageDetail struct {
	Time        time.Time       `json:"time"`
	MessageType string          `json:"message_type"`
	MessageID   string          `json:"message_id"`
	UserID      string          `json:"user_id"`
	GroupID     string          `json:"group_id,omitempty"`
	RawMessage  string          `json:"raw_message"`
	Message     json.RawMessage `json:"message"`
	Sender      map[string]any  `json:"sender,omitempty"`
}

type ForwardMessage struct {
	ID    string               `json:"id"`
	Nodes []ForwardMessageNode `json:"nodes"`
}

type ForwardMessageNode struct {
	Time      time.Time         `json:"time,omitempty"`
	MessageID string            `json:"message_id,omitempty"`
	UserID    string            `json:"user_id,omitempty"`
	Nickname  string            `json:"nickname,omitempty"`
	Content   []message.Segment `json:"content"`
}

type ResolvedMedia struct {
	File     string `json:"file,omitempty"`
	URL      string `json:"url,omitempty"`
	FileName string `json:"file_name,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

type LoginInfo struct {
	UserID   string `json:"user_id"`
	Nickname string `json:"nickname"`
}

type BotStatus struct {
	Online bool           `json:"online"`
	Good   bool           `json:"good"`
	Stat   map[string]any `json:"stat,omitempty"`
}

type UserInfo struct {
	UserID   string `json:"user_id"`
	Nickname string `json:"nickname"`
	Sex      string `json:"sex,omitempty"`
	Age      int    `json:"age,omitempty"`
}

type GroupInfo struct {
	GroupID        string `json:"group_id"`
	GroupName      string `json:"group_name"`
	MemberCount    int    `json:"member_count,omitempty"`
	MaxMemberCount int    `json:"max_member_count,omitempty"`
}

type GroupMemberInfo struct {
	GroupID  string `json:"group_id,omitempty"`
	UserID   string `json:"user_id"`
	Nickname string `json:"nickname"`
	Card     string `json:"card,omitempty"`
	Role     string `json:"role,omitempty"`
	Sex      string `json:"sex,omitempty"`
	Age      int    `json:"age,omitempty"`
	Level    string `json:"level,omitempty"`
	Title    string `json:"title,omitempty"`
	Area     string `json:"area,omitempty"`
	JoinTime int64  `json:"join_time,omitempty"`
	LastSent int64  `json:"last_sent_time,omitempty"`
}

type ActionError struct {
	Endpoint string `json:"endpoint"`
	RetCode  int64  `json:"retcode"`
	Message  string `json:"message"`
	Wording  string `json:"wording"`
	Cause    error  `json:"-"`
}

func (e *ActionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return e.Endpoint + ": " + e.Message + " (" + e.Cause.Error() + ")"
	}
	return e.Endpoint + ": " + e.Message
}
