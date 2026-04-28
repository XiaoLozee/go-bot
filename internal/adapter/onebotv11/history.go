package onebotv11

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
)

type historyMessageData struct {
	Time        int64           `json:"time"`
	MessageType string          `json:"message_type"`
	MessageID   any             `json:"message_id"`
	MessageSeq  any             `json:"message_seq"`
	RealID      any             `json:"real_id"`
	ID          any             `json:"id"`
	Message     json.RawMessage `json:"message"`
	RawMessage  string          `json:"raw_message"`
	GroupID     any             `json:"group_id"`
	UserID      any             `json:"user_id"`
	Sender      map[string]any  `json:"sender"`
}

func RecentMessageAction(req adapter.RecentMessagesRequest) (string, map[string]any, error) {
	chatType := strings.ToLower(strings.TrimSpace(req.ChatType))
	count := req.Count
	payload := map[string]any{}
	if count > 0 {
		payload["count"] = count
	}

	switch chatType {
	case "group":
		groupID := strings.TrimSpace(req.GroupID)
		if groupID == "" {
			return "", nil, fmt.Errorf("群号不能为空")
		}
		payload["group_id"] = groupID
		return "get_group_msg_history", payload, nil
	case "private":
		userID := strings.TrimSpace(req.UserID)
		if userID == "" {
			return "", nil, fmt.Errorf("QQ 号不能为空")
		}
		payload["user_id"] = userID
		return "get_friend_msg_history", payload, nil
	default:
		return "", nil, fmt.Errorf("会话类型仅支持 group 或 private")
	}
}

func ParseMessageHistory(raw json.RawMessage, defaults adapter.RecentMessagesRequest) ([]adapter.MessageDetail, error) {
	items, err := unwrapHistoryMessages(bytes.TrimSpace(raw))
	if err != nil {
		return nil, err
	}

	out := make([]adapter.MessageDetail, 0, len(items))
	for _, item := range items {
		detail, ok := parseHistoryMessage(item, defaults)
		if !ok {
			continue
		}
		out = append(out, detail)
	}
	return out, nil
}

func unwrapHistoryMessages(raw json.RawMessage) ([]json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}

	switch raw[0] {
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("解析最近消息列表失败: %w", err)
		}
		return items, nil
	case '{':
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, fmt.Errorf("解析最近消息响应失败: %w", err)
		}
		if _, ok := obj["message_id"]; ok {
			return []json.RawMessage{raw}, nil
		}
		for _, key := range []string{"messages", "message", "items", "data"} {
			value := bytes.TrimSpace(obj[key])
			if len(value) == 0 || bytes.Equal(value, []byte("null")) {
				continue
			}
			return unwrapHistoryMessages(value)
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("最近消息响应格式不支持")
	}
}

func parseHistoryMessage(raw json.RawMessage, defaults adapter.RecentMessagesRequest) (adapter.MessageDetail, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return adapter.MessageDetail{}, false
	}

	var item historyMessageData
	if err := json.Unmarshal(raw, &item); err != nil {
		return adapter.MessageDetail{}, false
	}

	chatType := strings.TrimSpace(item.MessageType)
	if chatType == "" {
		chatType = strings.ToLower(strings.TrimSpace(defaults.ChatType))
	}
	groupID := normalizeHistoryID(item.GroupID)
	if groupID == "" {
		groupID = strings.TrimSpace(defaults.GroupID)
	}
	userID := normalizeHistoryID(item.UserID)
	if userID == "" {
		userID = normalizeHistoryID(item.Sender["user_id"])
	}
	if userID == "" {
		userID = strings.TrimSpace(defaults.UserID)
	}

	messageTime := time.Unix(item.Time, 0)
	if item.Time <= 0 {
		messageTime = time.Now()
	}
	messageID := firstHistoryID(item.MessageID, item.MessageSeq, item.RealID, item.ID)
	if messageID == "" {
		messageID = syntheticHistoryMessageID(defaults, chatType, groupID, userID, messageTime, item.RawMessage, item.Message)
	}

	return adapter.MessageDetail{
		Time:        messageTime,
		MessageType: chatType,
		MessageID:   messageID,
		UserID:      userID,
		GroupID:     groupID,
		RawMessage:  item.RawMessage,
		Message:     append(json.RawMessage(nil), item.Message...),
		Sender:      item.Sender,
	}, true
}

func firstHistoryID(values ...any) string {
	for _, value := range values {
		if id := normalizeHistoryID(value); id != "" {
			return id
		}
	}
	return ""
}

func syntheticHistoryMessageID(defaults adapter.RecentMessagesRequest, chatType, groupID, userID string, at time.Time, rawMessage string, raw json.RawMessage) string {
	scope := strings.TrimSpace(groupID)
	if scope == "" {
		scope = strings.TrimSpace(userID)
	}
	if scope == "" {
		scope = strings.TrimSpace(defaults.GroupID)
	}
	if scope == "" {
		scope = strings.TrimSpace(defaults.UserID)
	}
	sum := sha1.Sum([]byte(strings.Join([]string{
		strings.TrimSpace(defaults.ConnectionID),
		strings.TrimSpace(chatType),
		scope,
		strings.TrimSpace(userID),
		strconv.FormatInt(at.UnixNano(), 10),
		strings.TrimSpace(rawMessage),
		strings.TrimSpace(string(raw)),
	}, "\x00")))
	return "history-" + hex.EncodeToString(sum[:10])
}

func normalizeHistoryID(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return strings.TrimSpace(x.String())
	case float64:
		if math.Trunc(x) == x {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		f := float64(x)
		if math.Trunc(f) == f {
			return strconv.FormatInt(int64(f), 10)
		}
		return strconv.FormatFloat(f, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}
