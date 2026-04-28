package onebotv11

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
)

func TestParseMessageHistory_UnwrapsMessagesEnvelope(t *testing.T) {
	raw := json.RawMessage(`{
		"messages": [
			{
				"time": 1710000000,
				"message_type": "group",
				"message_id": 10001,
				"group_id": 20001,
				"user_id": 30001,
				"raw_message": "hello",
				"message": [{"type":"text","data":{"text":"hello"}}],
				"sender": {"nickname": "Alice"}
			}
		]
	}`)

	items, err := ParseMessageHistory(raw, adapter.RecentMessagesRequest{ChatType: "group"})
	if err != nil {
		t.Fatalf("ParseMessageHistory() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ParseMessageHistory() len = %d, want 1", len(items))
	}
	item := items[0]
	if item.MessageID != "10001" {
		t.Fatalf("MessageID = %q, want 10001", item.MessageID)
	}
	if item.GroupID != "20001" || item.UserID != "30001" {
		t.Fatalf("IDs = group %q user %q, want 20001/30001", item.GroupID, item.UserID)
	}
	if item.Time != time.Unix(1710000000, 0) {
		t.Fatalf("Time = %v, want unix 1710000000", item.Time)
	}
}

func TestParseMessageHistory_UsesMessageSeqWhenMessageIDMissing(t *testing.T) {
	raw := json.RawMessage(`{
		"messages": [
			{
				"time": 1710000001,
				"message_type": "group",
				"message_seq": 778899,
				"group_id": 20001,
				"user_id": 30001,
				"raw_message": "hello from seq",
				"message": [{"type":"text","data":{"text":"hello from seq"}}]
			}
		]
	}`)

	items, err := ParseMessageHistory(raw, adapter.RecentMessagesRequest{ConnectionID: "napcat-main", ChatType: "group", GroupID: "20001"})
	if err != nil {
		t.Fatalf("ParseMessageHistory() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ParseMessageHistory() len = %d, want 1", len(items))
	}
	if items[0].MessageID != "778899" {
		t.Fatalf("MessageID = %q, want message_seq 778899", items[0].MessageID)
	}
}
