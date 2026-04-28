package onebotv11

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
)

func ParseForwardMessage(forwardID string, raw json.RawMessage) (*adapter.ForwardMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, fmt.Errorf("合并转发消息内容为空")
	}

	messagesRaw := raw
	if raw[0] == '{' {
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("解析合并转发响应失败: %w", err)
		}
		for _, key := range []string{"messages", "nodes", "content"} {
			if value := bytes.TrimSpace(envelope[key]); len(value) > 0 && !bytes.Equal(value, []byte("null")) {
				messagesRaw = value
				break
			}
		}
	}

	var items []json.RawMessage
	if err := json.Unmarshal(messagesRaw, &items); err != nil {
		return nil, fmt.Errorf("解析合并转发节点失败: %w", err)
	}

	nodes := make([]adapter.ForwardMessageNode, 0, len(items))
	for _, item := range items {
		node, err := parseForwardNode(item)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return &adapter.ForwardMessage{
		ID:    strings.TrimSpace(forwardID),
		Nodes: nodes,
	}, nil
}

func parseForwardNode(raw json.RawMessage) (adapter.ForwardMessageNode, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return adapter.ForwardMessageNode{}, fmt.Errorf("解析合并转发节点失败: %w", err)
	}

	data := obj
	if nodeType := rawString(obj["type"]); strings.EqualFold(nodeType, "node") {
		if nested := rawObject(obj["data"]); nested != nil {
			data = nested
		}
	}
	sender := rawObject(firstRaw(obj, data, "sender"))

	node := adapter.ForwardMessageNode{
		MessageID: firstString(obj, data, "message_id", "id"),
		UserID:    firstString(data, sender, "user_id", "uin", "qq"),
		Nickname:  firstString(data, sender, "nickname", "name", "card"),
		Content:   parseForwardContent(firstRaw(data, obj, "content", "message")),
	}
	if unix := rawInt64(firstRaw(obj, data, "time")); unix > 0 {
		node.Time = time.Unix(unix, 0)
	}
	return node, nil
}

func parseForwardContent(raw json.RawMessage) []message.Segment {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return []message.Segment{}
	}

	if raw[0] == '"' {
		if text := rawString(raw); text != "" {
			return []message.Segment{message.Text(text)}
		}
		return []message.Segment{}
	}

	if raw[0] == '[' {
		var segments []message.Segment
		if err := json.Unmarshal(raw, &segments); err == nil {
			return segsOrEmpty(segments)
		}
		return []message.Segment{}
	}

	if raw[0] == '{' {
		var segment message.Segment
		if err := json.Unmarshal(raw, &segment); err == nil && strings.TrimSpace(segment.Type) != "" {
			return []message.Segment{segment}
		}
	}

	return []message.Segment{}
}

func segsOrEmpty(items []message.Segment) []message.Segment {
	if items == nil {
		return []message.Segment{}
	}
	return items
}

func firstRaw(primary map[string]json.RawMessage, fallback map[string]json.RawMessage, keys ...string) json.RawMessage {
	for _, key := range keys {
		if value := bytes.TrimSpace(primary[key]); len(value) > 0 && !bytes.Equal(value, []byte("null")) {
			return value
		}
	}
	for _, key := range keys {
		if value := bytes.TrimSpace(fallback[key]); len(value) > 0 && !bytes.Equal(value, []byte("null")) {
			return value
		}
	}
	return nil
}

func firstString(primary map[string]json.RawMessage, fallback map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		if value := rawString(primary[key]); value != "" {
			return value
		}
	}
	for _, key := range keys {
		if value := rawString(fallback[key]); value != "" {
			return value
		}
	}
	return ""
}

func rawObject(raw json.RawMessage) map[string]json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	return obj
}

func rawString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}

	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return strings.TrimSpace(number.String())
	}

	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return ""
	}
	switch item := value.(type) {
	case json.Number:
		return item.String()
	case float64:
		return strconv.FormatInt(int64(item), 10)
	case bool:
		return strconv.FormatBool(item)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", item))
	}
}

func rawInt64(raw json.RawMessage) int64 {
	value := rawString(raw)
	if value == "" {
		return 0
	}
	out, _ := strconv.ParseInt(value, 10, 64)
	return out
}
