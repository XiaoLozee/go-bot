package event

import (
	"encoding/json"
	"time"

	"github.com/XiaoLozee/go-bot/internal/domain/message"
)

type Event struct {
	ID           string            `json:"id"`
	ConnectionID string            `json:"connection_id"`
	Platform     string            `json:"platform"`
	Kind         string            `json:"kind"`
	ChatType     string            `json:"chat_type"`
	UserID       string            `json:"user_id,omitempty"`
	GroupID      string            `json:"group_id,omitempty"`
	MessageID    string            `json:"message_id,omitempty"`
	RawText      string            `json:"raw_text,omitempty"`
	Segments     []message.Segment `json:"segments,omitempty"`
	Timestamp    time.Time         `json:"timestamp"`
	RawPayload   json.RawMessage   `json:"raw_payload,omitempty"`
	Meta         map[string]string `json:"meta,omitempty"`
}
