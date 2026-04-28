package ingress

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
	"github.com/google/uuid"
)

type baseEvent struct {
	Time        int64           `json:"time"`
	PostType    string          `json:"post_type"`
	MessageType string          `json:"message_type"`
	MessageID   any             `json:"message_id"`
	UserID      any             `json:"user_id"`
	GroupID     any             `json:"group_id"`
	SelfID      any             `json:"self_id"`
	RawMessage  string          `json:"raw_message"`
	Message     json.RawMessage `json:"message"`
	Sender      senderData      `json:"sender"`
}

type senderData struct {
	Nickname string `json:"nickname"`
	Card     string `json:"card"`
}

func ParseEvent(connectionID string, payload []byte) (event.Event, error) {
	var base baseEvent
	if err := json.Unmarshal(payload, &base); err != nil {
		return event.Event{}, fmt.Errorf("解析 OneBot 基础事件失败: %w", err)
	}

	evt := event.Event{
		ID:           uuid.NewString(),
		ConnectionID: connectionID,
		Platform:     "onebot_v11",
		Kind:         base.PostType,
		ChatType:     "system",
		UserID:       normalizeID(base.UserID),
		GroupID:      normalizeID(base.GroupID),
		MessageID:    normalizeID(base.MessageID),
		RawText:      base.RawMessage,
		Timestamp:    time.Unix(base.Time, 0),
		RawPayload:   append(json.RawMessage(nil), payload...),
		Meta: map[string]string{
			"self_id": normalizeID(base.SelfID),
		},
	}
	addMetaIfNotEmpty(evt.Meta, "sender_card", base.Sender.Card)
	addMetaIfNotEmpty(evt.Meta, "sender_nickname", base.Sender.Nickname)
	addMetaIfNotEmpty(evt.Meta, "nickname", firstNonEmpty(base.Sender.Card, base.Sender.Nickname))

	switch base.PostType {
	case "message", "message_sent":
		evt.Kind = "message"
		evt.ChatType = base.MessageType
		evt.Segments = parseSegments(base.Message, base.RawMessage)
	case "notice":
		evt.Kind = "notice"
	case "request":
		evt.Kind = "request"
	case "meta_event":
		evt.Kind = "meta"
	}

	return evt, nil
}

func addMetaIfNotEmpty(meta map[string]string, key, value string) {
	if meta == nil {
		return
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	meta[key] = value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseSegments(raw json.RawMessage, fallback string) []message.Segment {
	if len(raw) == 0 {
		if fallback == "" {
			return nil
		}
		return []message.Segment{message.Text(fallback)}
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return []message.Segment{message.Text(asString)}
	}

	var asArray []struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &asArray); err == nil {
		segs := make([]message.Segment, 0, len(asArray))
		for _, seg := range asArray {
			segs = append(segs, message.Segment{
				Type: seg.Type,
				Data: seg.Data,
			})
		}
		return segs
	}

	var asSingle struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &asSingle); err == nil && asSingle.Type != "" {
		return []message.Segment{{
			Type: asSingle.Type,
			Data: asSingle.Data,
		}}
	}

	if fallback == "" {
		return nil
	}
	return []message.Segment{message.Text(fallback)}
}

func normalizeID(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case float32:
		return strconv.FormatInt(int64(x), 10)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case json.Number:
		return x.String()
	default:
		return fmt.Sprintf("%v", x)
	}
}
