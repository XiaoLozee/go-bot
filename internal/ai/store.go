package ai

import (
	"context"
	"time"
)

type Store interface {
	Close() error
	LoadSessions(ctx context.Context) ([]SessionState, error)
	LoadCandidateMemories(ctx context.Context) ([]CandidateMemory, error)
	LoadLongTermMemories(ctx context.Context) ([]LongTermMemory, error)
	LoadGroupProfiles(ctx context.Context) ([]GroupProfile, error)
	LoadUserProfiles(ctx context.Context) ([]UserInGroupProfile, error)
	LoadRelationEdges(ctx context.Context) ([]RelationEdge, error)
	LoadRecentRawMessages(ctx context.Context, limit int) ([]RawMessageLog, error)
	ListMessageLogs(ctx context.Context, query MessageLogQuery) ([]MessageLog, error)
	ListMessageSearchSuggestions(ctx context.Context, query MessageSuggestionQuery) (MessageSearchSuggestions, error)
	GetMessageDetail(ctx context.Context, messageID string) (MessageDetail, error)
	AppendRawMessage(ctx context.Context, item RawMessageLog) error
	AppendMessageLog(ctx context.Context, item MessageLog) error
	AppendMessageImages(ctx context.Context, items []MessageImage) error
	UpsertMessageDisplayHints(ctx context.Context, item MessageLog) error
	SaveSession(ctx context.Context, session SessionState) error
	UpsertCandidateMemory(ctx context.Context, item CandidateMemory) error
	UpsertLongTermMemory(ctx context.Context, item LongTermMemory) error
	UpsertGroupProfile(ctx context.Context, item GroupProfile) error
	UpsertUserProfile(ctx context.Context, item UserInGroupProfile) error
	UpsertRelationEdge(ctx context.Context, item RelationEdge) error
	DeleteCandidateMemory(ctx context.Context, id string) error
	DeleteLongTermMemory(ctx context.Context, id string) error
}

type RawMessageLog struct {
	MessageID        string
	ConnectionID     string
	ChatType         string
	GroupID          string
	UserID           string
	ContentText      string
	NormalizedHash   string
	ReplyToMessageID string
	CreatedAt        time.Time
}

type messageGroupDisplayCache struct {
	ConnectionID string
	GroupID      string
	GroupName    string
	UpdatedAt    time.Time
}

type messageUserDisplayCache struct {
	ConnectionID string
	ChatType     string
	GroupID      string
	UserID       string
	DisplayName  string
	Nickname     string
	UpdatedAt    time.Time
}
