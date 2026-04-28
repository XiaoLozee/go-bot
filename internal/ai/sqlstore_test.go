package ai

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
	mysql "github.com/go-sql-driver/mysql"
)

func TestOpenDriverAndDSN_SQLiteAddsLockPragmas(t *testing.T) {
	driver, dsn, err := openDriverAndDSN(config.StorageConfig{
		Engine: "sqlite",
		SQLite: config.SQLiteConfig{
			Path: t.TempDir() + "/app.db",
		},
	}, "sqlite")
	if err != nil {
		t.Fatalf("openDriverAndDSN() error = %v", err)
	}
	if driver != "sqlite" {
		t.Fatalf("driver = %q, want sqlite", driver)
	}
	for _, want := range []string{
		"_pragma=busy_timeout(10000)",
		"_pragma=journal_mode(WAL)",
		"_pragma=foreign_keys(1)",
	} {
		if !strings.Contains(dsn, want) {
			t.Fatalf("dsn = %q, want to contain %q", dsn, want)
		}
	}
}

func TestOpenDriverAndDSN_SQLiteKeepsMemoryDSN(t *testing.T) {
	_, dsn, err := openDriverAndDSN(config.StorageConfig{
		Engine: "sqlite",
		SQLite: config.SQLiteConfig{
			Path: ":memory:",
		},
	}, "sqlite")
	if err != nil {
		t.Fatalf("openDriverAndDSN() error = %v", err)
	}
	if dsn != ":memory:" {
		t.Fatalf("dsn = %q, want :memory:", dsn)
	}
}

func TestOpenDriverAndDSN_MySQLEscapesCredentialsAndPreservesParams(t *testing.T) {
	driver, dsn, err := openDriverAndDSN(config.StorageConfig{
		Engine: "mysql",
		MySQL: config.MySQLConfig{
			Host:     "::1",
			Port:     3306,
			Username: "bot",
			Password: "p@ss:/?#&=word",
			Database: "go_bot",
			Params:   "timeout=5s&parseTime=true&loc=UTC&sql_mode=ANSI_QUOTES",
		},
	}, "mysql")
	if err != nil {
		t.Fatalf("openDriverAndDSN() error = %v", err)
	}
	if driver != "mysql" {
		t.Fatalf("driver = %q, want mysql", driver)
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN(%q) error = %v", dsn, err)
	}
	if parsed.User != "bot" || parsed.Passwd != "p@ss:/?#&=word" || parsed.DBName != "go_bot" {
		t.Fatalf("parsed DSN = user %q password %q db %q, want escaped values preserved", parsed.User, parsed.Passwd, parsed.DBName)
	}
	if parsed.Addr != "[::1]:3306" {
		t.Fatalf("Addr = %q, want [::1]:3306", parsed.Addr)
	}
	if !parsed.ParseTime {
		t.Fatalf("ParseTime = false, want true")
	}
	if parsed.Loc == nil || parsed.Loc.String() != "UTC" {
		t.Fatalf("Loc = %v, want UTC", parsed.Loc)
	}
	if !strings.Contains(dsn, "charset=utf8mb4") {
		t.Fatalf("dsn = %q, want charset=utf8mb4", dsn)
	}
	if got := parsed.Params["sql_mode"]; got != "ANSI_QUOTES" {
		t.Fatalf("sql_mode param = %q, want ANSI_QUOTES", got)
	}
	if parsed.Timeout != 5*time.Second {
		t.Fatalf("Timeout = %s, want 5s", parsed.Timeout)
	}
}

func TestSchemaStatements_MySQLMemoryIdentityUsesContentHash(t *testing.T) {
	statements := strings.Join(schemaStatements("mysql"), "\n")
	for _, want := range []string{
		"content_hash VARCHAR(64) NOT NULL",
		"uk_candidate_memory_identity_hash (scope, memory_type, subtype, subject_id, group_id, content_hash)",
		"uk_long_term_memory_identity_hash (scope, memory_type, subtype, subject_id, group_id, content_hash)",
	} {
		if !strings.Contains(statements, want) {
			t.Fatalf("mysql schema missing %q in:\n%s", want, statements)
		}
	}
	for _, bad := range []string{
		"uk_candidate_memory_identity (scope, memory_type, subtype, subject_id, group_id, content)",
		"uk_long_term_memory_identity (scope, memory_type, subtype, subject_id, group_id, content)",
	} {
		if strings.Contains(statements, bad) {
			t.Fatalf("mysql schema still contains oversized key %q", bad)
		}
	}
}

func TestMemoryUpsert_MySQLIncludesContentHash(t *testing.T) {
	store := &sqlStore{engine: "mysql"}
	candidateQuery, candidateArgs := store.candidateUpsert(CandidateMemory{Content: "same content"}, "[]")
	if !strings.Contains(candidateQuery, "content_hash") || len(candidateArgs) != 15 {
		t.Fatalf("candidate upsert query = %q args = %d, want content_hash and 15 args", candidateQuery, len(candidateArgs))
	}
	if got, want := candidateArgs[7], memoryContentHash("same content"); got != want {
		t.Fatalf("candidate content hash = %v, want %s", got, want)
	}

	longTermQuery, longTermArgs := store.longTermUpsert(LongTermMemory{Content: "same content"}, "[]")
	if !strings.Contains(longTermQuery, "content_hash") || len(longTermArgs) != 14 {
		t.Fatalf("long-term upsert query = %q args = %d, want content_hash and 14 args", longTermQuery, len(longTermArgs))
	}
	if got, want := longTermArgs[7], memoryContentHash("same content"); got != want {
		t.Fatalf("long-term content hash = %v, want %s", got, want)
	}
}

func TestSQLStore_SQLiteRoundTrip(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := openStore(context.Background(), config.StorageConfig{
		Engine: "sqlite",
		SQLite: config.SQLiteConfig{Path: ":memory:"},
		Logs: config.LogsConfig{
			Dir:        "./data/logs",
			MaxSizeMB:  10,
			MaxBackups: 3,
			MaxAgeDays: 7,
		},
	}, logger)
	if err != nil {
		t.Fatalf("openStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Unix(1710000000, 0).UTC()
	if err := store.SaveSession(context.Background(), SessionState{
		Scope:        "group:10001",
		GroupID:      "10001",
		Recent:       []ConversationMessage{{Role: "user", UserID: "20002", Text: "我喜欢东方Project", At: now}},
		TopicSummary: "正在聊东方Project",
		ActiveUsers:  []string{"20002"},
		LastBotAction: &BotAction{
			Mode:     "utility",
			Accepted: true,
			At:       now,
		},
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	candidate := CandidateMemory{
		ID:            "candidate-1",
		Scope:         "user_in_group",
		MemoryType:    "preference",
		Subtype:       "interest",
		SubjectID:     "20002",
		GroupID:       "10001",
		Content:       "用户喜欢 东方Project",
		Confidence:    0.8,
		EvidenceCount: 2,
		SourceMsgIDs:  []string{"msg-1", "msg-2"},
		Status:        "promoted",
		TTLDays:       30,
		CreatedAt:     now,
		LastSeenAt:    now,
	}
	if err := store.UpsertCandidateMemory(context.Background(), candidate); err != nil {
		t.Fatalf("UpsertCandidateMemory() error = %v", err)
	}

	memory := LongTermMemory{
		ID:            "memory-1",
		Scope:         "user_in_group",
		MemoryType:    "semantic",
		Subtype:       "preference",
		SubjectID:     "20002",
		GroupID:       "10001",
		Content:       "用户喜欢 东方Project",
		Confidence:    0.8,
		EvidenceCount: 2,
		SourceRefs:    []string{"msg-1", "msg-2"},
		TTLDays:       180,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.UpsertLongTermMemory(context.Background(), memory); err != nil {
		t.Fatalf("UpsertLongTermMemory() error = %v", err)
	}

	groupProfile := GroupProfile{
		GroupID:           "10001",
		StyleTags:         []string{"轻松玩梗", "高互动"},
		TopicFocus:        []string{"东方Project", "梗图"},
		ActiveMemes:       []string{"太草了"},
		SoftRules:         []string{"文明聊天"},
		HardRules:         []string{"广告"},
		ReflectionSummary: "最近常聊 东方Project、梗图。整体风格偏轻松玩梗 / 高互动。",
		HumorDensity:      0.72,
		EmojiRate:         0.24,
		Formality:         0.18,
		UpdatedAt:         now,
	}
	if err := store.UpsertGroupProfile(context.Background(), groupProfile); err != nil {
		t.Fatalf("UpsertGroupProfile() error = %v", err)
	}

	userProfile := UserInGroupProfile{
		GroupID:                 "10001",
		UserID:                  "20002",
		DisplayName:             "Alice",
		Nicknames:               []string{"Alice"},
		TopicPreferences:        []string{"东方Project"},
		StyleTags:               []string{"短句快聊"},
		TabooTopics:             []string{"广告"},
		InteractionLevelWithBot: 7,
		TeasingTolerance:        0.62,
		TrustScore:              0.78,
		LastActiveAt:            now,
		UpdatedAt:               now,
	}
	if err := store.UpsertUserProfile(context.Background(), userProfile); err != nil {
		t.Fatalf("UpsertUserProfile() error = %v", err)
	}

	relationEdge := RelationEdge{
		ID:                "10001|20002|30003|conversation",
		GroupID:           "10001",
		NodeA:             "20002",
		NodeB:             "30003",
		RelationType:      "conversation",
		Strength:          0.66,
		EvidenceCount:     4,
		LastInteractionAt: now,
	}
	if err := store.UpsertRelationEdge(context.Background(), relationEdge); err != nil {
		t.Fatalf("UpsertRelationEdge() error = %v", err)
	}

	candidates, err := store.LoadCandidateMemories(context.Background())
	if err != nil {
		t.Fatalf("LoadCandidateMemories() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates))
	}
	if candidates[0].EvidenceCount != 2 {
		t.Fatalf("candidate evidence_count = %d, want 2", candidates[0].EvidenceCount)
	}

	sessions, err := store.LoadSessions(context.Background())
	if err != nil {
		t.Fatalf("LoadSessions() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("session count = %d, want 1", len(sessions))
	}
	if sessions[0].TopicSummary != "正在聊东方Project" {
		t.Fatalf("session topic_summary = %q, want round-tripped content", sessions[0].TopicSummary)
	}
	if sessions[0].LastBotAction == nil || sessions[0].LastBotAction.Mode != "utility" {
		t.Fatalf("session last_bot_action = %+v, want utility action", sessions[0].LastBotAction)
	}

	memories, err := store.LoadLongTermMemories(context.Background())
	if err != nil {
		t.Fatalf("LoadLongTermMemories() error = %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("long term memory count = %d, want 1", len(memories))
	}
	if memories[0].Content != "用户喜欢 东方Project" {
		t.Fatalf("long term memory content = %q, want round-tripped content", memories[0].Content)
	}

	groupProfiles, err := store.LoadGroupProfiles(context.Background())
	if err != nil {
		t.Fatalf("LoadGroupProfiles() error = %v", err)
	}
	if len(groupProfiles) != 1 {
		t.Fatalf("group profile count = %d, want 1", len(groupProfiles))
	}
	if len(groupProfiles[0].TopicFocus) != 2 || groupProfiles[0].TopicFocus[0] != "东方Project" {
		t.Fatalf("group profile topic_focus = %+v, want round-tripped topics", groupProfiles[0].TopicFocus)
	}
	if groupProfiles[0].ReflectionSummary != groupProfile.ReflectionSummary {
		t.Fatalf("group profile reflection_summary = %q, want round-tripped summary", groupProfiles[0].ReflectionSummary)
	}

	userProfiles, err := store.LoadUserProfiles(context.Background())
	if err != nil {
		t.Fatalf("LoadUserProfiles() error = %v", err)
	}
	if len(userProfiles) != 1 {
		t.Fatalf("user profile count = %d, want 1", len(userProfiles))
	}
	if userProfiles[0].DisplayName != "Alice" || userProfiles[0].InteractionLevelWithBot != 7 {
		t.Fatalf("user profile = %+v, want round-tripped content", userProfiles[0])
	}

	relationEdges, err := store.LoadRelationEdges(context.Background())
	if err != nil {
		t.Fatalf("LoadRelationEdges() error = %v", err)
	}
	if len(relationEdges) != 1 {
		t.Fatalf("relation edge count = %d, want 1", len(relationEdges))
	}
	if relationEdges[0].RelationType != "conversation" || relationEdges[0].EvidenceCount != 4 {
		t.Fatalf("relation edge = %+v, want round-tripped edge", relationEdges[0])
	}

	if err := store.DeleteCandidateMemory(context.Background(), candidate.ID); err != nil {
		t.Fatalf("DeleteCandidateMemory() error = %v", err)
	}
	if err := store.DeleteLongTermMemory(context.Background(), memory.ID); err != nil {
		t.Fatalf("DeleteLongTermMemory() error = %v", err)
	}
	candidates, err = store.LoadCandidateMemories(context.Background())
	if err != nil {
		t.Fatalf("LoadCandidateMemories() after delete error = %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidate count after delete = %d, want 0", len(candidates))
	}
	memories, err = store.LoadLongTermMemories(context.Background())
	if err != nil {
		t.Fatalf("LoadLongTermMemories() after delete error = %v", err)
	}
	if len(memories) != 0 {
		t.Fatalf("long term memory count after delete = %d, want 0", len(memories))
	}
}

func TestSQLStore_ListMessageLogsSupportsFuzzySearchAndSuggestions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := openStore(context.Background(), config.StorageConfig{
		Engine: "sqlite",
		SQLite: config.SQLiteConfig{Path: ":memory:"},
		Logs: config.LogsConfig{
			Dir:        "./data/logs",
			MaxSizeMB:  10,
			MaxBackups: 3,
			MaxAgeDays: 7,
		},
	}, logger)
	if err != nil {
		t.Fatalf("openStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	now := time.Unix(1710001000, 0).UTC()
	items := []MessageLog{
		{
			MessageID:     "msg-1",
			ConnectionID:  "napcat-main",
			ChatType:      "group",
			GroupID:       "10001",
			UserID:        "20002",
			SenderRole:    "user",
			SenderName:    "Alice",
			TextContent:   "第一条群消息",
			HasText:       true,
			MessageStatus: "normal",
			OccurredAt:    now,
			CreatedAt:     now,
		},
		{
			MessageID:     "msg-2",
			ConnectionID:  "napcat-main",
			ChatType:      "group",
			GroupID:       "10001",
			UserID:        "20002",
			SenderRole:    "user",
			SenderName:    "Alice",
			TextContent:   "第二条群消息",
			HasText:       true,
			MessageStatus: "normal",
			OccurredAt:    now.Add(1 * time.Minute),
			CreatedAt:     now.Add(1 * time.Minute),
		},
		{
			MessageID:     "msg-3",
			ConnectionID:  "napcat-main",
			ChatType:      "group",
			GroupID:       "10088",
			UserID:        "20099",
			SenderRole:    "user",
			SenderName:    "Bob",
			TextContent:   "第三条群消息",
			HasText:       true,
			MessageStatus: "normal",
			OccurredAt:    now.Add(2 * time.Minute),
			CreatedAt:     now.Add(2 * time.Minute),
		},
		{
			MessageID:     "msg-4",
			ConnectionID:  "napcat-main",
			ChatType:      "private",
			UserID:        "30001",
			SenderRole:    "user",
			SenderName:    "Carol",
			TextContent:   "私聊消息",
			HasText:       true,
			MessageStatus: "normal",
			OccurredAt:    now.Add(3 * time.Minute),
			CreatedAt:     now.Add(3 * time.Minute),
		},
	}
	for _, item := range items {
		if err := store.AppendMessageLog(context.Background(), item); err != nil {
			t.Fatalf("AppendMessageLog(%s) error = %v", item.MessageID, err)
		}
	}

	groupMatches, err := store.ListMessageLogs(context.Background(), MessageLogQuery{
		ChatType: "group",
		GroupID:  "001",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListMessageLogs() group fuzzy error = %v", err)
	}
	if len(groupMatches) != 2 {
		t.Fatalf("group fuzzy match len = %d, want 2", len(groupMatches))
	}
	for _, item := range groupMatches {
		if item.GroupID != "10001" {
			t.Fatalf("group fuzzy match group_id = %q, want 10001", item.GroupID)
		}
	}

	userMatches, err := store.ListMessageLogs(context.Background(), MessageLogQuery{
		ChatType: "group",
		UserID:   "099",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListMessageLogs() user fuzzy error = %v", err)
	}
	if len(userMatches) != 1 || userMatches[0].UserID != "20099" {
		t.Fatalf("user fuzzy match = %+v, want one user 20099", userMatches)
	}

	suggestions, err := store.ListMessageSearchSuggestions(context.Background(), MessageSuggestionQuery{
		ChatType: "group",
		GroupID:  "100",
		UserID:   "200",
		Limit:    8,
	})
	if err != nil {
		t.Fatalf("ListMessageSearchSuggestions() error = %v", err)
	}
	if !reflect.DeepEqual(suggestions.Groups, []string{"10088", "10001"}) {
		t.Fatalf("group suggestions = %+v, want distinct recent groups", suggestions.Groups)
	}
	if !reflect.DeepEqual(suggestions.Users, []string{"20099", "20002"}) {
		t.Fatalf("user suggestions = %+v, want distinct recent users", suggestions.Users)
	}

	privateSuggestions, err := store.ListMessageSearchSuggestions(context.Background(), MessageSuggestionQuery{
		ChatType: "private",
		UserID:   "300",
		Limit:    8,
	})
	if err != nil {
		t.Fatalf("ListMessageSearchSuggestions() private error = %v", err)
	}
	if len(privateSuggestions.Groups) != 0 {
		t.Fatalf("private group suggestions = %+v, want empty", privateSuggestions.Groups)
	}
	if !reflect.DeepEqual(privateSuggestions.Users, []string{"30001"}) {
		t.Fatalf("private user suggestions = %+v, want [30001]", privateSuggestions.Users)
	}
}
