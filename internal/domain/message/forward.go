package message

type ForwardNode struct {
	UserID   string    `json:"user_id"`
	Nickname string    `json:"nickname"`
	Content  []Segment `json:"content"`
}

type ForwardOptions struct {
	Prompt  string `json:"prompt,omitempty"`
	Summary string `json:"summary,omitempty"`
	Source  string `json:"source,omitempty"`
}

func TextNode(userID, nickname, text string) ForwardNode {
	return ForwardNode{
		UserID:   userID,
		Nickname: nickname,
		Content:  []Segment{Text(text)},
	}
}
