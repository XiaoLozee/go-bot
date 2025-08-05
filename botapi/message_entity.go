package botapi

import "github.com/XiaoLuozee/go-bot/handler"

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

// GroupPoke 发送群聊戳一戳结构 接口 /group_poke
type GroupPoke struct {
	GroupId int64 `json:"group_id"`
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

type AtData struct {
	QQ   any    `json:"qq"`
	Name string `json:"name,omitempty"`
}
