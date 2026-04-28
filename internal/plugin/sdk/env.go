package sdk

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
)

type PluginInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Enabled     bool   `json:"enabled"`
	Builtin     bool   `json:"builtin"`
}

type PluginCatalog interface {
	ListPlugins() []PluginInfo
}

type AppInfo struct {
	Name        string `json:"name,omitempty"`
	Environment string `json:"environment,omitempty"`
	OwnerQQ     string `json:"owner_qq,omitempty"`
}

func (a AppInfo) HasOwner() bool {
	return strings.TrimSpace(a.OwnerQQ) != ""
}

func (a AppInfo) IsOwner(userID string) bool {
	ownerQQ := strings.TrimSpace(a.OwnerQQ)
	return ownerQQ != "" && ownerQQ == strings.TrimSpace(userID)
}

type UserInfo struct {
	UserID   string `json:"user_id"`
	Nickname string `json:"nickname"`
	Sex      string `json:"sex,omitempty"`
	Age      int    `json:"age,omitempty"`
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

type GroupInfo struct {
	GroupID        string `json:"group_id"`
	GroupName      string `json:"group_name"`
	MemberCount    int    `json:"member_count,omitempty"`
	MaxMemberCount int    `json:"max_member_count,omitempty"`
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

type BotAPI interface {
	GetStrangerInfo(ctx context.Context, connectionID, userID string) (*UserInfo, error)
	GetGroupInfo(ctx context.Context, connectionID, groupID string) (*GroupInfo, error)
	GetGroupMemberList(ctx context.Context, connectionID, groupID string) ([]GroupMemberInfo, error)
	GetGroupMemberInfo(ctx context.Context, connectionID, groupID, userID string) (*GroupMemberInfo, error)
	GetMessage(ctx context.Context, connectionID, messageID string) (*MessageDetail, error)
	GetForwardMessage(ctx context.Context, connectionID, forwardID string) (*ForwardMessage, error)
	DeleteMessage(ctx context.Context, connectionID, messageID string) error
	ResolveMedia(ctx context.Context, connectionID, segmentType, file string) (*ResolvedMedia, error)
	GetLoginInfo(ctx context.Context, connectionID string) (*LoginInfo, error)
	GetStatus(ctx context.Context, connectionID string) (*BotStatus, error)
	SendGroupForward(ctx context.Context, connectionID, groupID string, nodes []message.ForwardNode, opts message.ForwardOptions) error
}

type Messenger interface {
	SendText(ctx context.Context, target message.Target, text string) error
	SendSegments(ctx context.Context, target message.Target, segs []message.Segment) error
	ReplyText(ctx context.Context, target message.Target, replyTo string, text string) error
}

type AIToolContext interface {
	Event() event.Event
	Target() message.Target
	ReplyTo() string
	ScheduleCurrentSend(text string, reply bool) error
}

type AIToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
	Available   func(evt event.Event) bool
	Handle      func(ctx context.Context, toolCtx AIToolContext, args json.RawMessage) (any, error)
}

type AIToolRegistrar interface {
	RegisterTools(namespace string, tools []AIToolDefinition) error
	UnregisterTools(namespace string)
}

type PluginConfigReader interface {
	Unmarshal(target any) error
	Raw() map[string]any
}

type Env struct {
	Logger        *slog.Logger
	Messenger     Messenger
	BotAPI        BotAPI
	AITools       AIToolRegistrar
	Config        PluginConfigReader
	PluginCatalog PluginCatalog
	App           AppInfo
}

func (e Env) OwnerQQ() string {
	return strings.TrimSpace(e.App.OwnerQQ)
}

func (e Env) IsOwner(userID string) bool {
	return e.App.IsOwner(userID)
}
