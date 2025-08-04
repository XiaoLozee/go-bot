package handler

import (
	"encoding/json"
	"fmt"
)

// ParseEvent 消息解析器
func ParseEvent(data []byte) (interface{}, error) {
	// 预解析
	var baseEvent BaseEvent
	if err := json.Unmarshal(data, &baseEvent); err != nil {
		// 如果连基础字段都解析失败，说明数据格式有问题，直接返回错误。
		return nil, fmt.Errorf("解析基础事件失败: %w", err)
	}

	// 根据 "post_type" 的值，将解析任务分发给解析函数
	switch baseEvent.PostType {
	case "message", "message_sent":
		// 消息事件
		return parseMessageEvent(data)
	case "notice":
		// 通知事件
		return parseNoticeEvent(data)
	case "request":
		// 请求事件
		return parseRequestEvent(data)
	default:
		return nil, fmt.Errorf("未知的 post_type: %s", baseEvent.PostType)
	}
}

// parseMessageEvent 解析消息事件
func parseMessageEvent(data []byte) (interface{}, error) {
	// 预解析，获取 "message_type"
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("解析基础消息事件失败: %w", err)
	}

	// 根据 "message_type" 解析到最终的结构体
	switch msg.MessageType {
	case "private":
		var privateMsg OB11PrivateMessage
		if err := json.Unmarshal(data, &privateMsg); err != nil {
			return nil, fmt.Errorf("解析私聊消息失败: %w", err)
		}
		return privateMsg, nil
	case "group":
		var groupMsg OB11GroupMessage
		if err := json.Unmarshal(data, &groupMsg); err != nil {
			return nil, fmt.Errorf("解析群聊消息失败: %w", err)
		}
		return groupMsg, nil
	default:
		return nil, fmt.Errorf("未知的消息类型: %s", msg.MessageType)
	}
}

// parseNoticeEvent 解析通知事件
func parseNoticeEvent(data []byte) (interface{}, error) {
	var base struct {
		NoticeType string `json:"notice_type"`
		SubType    string `json:"sub_type"`
	}
	if err := json.Unmarshal(data, &base); err != nil {
		return nil, fmt.Errorf("解析基础通知事件失败: %w", err)
	}

	// 根据 "notice_type" 解析到最终的结构体
	switch base.NoticeType {
	case "group_upload":
		var notice GroupUploadNotice
		if err := json.Unmarshal(data, &notice); err != nil {
			return nil, err
		}
		return notice, nil
	case "group_admin":
		var notice GroupAdminNotice
		if err := json.Unmarshal(data, &notice); err != nil {
			return nil, err
		}
		return notice, nil
	case "group_decrease":
		var notice GroupDecreaseNotice
		if err := json.Unmarshal(data, &notice); err != nil {
			return nil, err
		}
		return notice, nil
	case "group_increase":
		var notice GroupIncreaseNotice
		if err := json.Unmarshal(data, &notice); err != nil {
			return nil, err
		}
		return notice, nil
	case "group_ban":
		var notice GroupBanNotice
		if err := json.Unmarshal(data, &notice); err != nil {
			return nil, err
		}
		return notice, nil
	case "friend_add":
		var notice FriendAddNotice
		if err := json.Unmarshal(data, &notice); err != nil {
			return nil, err
		}
		return notice, nil
	case "group_recall":
		var notice GroupRecallNotice
		if err := json.Unmarshal(data, &notice); err != nil {
			return nil, err
		}
		return notice, nil
	case "friend_recall":
		var notice FriendRecallNotice
		if err := json.Unmarshal(data, &notice); err != nil {
			return nil, err
		}
		return notice, nil
	case "poke": // 直接处理 notice_type: "poke" 的情况
		var notice PokeNotice
		if err := json.Unmarshal(data, &notice); err != nil {
			return nil, err
		}
		return notice, nil
	case "notify": // 新增的 case，处理 "notify" 这种复合类型
		// 进一步根据 sub_type 判断
		switch base.SubType {
		case "poke":
			var notice PokeNotice
			if err := json.Unmarshal(data, &notice); err != nil {
				return nil, err
			}
			return notice, nil
		default:
			return nil, fmt.Errorf("未知的 notify 子类型(sub_type): %s", base.SubType)
		}
	default:
		return nil, fmt.Errorf("未知的通知类型: %s", base.NoticeType)
	}
}

// parseRequestEvent 解析请求事件
func parseRequestEvent(data []byte) (interface{}, error) {
	// 预解析，获取 "request_type"
	var requestBase RequestEventBase
	if err := json.Unmarshal(data, &requestBase); err != nil {
		return nil, fmt.Errorf("解析基础请求事件失败: %w", err)
	}

	// 根据 "request_type" 解析到最终的结构体
	switch requestBase.RequestType {
	case "friend":
		var request FriendRequest
		if err := json.Unmarshal(data, &request); err != nil {
			return nil, fmt.Errorf("解析好友请求失败: %w", err)
		}
		return request, nil
	case "group":
		var request GroupRequest
		if err := json.Unmarshal(data, &request); err != nil {
			return nil, fmt.Errorf("解析群组请求失败: %w", err)
		}
		return request, nil
	default:
		return nil, fmt.Errorf("未知的请求类型: %s", requestBase.RequestType)
	}
}
