package botapi

import "github.com/XiaoLuozee/go-bot/handler"

// Message 消息结构
type Message struct {
	Action  string      `json:"action"`
	Message interface{} `json:"Message"`
	Echo    string      `json:"echo,omitempty"` // echo 字段用于唯一标识请求
}

// GroupMsg 发送群消息结构 接口 /send_group_msg
type GroupMsg struct {
	GroupId int64                 `json:"group_id"`
	Message []handler.OB11Segment `json:"message"`
}

// GroupForwardMsg 发送群合并转发消息结构 接口 /send_group_forward_msg
type GroupForwardMsg struct {
	GroupMsg
	News    []string `json:"news"`
	Prompt  string   `json:"prompt"`  // 外显
	Summary string   `json:"summary"` // 底下文本
	Source  string   `json:"source"`  // 内容
}

// GroupPoke 发送群聊戳一戳结构 接口 /group_poke
type GroupPoke struct {
	GroupId int64 `json:"group_id"`
	UserId  int64 `json:"user_id"`
}

// PrivateMsg 发送私聊消息结构 接口 /send_private_msg
type PrivateMsg struct {
	UserId  int64                 `json:"user_id"`
	Message []handler.OB11Segment `json:"message"`
}

// TextData 用于安全地序列化文本内容
type TextData struct {
	Text string `json:"text"`
}

// FileData 用于安全地序列化文件内容
type FileData struct {
	File string `json:"file"`
}
