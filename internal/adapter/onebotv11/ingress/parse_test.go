package ingress

import "testing"

func TestParseEvent_GroupMessage(t *testing.T) {
	payload := []byte(`{
		"time": 1710000000,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "group",
		"sub_type": "normal",
		"message_id": 10001,
		"group_id": 20001,
		"user_id": 30001,
		"sender": {
			"nickname": "Alice",
			"card": "Alice Card"
		},
		"raw_message": "菜单",
		"message": [
			{"type":"text","data":{"text":"菜单"}}
		]
	}`)

	evt, err := ParseEvent("napcat-main", payload)
	if err != nil {
		t.Fatalf("ParseEvent() error = %v", err)
	}

	if evt.ConnectionID != "napcat-main" {
		t.Fatalf("ConnectionID = %s, want napcat-main", evt.ConnectionID)
	}
	if evt.Kind != "message" {
		t.Fatalf("Kind = %s, want message", evt.Kind)
	}
	if evt.ChatType != "group" {
		t.Fatalf("ChatType = %s, want group", evt.ChatType)
	}
	if evt.GroupID != "20001" {
		t.Fatalf("GroupID = %s, want 20001", evt.GroupID)
	}
	if evt.UserID != "30001" {
		t.Fatalf("UserID = %s, want 30001", evt.UserID)
	}
	if evt.RawText != "菜单" {
		t.Fatalf("RawText = %s, want 菜单", evt.RawText)
	}
	if evt.Meta["sender_card"] != "Alice Card" {
		t.Fatalf("sender_card = %s, want Alice Card", evt.Meta["sender_card"])
	}
	if evt.Meta["sender_nickname"] != "Alice" {
		t.Fatalf("sender_nickname = %s, want Alice", evt.Meta["sender_nickname"])
	}
	if evt.Meta["nickname"] != "Alice Card" {
		t.Fatalf("nickname = %s, want Alice Card", evt.Meta["nickname"])
	}
	if len(evt.Segments) != 1 || evt.Segments[0].Type != "text" {
		t.Fatalf("Segments = %#v, want one text segment", evt.Segments)
	}
}
