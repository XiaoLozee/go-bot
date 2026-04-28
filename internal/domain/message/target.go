package message

type Target struct {
	ConnectionID string `json:"connection_id"`
	ChatType     string `json:"chat_type"`
	UserID       string `json:"user_id,omitempty"`
	GroupID      string `json:"group_id,omitempty"`
	ReplyTo      string `json:"reply_to,omitempty"`
}
