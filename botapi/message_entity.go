package botapi

import "github.com/XiaoLuozee/go-bot/handler"

type MusicPlatform string

const (
	QQ     MusicPlatform = "qq"
	WangYi MusicPlatform = "163"
	KuGou  MusicPlatform = "kugou"
	MiGu   MusicPlatform = "migu"
	KuWo   MusicPlatform = "kuwo"
	// Custom 自定义
	Custom MusicPlatform = "custom"
)

// Message 消息结构
type Message struct {
	Action  string      `json:"action"`
	Message interface{} `json:"Message"`
	Echo    string      `json:"echo,omitempty"` // echo 字段用于唯一标识请求
}

// GroupMsgParams 发送群消息结构 接口 /send_group_msg
type GroupMsgParams struct {
	GroupId int64                 `json:"group_id"`
	Message []handler.OB11Segment `json:"message"`
}

// ForwardNodeData 定义了一个转发节点的具体数据。
type ForwardNodeData struct {
	// 用于自定义消息节点
	UserID   int64  `json:"user_id,omitempty"`  // 发送者 QQ 号
	Nickname string `json:"nickname,omitempty"` // 发送者昵称
	// 消息内容，可以是字符串（CQ码）或消息段数组
	Content interface{} `json:"content,omitempty"`
	// 用于转发已存在的消息节点
	MessageID *int64 `json:"id,omitempty"` // 要转发的聊天记录的 message_id
}

// ForwardNode 代表合并转发消息中的一个节点（即一条消息）
type ForwardNode struct {
	Type string          `json:"type"` // 这个字段的值必须是 "node"
	Data ForwardNodeData `json:"data"`
}

// GroupForwardMsgParams 对应 /send_group_forward_msg 接口
type GroupForwardMsgParams struct {
	GroupID int64 `json:"group_id"`

	// "messages" 由多个 ForwardNode 组成。
	Messages []ForwardNode `json:"messages"`

	// --- 以下是某些 OneBot 实现支持的可选自定义扩展字段 ---
	Prompt  string `json:"prompt,omitempty"`  // 外显摘要，例如 "[3条聊天记录]"
	Summary string `json:"summary,omitempty"` // 预览窗格底部的摘要
	Source  string `json:"source,omitempty"`  // 预览窗格顶部的来源
}

// PrivateForwardMsgParams 对应 /send_private_forward_msg 接口
type PrivateForwardMsgParams struct {
	// 目标用户的 QQ 号。
	UserID int64 `json:"user_id"`

	Messages []ForwardNode `json:"messages"`
}

// JsonData 发送Json结构
type JsonData struct {
	Data string `json:"data"`
}

// PokeData 发送群聊戳一戳结构
type PokeData struct {
	GroupId int64 `json:"group_id,omitempty""`
	UserId  int64 `json:"user_id"`
}

// PrivateMsgParams 发送私聊消息结构 接口 /send_private_msg
type PrivateMsgParams struct {
	UserId  int64                 `json:"user_id"`
	Message []handler.OB11Segment `json:"message"`
}

// TextData 用于安全地序列化文本内容
type TextData struct {
	Text string `json:"text"`
}

// FileData 用于安全地序列化文件内容
type FileData struct {
	File    string `json:"file"`
	Name    string `json:"name,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// AtData 用于序列化At内容
type AtData struct {
	QQ   any    `json:"qq"`
	Name string `json:"name,omitempty"`
}

type MusicData struct {
	Type  MusicPlatform `json:"type"`
	Id    string        `json:"id,omitempty"`    // 音乐id，自定义音乐时不需要
	Url   string        `json:"url,omitempty"`   // 跳转链接，点击后跳转目标 URL
	Audio string        `json:"audio,omitempty"` // 音频链接，用于自定义音乐
	Title string        `json:"title,omitempty"` // 标题链接，用于自定义音乐
	Image string        `json:"image,omitempty"` // 图片链接，用于自定义音乐
}

type ForwardData struct {
	GroupId   int64 `json:"group_id,omitempty"`
	UserId    int64 `json:"user_id,omitempty"`
	MessageID int64 `json:"message_id"` // 要转发的聊天记录的 message_id
}

type IdData struct {
	Id int64 `json:"id"`
}
