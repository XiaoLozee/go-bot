package handler

import "encoding/json"

// BaseEvent 所有事件共有的基础字段。
type BaseEvent struct {
	Time     int64  `json:"time"`      // 事件发生的时间戳（秒）
	PostType string `json:"post_type"` // 事件类型 (e.g., "message", "notice", "request")
	SelfID   int64  `json:"self_id"`   // 收到事件的机器人 QQ 号
}

// NoticeEventBase 通知事件
type NoticeEventBase struct {
	BaseEvent
	NoticeType string `json:"notice_type"`
}

// -- 具体通知事件的结构体定义 --

// NoticeFile 用于 group_upload 事件中的 file 字段。
type NoticeFile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Busid int64  `json:"busid"`
}

// GroupUploadNotice 群文件上传
type GroupUploadNotice struct {
	NoticeEventBase
	GroupID int64      `json:"group_id"`
	UserID  int64      `json:"user_id"`
	File    NoticeFile `json:"file"`
}

// GroupAdminNotice 群管理员变动
type GroupAdminNotice struct {
	NoticeEventBase
	SubType string `json:"sub_type"` // 'set' or 'unset'
	GroupID int64  `json:"group_id"`
	UserID  int64  `json:"user_id"`
}

// GroupDecreaseNotice 群成员减少
type GroupDecreaseNotice struct {
	NoticeEventBase
	SubType    string `json:"sub_type"` // 'leave', 'kick', 'kick_me'
	GroupID    int64  `json:"group_id"`
	OperatorID int64  `json:"operator_id"`
	UserID     int64  `json:"user_id"`
}

// GroupIncreaseNotice 群成员增加
type GroupIncreaseNotice struct {
	NoticeEventBase
	SubType    string `json:"sub_type"` // 'approve', 'invite'
	GroupID    int64  `json:"group_id"`
	OperatorID int64  `json:"operator_id"`
	UserID     int64  `json:"user_id"`
}

// GroupBanNotice 群禁言
type GroupBanNotice struct {
	NoticeEventBase
	SubType    string `json:"sub_type"` // 'ban', 'lift_ban'
	GroupID    int64  `json:"group_id"`
	OperatorID int64  `json:"operator_id"`
	UserID     int64  `json:"user_id"`
	Duration   int64  `json:"duration"` // 禁言时长，单位秒
}

// FriendAddNotice 新添加好友
type FriendAddNotice struct {
	NoticeEventBase
	UserID int64 `json:"user_id"`
}

// GroupRecallNotice 群消息撤回
type GroupRecallNotice struct {
	NoticeEventBase
	GroupID    int64 `json:"group_id"`
	UserID     int64 `json:"user_id"`
	OperatorID int64 `json:"operator_id"`
	MessageID  int64 `json:"message_id"`
}

// FriendRecallNotice 好友消息撤回
type FriendRecallNotice struct {
	NoticeEventBase
	UserID    int64 `json:"user_id"`
	MessageID int64 `json:"message_id"`
}

// PokeNotice 戳一戳
type PokeNotice struct {
	NoticeEventBase
	SubType  string `json:"sub_type"`           // 通常是 "poke"
	GroupID  *int64 `json:"group_id,omitempty"` // 私聊戳一戳时不存在
	UserID   int64  `json:"user_id"`
	TargetID int64  `json:"target_id"`
}

// GroupCardNotice 群名片变更
type GroupCardNotice struct {
	NoticeEventBase
	GroupID int64  `json:"group_id"`
	UserID  int64  `json:"user_id"`
	CardNew string `json:"card_new"`
	CardOld string `json:"card_old"`
}

// RequestEventBase 请求事件
type RequestEventBase struct {
	BaseEvent
	RequestType string `json:"request_type"`
	Comment     string `json:"comment"`
	Flag        string `json:"flag"`
}

// -- 具体请求事件的结构体定义 --

// FriendRequest 好友请求
type FriendRequest struct {
	RequestEventBase
	UserID int64 `json:"user_id"`
}

// GroupRequest 群请求
type GroupRequest struct {
	RequestEventBase
	SubType string `json:"sub_type"` // 'add' or 'invite'
	GroupID int64  `json:"group_id"`
	UserID  int64  `json:"user_id"`
}

// Message 消息共有字段
type Message struct {
	BaseEvent
	MessageType string        `json:"message_type"` // 消息类型
	SubType     string        `json:"sub_type"`     // 子类型
	MessageId   int64         `json:"message_id"`   // 消息id
	UserID      int64         `json:"user_id"`      // 发送者QQ号
	RawMessage  string        `json:"raw_message"`  // 原始消息内容
	Font        int64         `json:"font"`         // 字体
	Message     []OB11Segment `json:"message"`      // 消息段数组
}

// OB11PrivateMessage 私聊消息
type OB11PrivateMessage struct {
	Message
	FriendSender

	TargetId   *int64 `json:"target_id,omitempty"`   // 临时会话目标QQ号(可选)
	TempSource *int64 `json:"temp_source,omitempty"` // 临时会话来源(可选)
}

// OB11GroupMessage 群消息
type OB11GroupMessage struct {
	Message
	GroupSender
	GroupId int64 `json:"group_id"` // 群号
}

// OB11Segment 消息段结构
type OB11Segment struct {
	Type string          `json:"type"` // 段落类型（如 'text'）
	Data json.RawMessage `json:"data"` // 消息段的具体数据内容
}

// 各类消息段

// Text 纯文本内容
type Text struct {
	Text string `json:"text"` // 文本内容
}

// Image 图片
type Image struct {
	File string  `json:"file"`          // 文件名
	Url  *string `json:"url,omitempty"` // 图片链接
}

// At 艾特某人
type At struct {
	QQ int64 `json:"qq"` // 艾特者QQ
}

// Face QQ表情
type Face struct {
	Id int64 `json:"id"` // 表情id
}

// Reply 回复消息
type Reply struct {
	Id int64 `json:"id"` // 回复消息id
}

// Sender 发送者共有属性
type Sender struct {
	UserId   int64  `json:"user_id"`  // 发送者QQ号
	NikeName string `json:"nikename"` // 发送者昵称
	Sex      string `json:"sex"`      // 性别
}

// FriendSender 私聊发送者
type FriendSender struct {
	Sender
	// 可选字段
	GroupId *int64 `json:"group_id,omitempty"` // 群临时会话群号
}

// GroupSender 群消息发送者
type GroupSender struct {
	Sender
	GroupId int64   `json:"group_id"`       // 群号
	Card    *string `json:"card,omitempty"` // 卡片名称（群名片）
	Role    string  `json:"role"`           // 成员角色 (e.g., "owner", "admin")
}
