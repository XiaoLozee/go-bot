package media

import "time"

type Asset struct {
	ID              string    `json:"id"`
	MessageID       string    `json:"message_id"`
	ConnectionID    string    `json:"connection_id"`
	ChatType        string    `json:"chat_type"`
	GroupID         string    `json:"group_id,omitempty"`
	UserID          string    `json:"user_id,omitempty"`
	SegmentIndex    int       `json:"segment_index"`
	SegmentType     string    `json:"segment_type"`
	FileName        string    `json:"file_name,omitempty"`
	OriginRef       string    `json:"origin_ref,omitempty"`
	OriginURL       string    `json:"origin_url,omitempty"`
	MimeType        string    `json:"mime_type,omitempty"`
	SizeBytes       int64     `json:"size_bytes"`
	SHA256          string    `json:"sha256,omitempty"`
	StorageBackend  string    `json:"storage_backend"`
	StorageKey      string    `json:"storage_key,omitempty"`
	PublicURL       string    `json:"public_url,omitempty"`
	Status          string    `json:"status"`
	Error           string    `json:"error,omitempty"`
	SegmentDataJSON string    `json:"segment_data_json,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
