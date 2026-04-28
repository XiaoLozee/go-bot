package ai

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"

	mysql "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type sqlStore struct {
	engine string
	db     *sql.DB
	logger *slog.Logger
}

const (
	sqliteBusyTimeoutMS       = 10000
	defaultSQLMaxOpenConns    = 16
	defaultSQLMaxIdleConns    = 8
	defaultSQLConnMaxLifetime = 30 * time.Minute
	defaultSQLConnMaxIdleTime = 5 * time.Minute
)

func openStore(ctx context.Context, cfg config.StorageConfig, logger *slog.Logger) (Store, error) {
	engine := normalizeLocalStorageEngine(cfg.Engine)
	driver, dsn, err := openDriverAndDSN(cfg, engine)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库连接失败: %w", err)
	}
	configureSQLDB(db, engine)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	store := &sqlStore{engine: engine, db: db, logger: logger}
	if err := store.initSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func openDriverAndDSN(cfg config.StorageConfig, engine string) (string, string, error) {
	switch engine {
	case "sqlite":
		path := strings.TrimSpace(cfg.SQLite.Path)
		if path == "" {
			return "", "", fmt.Errorf("storage.sqlite.path 不能为空")
		}
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", "", fmt.Errorf("创建 SQLite 数据目录失败: %w", err)
			}
		}
		return "sqlite", sqliteDSN(path), nil
	case "mysql":
		dsn, err := mysqlDSN(cfg.MySQL)
		if err != nil {
			return "", "", err
		}
		return "mysql", dsn, nil
	case "postgresql":
		cfg := cfg.PostgreSQL
		sslMode := strings.TrimSpace(cfg.SSLMode)
		if sslMode == "" {
			sslMode = "disable"
		}
		schema := strings.TrimSpace(cfg.Schema)
		if schema == "" {
			schema = "public"
		}
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s search_path=%s", cfg.Host, cfg.Port, cfg.Username, cfg.Password, cfg.Database, sslMode, schema)
		return "pgx", dsn, nil
	default:
		return "", "", fmt.Errorf("未知存储引擎: %s", engine)
	}
}

func configureSQLDB(db *sql.DB, engine string) {
	if db == nil {
		return
	}
	if engine == "sqlite" {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		return
	}
	db.SetMaxOpenConns(defaultSQLMaxOpenConns)
	db.SetMaxIdleConns(defaultSQLMaxIdleConns)
	db.SetConnMaxLifetime(defaultSQLConnMaxLifetime)
	db.SetConnMaxIdleTime(defaultSQLConnMaxIdleTime)
}

func mysqlDSN(cfg config.MySQLConfig) (string, error) {
	params, err := mysqlParams(cfg.Params)
	if err != nil {
		return "", err
	}
	mysqlCfg := mysql.NewConfig()
	mysqlCfg.User = strings.TrimSpace(cfg.Username)
	mysqlCfg.Passwd = cfg.Password
	mysqlCfg.Net = "tcp"
	mysqlCfg.Addr = net.JoinHostPort(strings.TrimSpace(cfg.Host), fmt.Sprintf("%d", cfg.Port))
	mysqlCfg.DBName = strings.TrimSpace(cfg.Database)
	mysqlCfg.ParseTime = true
	mysqlCfg.Params = params
	if loc := strings.TrimSpace(params["loc"]); loc != "" {
		loaded, err := time.LoadLocation(loc)
		if err != nil {
			return "", fmt.Errorf("storage.mysql.params loc 无效: %w", err)
		}
		mysqlCfg.Loc = loaded
		delete(mysqlCfg.Params, "loc")
	}
	if value := strings.TrimSpace(params["parseTime"]); value != "" {
		mysqlCfg.ParseTime = strings.EqualFold(value, "true") || value == "1"
		delete(mysqlCfg.Params, "parseTime")
	}
	if _, ok := mysqlCfg.Params["charset"]; !ok {
		mysqlCfg.Params["charset"] = "utf8mb4"
	}
	return mysqlCfg.FormatDSN(), nil
}

func mysqlParams(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "charset=utf8mb4&parseTime=true&loc=Local"
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return nil, fmt.Errorf("storage.mysql.params 格式错误: %w", err)
	}
	params := make(map[string]string, len(values)+2)
	for key, items := range values {
		key = strings.TrimSpace(key)
		if key == "" || len(items) == 0 {
			continue
		}
		switch strings.ToLower(key) {
		case "parsetime":
			key = "parseTime"
		case "charset":
			key = "charset"
		case "loc":
			key = "loc"
		}
		params[key] = items[len(items)-1]
	}
	if _, ok := params["parseTime"]; !ok {
		params["parseTime"] = "true"
	}
	if _, ok := params["charset"]; !ok {
		params["charset"] = "utf8mb4"
	}
	return params, nil
}

func sqliteDSN(path string) string {
	if path == ":memory:" {
		return path
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return fmt.Sprintf("%s%s_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path, separator, sqliteBusyTimeoutMS)
}

func (s *sqlStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *sqlStore) LoadSessions(ctx context.Context) ([]SessionState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT scope, group_id, recent_window_json, topic_summary, active_users_json, last_bot_action_json, updated_at FROM session_state ORDER BY updated_at DESC LIMIT 200`)
	if err != nil {
		return nil, fmt.Errorf("查询会话状态失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]SessionState, 0, 32)
	for rows.Next() {
		var item SessionState
		var recentJSON string
		var activeUsersJSON string
		var lastBotActionJSON string
		var updatedAt string
		if err := rows.Scan(&item.Scope, &item.GroupID, &recentJSON, &item.TopicSummary, &activeUsersJSON, &lastBotActionJSON, &updatedAt); err != nil {
			return nil, fmt.Errorf("读取会话状态失败: %w", err)
		}
		if err := decodeJSONInto(recentJSON, &item.Recent); err != nil {
			return nil, fmt.Errorf("解析会话 recent_window_json 失败: %w", err)
		}
		if err := decodeJSONInto(activeUsersJSON, &item.ActiveUsers); err != nil {
			return nil, fmt.Errorf("解析会话 active_users_json 失败: %w", err)
		}
		if err := decodeJSONInto(lastBotActionJSON, &item.LastBotAction); err != nil {
			return nil, fmt.Errorf("解析会话 last_bot_action_json 失败: %w", err)
		}
		item.UpdatedAt = parseStoredTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) LoadCandidateMemories(ctx context.Context) ([]CandidateMemory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, scope, memory_type, subtype, subject_id, group_id, content, confidence, evidence_count, source_msg_ids_json, status, ttl_days, created_at, last_seen_at FROM candidate_memory ORDER BY last_seen_at DESC LIMIT 1000`)
	if err != nil {
		return nil, fmt.Errorf("查询候选记忆失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]CandidateMemory, 0, 64)
	for rows.Next() {
		var item CandidateMemory
		var sourceJSON string
		var createdAt string
		var lastSeenAt string
		if err := rows.Scan(&item.ID, &item.Scope, &item.MemoryType, &item.Subtype, &item.SubjectID, &item.GroupID, &item.Content, &item.Confidence, &item.EvidenceCount, &sourceJSON, &item.Status, &item.TTLDays, &createdAt, &lastSeenAt); err != nil {
			return nil, fmt.Errorf("读取候选记忆失败: %w", err)
		}
		item.SourceMsgIDs = decodeStringList(sourceJSON)
		item.CreatedAt = parseStoredTime(createdAt)
		item.LastSeenAt = parseStoredTime(lastSeenAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) LoadLongTermMemories(ctx context.Context) ([]LongTermMemory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, scope, memory_type, subtype, subject_id, group_id, content, confidence, evidence_count, source_refs_json, ttl_days, created_at, updated_at FROM long_term_memory ORDER BY updated_at DESC LIMIT 1000`)
	if err != nil {
		return nil, fmt.Errorf("查询长期记忆失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]LongTermMemory, 0, 64)
	for rows.Next() {
		var item LongTermMemory
		var sourceJSON string
		var createdAt string
		var updatedAt string
		if err := rows.Scan(&item.ID, &item.Scope, &item.MemoryType, &item.Subtype, &item.SubjectID, &item.GroupID, &item.Content, &item.Confidence, &item.EvidenceCount, &sourceJSON, &item.TTLDays, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("读取长期记忆失败: %w", err)
		}
		item.SourceRefs = decodeStringList(sourceJSON)
		item.CreatedAt = parseStoredTime(createdAt)
		item.UpdatedAt = parseStoredTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) LoadGroupProfiles(ctx context.Context) ([]GroupProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT group_id, style_tags_json, topic_focus_json, active_memes_json, soft_rules_json, hard_rules_json, reflection_summary, humor_density, emoji_rate, formality, updated_at FROM group_profile ORDER BY updated_at DESC LIMIT 512`)
	if err != nil {
		return nil, fmt.Errorf("查询群画像失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]GroupProfile, 0, 32)
	for rows.Next() {
		var item GroupProfile
		var styleJSON string
		var topicJSON string
		var memeJSON string
		var softJSON string
		var hardJSON string
		var reflectionSummary sql.NullString
		var updatedAt string
		if err := rows.Scan(&item.GroupID, &styleJSON, &topicJSON, &memeJSON, &softJSON, &hardJSON, &reflectionSummary, &item.HumorDensity, &item.EmojiRate, &item.Formality, &updatedAt); err != nil {
			return nil, fmt.Errorf("读取群画像失败: %w", err)
		}
		item.StyleTags = decodeStringList(styleJSON)
		item.TopicFocus = decodeStringList(topicJSON)
		item.ActiveMemes = decodeStringList(memeJSON)
		item.SoftRules = decodeStringList(softJSON)
		item.HardRules = decodeStringList(hardJSON)
		item.ReflectionSummary = strings.TrimSpace(reflectionSummary.String)
		item.UpdatedAt = parseStoredTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) LoadUserProfiles(ctx context.Context) ([]UserInGroupProfile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT group_id, user_id, display_name, nicknames_json, topic_preferences_json, style_tags_json, taboo_topics_json, interaction_level_with_bot, teasing_tolerance, trust_score, last_active_at, updated_at FROM user_in_group_profile ORDER BY updated_at DESC LIMIT 1024`)
	if err != nil {
		return nil, fmt.Errorf("查询成员画像失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]UserInGroupProfile, 0, 64)
	for rows.Next() {
		var item UserInGroupProfile
		var nicknamesJSON string
		var preferencesJSON string
		var styleJSON string
		var tabooJSON string
		var lastActiveAt string
		var updatedAt string
		if err := rows.Scan(&item.GroupID, &item.UserID, &item.DisplayName, &nicknamesJSON, &preferencesJSON, &styleJSON, &tabooJSON, &item.InteractionLevelWithBot, &item.TeasingTolerance, &item.TrustScore, &lastActiveAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("读取成员画像失败: %w", err)
		}
		item.Nicknames = decodeStringList(nicknamesJSON)
		item.TopicPreferences = decodeStringList(preferencesJSON)
		item.StyleTags = decodeStringList(styleJSON)
		item.TabooTopics = decodeStringList(tabooJSON)
		item.LastActiveAt = parseStoredTime(lastActiveAt)
		item.UpdatedAt = parseStoredTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) LoadRelationEdges(ctx context.Context) ([]RelationEdge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, group_id, node_a, node_b, relation_type, strength, evidence_count, last_interaction_at FROM relation_edge ORDER BY last_interaction_at DESC LIMIT 2048`)
	if err != nil {
		return nil, fmt.Errorf("查询关系图谱失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]RelationEdge, 0, 64)
	for rows.Next() {
		var item RelationEdge
		var lastInteractionAt string
		if err := rows.Scan(&item.ID, &item.GroupID, &item.NodeA, &item.NodeB, &item.RelationType, &item.Strength, &item.EvidenceCount, &lastInteractionAt); err != nil {
			return nil, fmt.Errorf("读取关系边失败: %w", err)
		}
		item.LastInteractionAt = parseStoredTime(lastInteractionAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) LoadRecentRawMessages(ctx context.Context, limit int) ([]RawMessageLog, error) {
	limit = maxInt(1, minInt(limit, 2048))
	query := fmt.Sprintf(`SELECT msg_id, connection_id, chat_type, group_id, user_id, content_text, normalized_hash, reply_to_msg_id, created_at FROM raw_message_log WHERE chat_type = 'group' AND group_id <> '' AND user_id <> '' AND reply_to_msg_id = '' ORDER BY created_at DESC LIMIT %d`, limit)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("查询原始群消息失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]RawMessageLog, 0, minInt(limit, 128))
	for rows.Next() {
		var item RawMessageLog
		var createdAt string
		if err := rows.Scan(&item.MessageID, &item.ConnectionID, &item.ChatType, &item.GroupID, &item.UserID, &item.ContentText, &item.NormalizedHash, &item.ReplyToMessageID, &createdAt); err != nil {
			return nil, fmt.Errorf("读取原始群消息失败: %w", err)
		}
		item.CreatedAt = parseStoredTime(createdAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) AppendRawMessage(ctx context.Context, item RawMessageLog) error {
	query, args := s.rawMessageUpsert(item)
	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("写入原始消息日志失败: %w", err)
	}
	return nil
}

func (s *sqlStore) SaveSession(ctx context.Context, session SessionState) error {
	recentJSON, err := encodeJSON(session.Recent)
	if err != nil {
		return err
	}
	activeUsersJSON, err := encodeJSON(session.ActiveUsers)
	if err != nil {
		return err
	}
	lastBotActionJSON, err := encodeJSON(session.LastBotAction)
	if err != nil {
		return err
	}
	query, args := s.sessionUpsert(session, recentJSON, activeUsersJSON, lastBotActionJSON)
	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("保存会话状态失败: %w", err)
	}
	return nil
}

func (s *sqlStore) UpsertCandidateMemory(ctx context.Context, item CandidateMemory) error {
	sourceJSON, err := encodeJSON(item.SourceMsgIDs)
	if err != nil {
		return err
	}
	query, args := s.candidateUpsert(item, sourceJSON)
	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("写入候选记忆失败: %w", err)
	}
	return nil
}

func (s *sqlStore) UpsertLongTermMemory(ctx context.Context, item LongTermMemory) error {
	sourceJSON, err := encodeJSON(item.SourceRefs)
	if err != nil {
		return err
	}
	query, args := s.longTermUpsert(item, sourceJSON)
	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("写入长期记忆失败: %w", err)
	}
	return nil
}

func (s *sqlStore) UpsertGroupProfile(ctx context.Context, item GroupProfile) error {
	styleJSON, err := encodeJSON(item.StyleTags)
	if err != nil {
		return err
	}
	topicJSON, err := encodeJSON(item.TopicFocus)
	if err != nil {
		return err
	}
	memeJSON, err := encodeJSON(item.ActiveMemes)
	if err != nil {
		return err
	}
	softJSON, err := encodeJSON(item.SoftRules)
	if err != nil {
		return err
	}
	hardJSON, err := encodeJSON(item.HardRules)
	if err != nil {
		return err
	}
	query, args := s.groupProfileUpsert(item, styleJSON, topicJSON, memeJSON, softJSON, hardJSON)
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("写入群画像失败: %w", err)
	}
	return nil
}

func (s *sqlStore) UpsertUserProfile(ctx context.Context, item UserInGroupProfile) error {
	nicknamesJSON, err := encodeJSON(item.Nicknames)
	if err != nil {
		return err
	}
	preferencesJSON, err := encodeJSON(item.TopicPreferences)
	if err != nil {
		return err
	}
	styleJSON, err := encodeJSON(item.StyleTags)
	if err != nil {
		return err
	}
	tabooJSON, err := encodeJSON(item.TabooTopics)
	if err != nil {
		return err
	}
	query, args := s.userProfileUpsert(item, nicknamesJSON, preferencesJSON, styleJSON, tabooJSON)
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("写入成员画像失败: %w", err)
	}
	return nil
}

func (s *sqlStore) UpsertRelationEdge(ctx context.Context, item RelationEdge) error {
	query, args := s.relationEdgeUpsert(item)
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("写入关系边失败: %w", err)
	}
	return nil
}

func (s *sqlStore) DeleteCandidateMemory(ctx context.Context, id string) error {
	query, args := s.deleteByIDQuery("candidate_memory", id)
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("删除候选记忆失败: %w", err)
	}
	return nil
}

func (s *sqlStore) DeleteLongTermMemory(ctx context.Context, id string) error {
	query, args := s.deleteByIDQuery("long_term_memory", id)
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("删除长期记忆失败: %w", err)
	}
	return nil
}

func (s *sqlStore) initSchema(ctx context.Context) error {
	for _, stmt := range schemaStatements(s.engine) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("初始化 %s 存储表失败: %w", s.engine, err)
		}
	}
	for _, stmt := range migrationStatements(s.engine) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, stmt); err != nil && !isIgnorableMigrationError(err) {
			return fmt.Errorf("执行 %s 存储迁移失败: %w", s.engine, err)
		}
	}
	return nil
}

func (s *sqlStore) rawMessageUpsert(item RawMessageLog) (string, []any) {
	createdAt := formatStoredTime(item.CreatedAt)
	args := []any{item.MessageID, item.ConnectionID, item.ChatType, item.GroupID, item.UserID, item.ContentText, item.NormalizedHash, item.ReplyToMessageID, createdAt}
	switch s.engine {
	case "postgresql":
		return `INSERT INTO raw_message_log (msg_id, connection_id, chat_type, group_id, user_id, content_text, normalized_hash, reply_to_msg_id, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT (msg_id) DO UPDATE SET connection_id=EXCLUDED.connection_id, chat_type=EXCLUDED.chat_type, group_id=EXCLUDED.group_id, user_id=EXCLUDED.user_id, content_text=EXCLUDED.content_text, normalized_hash=EXCLUDED.normalized_hash, reply_to_msg_id=EXCLUDED.reply_to_msg_id, created_at=EXCLUDED.created_at`, args
	case "mysql":
		return `INSERT INTO raw_message_log (msg_id, connection_id, chat_type, group_id, user_id, content_text, normalized_hash, reply_to_msg_id, created_at) VALUES (?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE connection_id=VALUES(connection_id), chat_type=VALUES(chat_type), group_id=VALUES(group_id), user_id=VALUES(user_id), content_text=VALUES(content_text), normalized_hash=VALUES(normalized_hash), reply_to_msg_id=VALUES(reply_to_msg_id), created_at=VALUES(created_at)`, args
	default:
		return `INSERT INTO raw_message_log (msg_id, connection_id, chat_type, group_id, user_id, content_text, normalized_hash, reply_to_msg_id, created_at) VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(msg_id) DO UPDATE SET connection_id=excluded.connection_id, chat_type=excluded.chat_type, group_id=excluded.group_id, user_id=excluded.user_id, content_text=excluded.content_text, normalized_hash=excluded.normalized_hash, reply_to_msg_id=excluded.reply_to_msg_id, created_at=excluded.created_at`, args
	}
}

func (s *sqlStore) sessionUpsert(session SessionState, recentJSON, activeUsersJSON, lastBotActionJSON string) (string, []any) {
	updatedAt := formatStoredTime(session.UpdatedAt)
	args := []any{session.Scope, session.GroupID, recentJSON, session.TopicSummary, activeUsersJSON, lastBotActionJSON, updatedAt}
	switch s.engine {
	case "postgresql":
		return `INSERT INTO session_state (scope, group_id, recent_window_json, topic_summary, active_users_json, last_bot_action_json, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (scope) DO UPDATE SET group_id=EXCLUDED.group_id, recent_window_json=EXCLUDED.recent_window_json, topic_summary=EXCLUDED.topic_summary, active_users_json=EXCLUDED.active_users_json, last_bot_action_json=EXCLUDED.last_bot_action_json, updated_at=EXCLUDED.updated_at`, args
	case "mysql":
		return `INSERT INTO session_state (scope, group_id, recent_window_json, topic_summary, active_users_json, last_bot_action_json, updated_at) VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE group_id=VALUES(group_id), recent_window_json=VALUES(recent_window_json), topic_summary=VALUES(topic_summary), active_users_json=VALUES(active_users_json), last_bot_action_json=VALUES(last_bot_action_json), updated_at=VALUES(updated_at)`, args
	default:
		return `INSERT INTO session_state (scope, group_id, recent_window_json, topic_summary, active_users_json, last_bot_action_json, updated_at) VALUES (?,?,?,?,?,?,?) ON CONFLICT(scope) DO UPDATE SET group_id=excluded.group_id, recent_window_json=excluded.recent_window_json, topic_summary=excluded.topic_summary, active_users_json=excluded.active_users_json, last_bot_action_json=excluded.last_bot_action_json, updated_at=excluded.updated_at`, args
	}
}

func (s *sqlStore) candidateUpsert(item CandidateMemory, sourceJSON string) (string, []any) {
	createdAt := formatStoredTime(item.CreatedAt)
	lastSeenAt := formatStoredTime(item.LastSeenAt)
	args := []any{item.ID, item.Scope, item.MemoryType, item.Subtype, item.SubjectID, item.GroupID, item.Content, item.Confidence, item.EvidenceCount, sourceJSON, item.Status, item.TTLDays, createdAt, lastSeenAt}
	switch s.engine {
	case "postgresql":
		return `INSERT INTO candidate_memory (id, scope, memory_type, subtype, subject_id, group_id, content, confidence, evidence_count, source_msg_ids_json, status, ttl_days, created_at, last_seen_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT (scope, memory_type, subtype, subject_id, group_id, content) DO UPDATE SET confidence=EXCLUDED.confidence, evidence_count=EXCLUDED.evidence_count, source_msg_ids_json=EXCLUDED.source_msg_ids_json, status=EXCLUDED.status, ttl_days=EXCLUDED.ttl_days, last_seen_at=EXCLUDED.last_seen_at`, args
	case "mysql":
		mysqlArgs := []any{item.ID, item.Scope, item.MemoryType, item.Subtype, item.SubjectID, item.GroupID, item.Content, memoryContentHash(item.Content), item.Confidence, item.EvidenceCount, sourceJSON, item.Status, item.TTLDays, createdAt, lastSeenAt}
		return `INSERT INTO candidate_memory (id, scope, memory_type, subtype, subject_id, group_id, content, content_hash, confidence, evidence_count, source_msg_ids_json, status, ttl_days, created_at, last_seen_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE content=VALUES(content), content_hash=VALUES(content_hash), confidence=VALUES(confidence), evidence_count=VALUES(evidence_count), source_msg_ids_json=VALUES(source_msg_ids_json), status=VALUES(status), ttl_days=VALUES(ttl_days), last_seen_at=VALUES(last_seen_at)`, mysqlArgs
	default:
		return `INSERT INTO candidate_memory (id, scope, memory_type, subtype, subject_id, group_id, content, confidence, evidence_count, source_msg_ids_json, status, ttl_days, created_at, last_seen_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(scope, memory_type, subtype, subject_id, group_id, content) DO UPDATE SET confidence=excluded.confidence, evidence_count=excluded.evidence_count, source_msg_ids_json=excluded.source_msg_ids_json, status=excluded.status, ttl_days=excluded.ttl_days, last_seen_at=excluded.last_seen_at`, args
	}
}

func (s *sqlStore) longTermUpsert(item LongTermMemory, sourceJSON string) (string, []any) {
	createdAt := formatStoredTime(item.CreatedAt)
	updatedAt := formatStoredTime(item.UpdatedAt)
	args := []any{item.ID, item.Scope, item.MemoryType, item.Subtype, item.SubjectID, item.GroupID, item.Content, item.Confidence, item.EvidenceCount, sourceJSON, item.TTLDays, createdAt, updatedAt}
	switch s.engine {
	case "postgresql":
		return `INSERT INTO long_term_memory (id, scope, memory_type, subtype, subject_id, group_id, content, confidence, evidence_count, source_refs_json, ttl_days, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT (scope, memory_type, subtype, subject_id, group_id, content) DO UPDATE SET confidence=EXCLUDED.confidence, evidence_count=EXCLUDED.evidence_count, source_refs_json=EXCLUDED.source_refs_json, ttl_days=EXCLUDED.ttl_days, updated_at=EXCLUDED.updated_at`, args
	case "mysql":
		mysqlArgs := []any{item.ID, item.Scope, item.MemoryType, item.Subtype, item.SubjectID, item.GroupID, item.Content, memoryContentHash(item.Content), item.Confidence, item.EvidenceCount, sourceJSON, item.TTLDays, createdAt, updatedAt}
		return `INSERT INTO long_term_memory (id, scope, memory_type, subtype, subject_id, group_id, content, content_hash, confidence, evidence_count, source_refs_json, ttl_days, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE content=VALUES(content), content_hash=VALUES(content_hash), confidence=VALUES(confidence), evidence_count=VALUES(evidence_count), source_refs_json=VALUES(source_refs_json), ttl_days=VALUES(ttl_days), updated_at=VALUES(updated_at)`, mysqlArgs
	default:
		return `INSERT INTO long_term_memory (id, scope, memory_type, subtype, subject_id, group_id, content, confidence, evidence_count, source_refs_json, ttl_days, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(scope, memory_type, subtype, subject_id, group_id, content) DO UPDATE SET confidence=excluded.confidence, evidence_count=excluded.evidence_count, source_refs_json=excluded.source_refs_json, ttl_days=excluded.ttl_days, updated_at=excluded.updated_at`, args
	}
}

func memoryContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func (s *sqlStore) groupProfileUpsert(item GroupProfile, styleJSON, topicJSON, memeJSON, softJSON, hardJSON string) (string, []any) {
	updatedAt := formatStoredTime(item.UpdatedAt)
	args := []any{item.GroupID, styleJSON, topicJSON, memeJSON, softJSON, hardJSON, item.ReflectionSummary, item.HumorDensity, item.EmojiRate, item.Formality, updatedAt}
	switch s.engine {
	case "postgresql":
		return `INSERT INTO group_profile (group_id, style_tags_json, topic_focus_json, active_memes_json, soft_rules_json, hard_rules_json, reflection_summary, humor_density, emoji_rate, formality, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (group_id) DO UPDATE SET style_tags_json=EXCLUDED.style_tags_json, topic_focus_json=EXCLUDED.topic_focus_json, active_memes_json=EXCLUDED.active_memes_json, soft_rules_json=EXCLUDED.soft_rules_json, hard_rules_json=EXCLUDED.hard_rules_json, reflection_summary=EXCLUDED.reflection_summary, humor_density=EXCLUDED.humor_density, emoji_rate=EXCLUDED.emoji_rate, formality=EXCLUDED.formality, updated_at=EXCLUDED.updated_at`, args
	case "mysql":
		return `INSERT INTO group_profile (group_id, style_tags_json, topic_focus_json, active_memes_json, soft_rules_json, hard_rules_json, reflection_summary, humor_density, emoji_rate, formality, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE style_tags_json=VALUES(style_tags_json), topic_focus_json=VALUES(topic_focus_json), active_memes_json=VALUES(active_memes_json), soft_rules_json=VALUES(soft_rules_json), hard_rules_json=VALUES(hard_rules_json), reflection_summary=VALUES(reflection_summary), humor_density=VALUES(humor_density), emoji_rate=VALUES(emoji_rate), formality=VALUES(formality), updated_at=VALUES(updated_at)`, args
	default:
		return `INSERT INTO group_profile (group_id, style_tags_json, topic_focus_json, active_memes_json, soft_rules_json, hard_rules_json, reflection_summary, humor_density, emoji_rate, formality, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(group_id) DO UPDATE SET style_tags_json=excluded.style_tags_json, topic_focus_json=excluded.topic_focus_json, active_memes_json=excluded.active_memes_json, soft_rules_json=excluded.soft_rules_json, hard_rules_json=excluded.hard_rules_json, reflection_summary=excluded.reflection_summary, humor_density=excluded.humor_density, emoji_rate=excluded.emoji_rate, formality=excluded.formality, updated_at=excluded.updated_at`, args
	}
}

func (s *sqlStore) userProfileUpsert(item UserInGroupProfile, nicknamesJSON, preferencesJSON, styleJSON, tabooJSON string) (string, []any) {
	lastActiveAt := formatStoredTime(item.LastActiveAt)
	updatedAt := formatStoredTime(item.UpdatedAt)
	args := []any{item.GroupID, item.UserID, item.DisplayName, nicknamesJSON, preferencesJSON, styleJSON, tabooJSON, item.InteractionLevelWithBot, item.TeasingTolerance, item.TrustScore, lastActiveAt, updatedAt}
	switch s.engine {
	case "postgresql":
		return `INSERT INTO user_in_group_profile (group_id, user_id, display_name, nicknames_json, topic_preferences_json, style_tags_json, taboo_topics_json, interaction_level_with_bot, teasing_tolerance, trust_score, last_active_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT (group_id, user_id) DO UPDATE SET display_name=EXCLUDED.display_name, nicknames_json=EXCLUDED.nicknames_json, topic_preferences_json=EXCLUDED.topic_preferences_json, style_tags_json=EXCLUDED.style_tags_json, taboo_topics_json=EXCLUDED.taboo_topics_json, interaction_level_with_bot=EXCLUDED.interaction_level_with_bot, teasing_tolerance=EXCLUDED.teasing_tolerance, trust_score=EXCLUDED.trust_score, last_active_at=EXCLUDED.last_active_at, updated_at=EXCLUDED.updated_at`, args
	case "mysql":
		return `INSERT INTO user_in_group_profile (group_id, user_id, display_name, nicknames_json, topic_preferences_json, style_tags_json, taboo_topics_json, interaction_level_with_bot, teasing_tolerance, trust_score, last_active_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE display_name=VALUES(display_name), nicknames_json=VALUES(nicknames_json), topic_preferences_json=VALUES(topic_preferences_json), style_tags_json=VALUES(style_tags_json), taboo_topics_json=VALUES(taboo_topics_json), interaction_level_with_bot=VALUES(interaction_level_with_bot), teasing_tolerance=VALUES(teasing_tolerance), trust_score=VALUES(trust_score), last_active_at=VALUES(last_active_at), updated_at=VALUES(updated_at)`, args
	default:
		return `INSERT INTO user_in_group_profile (group_id, user_id, display_name, nicknames_json, topic_preferences_json, style_tags_json, taboo_topics_json, interaction_level_with_bot, teasing_tolerance, trust_score, last_active_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(group_id, user_id) DO UPDATE SET display_name=excluded.display_name, nicknames_json=excluded.nicknames_json, topic_preferences_json=excluded.topic_preferences_json, style_tags_json=excluded.style_tags_json, taboo_topics_json=excluded.taboo_topics_json, interaction_level_with_bot=excluded.interaction_level_with_bot, teasing_tolerance=excluded.teasing_tolerance, trust_score=excluded.trust_score, last_active_at=excluded.last_active_at, updated_at=excluded.updated_at`, args
	}
}

func (s *sqlStore) relationEdgeUpsert(item RelationEdge) (string, []any) {
	lastInteractionAt := formatStoredTime(item.LastInteractionAt)
	args := []any{item.ID, item.GroupID, item.NodeA, item.NodeB, item.RelationType, item.Strength, item.EvidenceCount, lastInteractionAt}
	switch s.engine {
	case "postgresql":
		return `INSERT INTO relation_edge (id, group_id, node_a, node_b, relation_type, strength, evidence_count, last_interaction_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (group_id, node_a, node_b, relation_type) DO UPDATE SET id=EXCLUDED.id, strength=EXCLUDED.strength, evidence_count=EXCLUDED.evidence_count, last_interaction_at=EXCLUDED.last_interaction_at`, args
	case "mysql":
		return `INSERT INTO relation_edge (id, group_id, node_a, node_b, relation_type, strength, evidence_count, last_interaction_at) VALUES (?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=VALUES(id), strength=VALUES(strength), evidence_count=VALUES(evidence_count), last_interaction_at=VALUES(last_interaction_at)`, args
	default:
		return `INSERT INTO relation_edge (id, group_id, node_a, node_b, relation_type, strength, evidence_count, last_interaction_at) VALUES (?,?,?,?,?,?,?,?) ON CONFLICT(group_id, node_a, node_b, relation_type) DO UPDATE SET id=excluded.id, strength=excluded.strength, evidence_count=excluded.evidence_count, last_interaction_at=excluded.last_interaction_at`, args
	}
}

func (s *sqlStore) deleteByIDQuery(tableName, id string) (string, []any) {
	switch s.engine {
	case "postgresql":
		return fmt.Sprintf("DELETE FROM %s WHERE id = $1", tableName), []any{id}
	default:
		return fmt.Sprintf("DELETE FROM %s WHERE id = ?", tableName), []any{id}
	}
}

func schemaStatements(engine string) []string {
	switch engine {
	case "mysql":
		return []string{
			`CREATE TABLE IF NOT EXISTS raw_message_log (msg_id VARCHAR(128) PRIMARY KEY, connection_id VARCHAR(128) NOT NULL, chat_type VARCHAR(32) NOT NULL, group_id VARCHAR(64) NOT NULL DEFAULT '', user_id VARCHAR(64) NOT NULL DEFAULT '', content_text TEXT NOT NULL, normalized_hash VARCHAR(64) NOT NULL DEFAULT '', reply_to_msg_id VARCHAR(128) NOT NULL DEFAULT '', created_at VARCHAR(64) NOT NULL, KEY idx_raw_message_log_reflection (chat_type, group_id, user_id, reply_to_msg_id, created_at))`,
			`CREATE TABLE IF NOT EXISTS ai_message_log (message_id VARCHAR(128) PRIMARY KEY, connection_id VARCHAR(128) NOT NULL, chat_type VARCHAR(32) NOT NULL, group_id VARCHAR(64) NOT NULL DEFAULT '', group_name VARCHAR(255) NOT NULL DEFAULT '', user_id VARCHAR(64) NOT NULL DEFAULT '', sender_role VARCHAR(32) NOT NULL, sender_name VARCHAR(128) NOT NULL DEFAULT '', sender_nickname VARCHAR(128) NOT NULL DEFAULT '', reply_to_message_id VARCHAR(128) NOT NULL DEFAULT '', text_content TEXT NOT NULL, normalized_hash VARCHAR(64) NOT NULL DEFAULT '', has_text TINYINT NOT NULL DEFAULT 0, has_image TINYINT NOT NULL DEFAULT 0, message_status VARCHAR(32) NOT NULL DEFAULT 'normal', occurred_at VARCHAR(64) NOT NULL, created_at VARCHAR(64) NOT NULL, KEY idx_ai_message_log_scope (chat_type, group_id, user_id, occurred_at))`,
			`CREATE TABLE IF NOT EXISTS ai_group_display_cache (connection_id VARCHAR(128) NOT NULL, group_id VARCHAR(64) NOT NULL, group_name VARCHAR(255) NOT NULL DEFAULT '', updated_at VARCHAR(64) NOT NULL, PRIMARY KEY (connection_id, group_id))`,
			`CREATE TABLE IF NOT EXISTS ai_user_display_cache (connection_id VARCHAR(128) NOT NULL, chat_type VARCHAR(32) NOT NULL, group_id VARCHAR(64) NOT NULL DEFAULT '', user_id VARCHAR(64) NOT NULL, display_name VARCHAR(128) NOT NULL DEFAULT '', nickname VARCHAR(128) NOT NULL DEFAULT '', updated_at VARCHAR(64) NOT NULL, PRIMARY KEY (connection_id, chat_type, group_id, user_id))`,
			`CREATE TABLE IF NOT EXISTS ai_message_image (id VARCHAR(160) PRIMARY KEY, message_id VARCHAR(128) NOT NULL, segment_index INT NOT NULL, origin_ref TEXT NOT NULL, vision_summary TEXT NOT NULL, vision_status VARCHAR(32) NOT NULL DEFAULT 'pending', created_at VARCHAR(64) NOT NULL, UNIQUE KEY uk_ai_message_image_identity (message_id, segment_index))`,
			`CREATE TABLE IF NOT EXISTS session_state (scope VARCHAR(128) PRIMARY KEY, group_id VARCHAR(64) NOT NULL DEFAULT '', recent_window_json LONGTEXT NOT NULL, topic_summary TEXT NOT NULL, active_users_json LONGTEXT NOT NULL, last_bot_action_json LONGTEXT NOT NULL, updated_at VARCHAR(64) NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS candidate_memory (id VARCHAR(128) PRIMARY KEY, scope VARCHAR(64) NOT NULL, memory_type VARCHAR(64) NOT NULL, subtype VARCHAR(64) NOT NULL, subject_id VARCHAR(64) NOT NULL DEFAULT '', group_id VARCHAR(64) NOT NULL DEFAULT '', content VARCHAR(512) NOT NULL, content_hash VARCHAR(64) NOT NULL, confidence DOUBLE NOT NULL, evidence_count INT NOT NULL, source_msg_ids_json LONGTEXT NOT NULL, status VARCHAR(32) NOT NULL, ttl_days INT NOT NULL, created_at VARCHAR(64) NOT NULL, last_seen_at VARCHAR(64) NOT NULL, UNIQUE KEY uk_candidate_memory_identity_hash (scope, memory_type, subtype, subject_id, group_id, content_hash))`,
			`CREATE TABLE IF NOT EXISTS long_term_memory (id VARCHAR(128) PRIMARY KEY, scope VARCHAR(64) NOT NULL, memory_type VARCHAR(64) NOT NULL, subtype VARCHAR(64) NOT NULL, subject_id VARCHAR(64) NOT NULL DEFAULT '', group_id VARCHAR(64) NOT NULL DEFAULT '', content VARCHAR(512) NOT NULL, content_hash VARCHAR(64) NOT NULL, confidence DOUBLE NOT NULL, evidence_count INT NOT NULL, source_refs_json LONGTEXT NOT NULL, ttl_days INT NOT NULL, created_at VARCHAR(64) NOT NULL, updated_at VARCHAR(64) NOT NULL, UNIQUE KEY uk_long_term_memory_identity_hash (scope, memory_type, subtype, subject_id, group_id, content_hash))`,
			`CREATE TABLE IF NOT EXISTS group_profile (group_id VARCHAR(64) PRIMARY KEY, style_tags_json LONGTEXT NOT NULL, topic_focus_json LONGTEXT NOT NULL, active_memes_json LONGTEXT NOT NULL, soft_rules_json LONGTEXT NOT NULL, hard_rules_json LONGTEXT NOT NULL, reflection_summary TEXT NOT NULL, humor_density DOUBLE NOT NULL, emoji_rate DOUBLE NOT NULL, formality DOUBLE NOT NULL, updated_at VARCHAR(64) NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS user_in_group_profile (group_id VARCHAR(64) NOT NULL, user_id VARCHAR(64) NOT NULL, display_name VARCHAR(128) NOT NULL DEFAULT '', nicknames_json LONGTEXT NOT NULL, topic_preferences_json LONGTEXT NOT NULL, style_tags_json LONGTEXT NOT NULL, taboo_topics_json LONGTEXT NOT NULL, interaction_level_with_bot INT NOT NULL, teasing_tolerance DOUBLE NOT NULL, trust_score DOUBLE NOT NULL, last_active_at VARCHAR(64) NOT NULL, updated_at VARCHAR(64) NOT NULL, PRIMARY KEY (group_id, user_id))`,
			`CREATE TABLE IF NOT EXISTS relation_edge (id VARCHAR(255) PRIMARY KEY, group_id VARCHAR(64) NOT NULL, node_a VARCHAR(64) NOT NULL, node_b VARCHAR(64) NOT NULL, relation_type VARCHAR(64) NOT NULL, strength DOUBLE NOT NULL, evidence_count INT NOT NULL, last_interaction_at VARCHAR(64) NOT NULL, UNIQUE KEY uk_relation_edge_identity (group_id, node_a, node_b, relation_type))`,
		}
	case "postgresql":
		return []string{
			`CREATE TABLE IF NOT EXISTS raw_message_log (msg_id VARCHAR(128) PRIMARY KEY, connection_id VARCHAR(128) NOT NULL, chat_type VARCHAR(32) NOT NULL, group_id VARCHAR(64) NOT NULL DEFAULT '', user_id VARCHAR(64) NOT NULL DEFAULT '', content_text TEXT NOT NULL, normalized_hash VARCHAR(64) NOT NULL DEFAULT '', reply_to_msg_id VARCHAR(128) NOT NULL DEFAULT '', created_at VARCHAR(64) NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS idx_raw_message_log_reflection ON raw_message_log (chat_type, group_id, user_id, reply_to_msg_id, created_at)`,
			`CREATE TABLE IF NOT EXISTS ai_message_log (message_id VARCHAR(128) PRIMARY KEY, connection_id VARCHAR(128) NOT NULL, chat_type VARCHAR(32) NOT NULL, group_id VARCHAR(64) NOT NULL DEFAULT '', group_name VARCHAR(255) NOT NULL DEFAULT '', user_id VARCHAR(64) NOT NULL DEFAULT '', sender_role VARCHAR(32) NOT NULL, sender_name VARCHAR(128) NOT NULL DEFAULT '', sender_nickname VARCHAR(128) NOT NULL DEFAULT '', reply_to_message_id VARCHAR(128) NOT NULL DEFAULT '', text_content TEXT NOT NULL, normalized_hash VARCHAR(64) NOT NULL DEFAULT '', has_text INTEGER NOT NULL DEFAULT 0, has_image INTEGER NOT NULL DEFAULT 0, message_status VARCHAR(32) NOT NULL DEFAULT 'normal', occurred_at VARCHAR(64) NOT NULL, created_at VARCHAR(64) NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS idx_ai_message_log_scope ON ai_message_log (chat_type, group_id, user_id, occurred_at)`,
			`CREATE TABLE IF NOT EXISTS ai_group_display_cache (connection_id VARCHAR(128) NOT NULL, group_id VARCHAR(64) NOT NULL, group_name VARCHAR(255) NOT NULL DEFAULT '', updated_at VARCHAR(64) NOT NULL, PRIMARY KEY (connection_id, group_id))`,
			`CREATE TABLE IF NOT EXISTS ai_user_display_cache (connection_id VARCHAR(128) NOT NULL, chat_type VARCHAR(32) NOT NULL, group_id VARCHAR(64) NOT NULL DEFAULT '', user_id VARCHAR(64) NOT NULL, display_name VARCHAR(128) NOT NULL DEFAULT '', nickname VARCHAR(128) NOT NULL DEFAULT '', updated_at VARCHAR(64) NOT NULL, PRIMARY KEY (connection_id, chat_type, group_id, user_id))`,
			`CREATE TABLE IF NOT EXISTS ai_message_image (id VARCHAR(160) PRIMARY KEY, message_id VARCHAR(128) NOT NULL, segment_index INTEGER NOT NULL, origin_ref TEXT NOT NULL, vision_summary TEXT NOT NULL, vision_status VARCHAR(32) NOT NULL DEFAULT 'pending', created_at VARCHAR(64) NOT NULL)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uk_ai_message_image_identity ON ai_message_image (message_id, segment_index)`,
			`CREATE TABLE IF NOT EXISTS session_state (scope VARCHAR(128) PRIMARY KEY, group_id VARCHAR(64) NOT NULL DEFAULT '', recent_window_json TEXT NOT NULL, topic_summary TEXT NOT NULL, active_users_json TEXT NOT NULL, last_bot_action_json TEXT NOT NULL, updated_at VARCHAR(64) NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS candidate_memory (id VARCHAR(128) PRIMARY KEY, scope VARCHAR(64) NOT NULL, memory_type VARCHAR(64) NOT NULL, subtype VARCHAR(64) NOT NULL, subject_id VARCHAR(64) NOT NULL DEFAULT '', group_id VARCHAR(64) NOT NULL DEFAULT '', content VARCHAR(512) NOT NULL, confidence DOUBLE PRECISION NOT NULL, evidence_count INTEGER NOT NULL, source_msg_ids_json TEXT NOT NULL, status VARCHAR(32) NOT NULL, ttl_days INTEGER NOT NULL, created_at VARCHAR(64) NOT NULL, last_seen_at VARCHAR(64) NOT NULL)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uk_candidate_memory_identity ON candidate_memory (scope, memory_type, subtype, subject_id, group_id, content)`,
			`CREATE TABLE IF NOT EXISTS long_term_memory (id VARCHAR(128) PRIMARY KEY, scope VARCHAR(64) NOT NULL, memory_type VARCHAR(64) NOT NULL, subtype VARCHAR(64) NOT NULL, subject_id VARCHAR(64) NOT NULL DEFAULT '', group_id VARCHAR(64) NOT NULL DEFAULT '', content VARCHAR(512) NOT NULL, confidence DOUBLE PRECISION NOT NULL, evidence_count INTEGER NOT NULL, source_refs_json TEXT NOT NULL, ttl_days INTEGER NOT NULL, created_at VARCHAR(64) NOT NULL, updated_at VARCHAR(64) NOT NULL)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uk_long_term_memory_identity ON long_term_memory (scope, memory_type, subtype, subject_id, group_id, content)`,
			`CREATE TABLE IF NOT EXISTS group_profile (group_id VARCHAR(64) PRIMARY KEY, style_tags_json TEXT NOT NULL, topic_focus_json TEXT NOT NULL, active_memes_json TEXT NOT NULL, soft_rules_json TEXT NOT NULL, hard_rules_json TEXT NOT NULL, reflection_summary TEXT NOT NULL DEFAULT '', humor_density DOUBLE PRECISION NOT NULL, emoji_rate DOUBLE PRECISION NOT NULL, formality DOUBLE PRECISION NOT NULL, updated_at VARCHAR(64) NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS user_in_group_profile (group_id VARCHAR(64) NOT NULL, user_id VARCHAR(64) NOT NULL, display_name VARCHAR(128) NOT NULL DEFAULT '', nicknames_json TEXT NOT NULL, topic_preferences_json TEXT NOT NULL, style_tags_json TEXT NOT NULL, taboo_topics_json TEXT NOT NULL, interaction_level_with_bot INTEGER NOT NULL, teasing_tolerance DOUBLE PRECISION NOT NULL, trust_score DOUBLE PRECISION NOT NULL, last_active_at VARCHAR(64) NOT NULL, updated_at VARCHAR(64) NOT NULL, PRIMARY KEY (group_id, user_id))`,
			`CREATE TABLE IF NOT EXISTS relation_edge (id VARCHAR(255) PRIMARY KEY, group_id VARCHAR(64) NOT NULL, node_a VARCHAR(64) NOT NULL, node_b VARCHAR(64) NOT NULL, relation_type VARCHAR(64) NOT NULL, strength DOUBLE PRECISION NOT NULL, evidence_count INTEGER NOT NULL, last_interaction_at VARCHAR(64) NOT NULL)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uk_relation_edge_identity ON relation_edge (group_id, node_a, node_b, relation_type)`,
		}
	default:
		return []string{
			`CREATE TABLE IF NOT EXISTS raw_message_log (msg_id TEXT PRIMARY KEY, connection_id TEXT NOT NULL, chat_type TEXT NOT NULL, group_id TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL DEFAULT '', content_text TEXT NOT NULL, normalized_hash TEXT NOT NULL DEFAULT '', reply_to_msg_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS idx_raw_message_log_reflection ON raw_message_log (chat_type, group_id, user_id, reply_to_msg_id, created_at)`,
			`CREATE TABLE IF NOT EXISTS ai_message_log (message_id TEXT PRIMARY KEY, connection_id TEXT NOT NULL, chat_type TEXT NOT NULL, group_id TEXT NOT NULL DEFAULT '', group_name TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL DEFAULT '', sender_role TEXT NOT NULL, sender_name TEXT NOT NULL DEFAULT '', sender_nickname TEXT NOT NULL DEFAULT '', reply_to_message_id TEXT NOT NULL DEFAULT '', text_content TEXT NOT NULL, normalized_hash TEXT NOT NULL DEFAULT '', has_text INTEGER NOT NULL DEFAULT 0, has_image INTEGER NOT NULL DEFAULT 0, message_status TEXT NOT NULL DEFAULT 'normal', occurred_at TEXT NOT NULL, created_at TEXT NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS idx_ai_message_log_scope ON ai_message_log (chat_type, group_id, user_id, occurred_at)`,
			`CREATE TABLE IF NOT EXISTS ai_group_display_cache (connection_id TEXT NOT NULL, group_id TEXT NOT NULL, group_name TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL, PRIMARY KEY (connection_id, group_id))`,
			`CREATE TABLE IF NOT EXISTS ai_user_display_cache (connection_id TEXT NOT NULL, chat_type TEXT NOT NULL, group_id TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL, display_name TEXT NOT NULL DEFAULT '', nickname TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL, PRIMARY KEY (connection_id, chat_type, group_id, user_id))`,
			`CREATE TABLE IF NOT EXISTS ai_message_image (id TEXT PRIMARY KEY, message_id TEXT NOT NULL, segment_index INTEGER NOT NULL, origin_ref TEXT NOT NULL, vision_summary TEXT NOT NULL, vision_status TEXT NOT NULL DEFAULT 'pending', created_at TEXT NOT NULL)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uk_ai_message_image_identity ON ai_message_image (message_id, segment_index)`,
			`CREATE TABLE IF NOT EXISTS session_state (scope TEXT PRIMARY KEY, group_id TEXT NOT NULL DEFAULT '', recent_window_json TEXT NOT NULL, topic_summary TEXT NOT NULL, active_users_json TEXT NOT NULL, last_bot_action_json TEXT NOT NULL, updated_at TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS candidate_memory (id TEXT PRIMARY KEY, scope TEXT NOT NULL, memory_type TEXT NOT NULL, subtype TEXT NOT NULL, subject_id TEXT NOT NULL DEFAULT '', group_id TEXT NOT NULL DEFAULT '', content TEXT NOT NULL, confidence REAL NOT NULL, evidence_count INTEGER NOT NULL, source_msg_ids_json TEXT NOT NULL, status TEXT NOT NULL, ttl_days INTEGER NOT NULL, created_at TEXT NOT NULL, last_seen_at TEXT NOT NULL)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uk_candidate_memory_identity ON candidate_memory (scope, memory_type, subtype, subject_id, group_id, content)`,
			`CREATE TABLE IF NOT EXISTS long_term_memory (id TEXT PRIMARY KEY, scope TEXT NOT NULL, memory_type TEXT NOT NULL, subtype TEXT NOT NULL, subject_id TEXT NOT NULL DEFAULT '', group_id TEXT NOT NULL DEFAULT '', content TEXT NOT NULL, confidence REAL NOT NULL, evidence_count INTEGER NOT NULL, source_refs_json TEXT NOT NULL, ttl_days INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uk_long_term_memory_identity ON long_term_memory (scope, memory_type, subtype, subject_id, group_id, content)`,
			`CREATE TABLE IF NOT EXISTS group_profile (group_id TEXT PRIMARY KEY, style_tags_json TEXT NOT NULL, topic_focus_json TEXT NOT NULL, active_memes_json TEXT NOT NULL, soft_rules_json TEXT NOT NULL, hard_rules_json TEXT NOT NULL, reflection_summary TEXT NOT NULL DEFAULT '', humor_density REAL NOT NULL, emoji_rate REAL NOT NULL, formality REAL NOT NULL, updated_at TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS user_in_group_profile (group_id TEXT NOT NULL, user_id TEXT NOT NULL, display_name TEXT NOT NULL DEFAULT '', nicknames_json TEXT NOT NULL, topic_preferences_json TEXT NOT NULL, style_tags_json TEXT NOT NULL, taboo_topics_json TEXT NOT NULL, interaction_level_with_bot INTEGER NOT NULL, teasing_tolerance REAL NOT NULL, trust_score REAL NOT NULL, last_active_at TEXT NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY (group_id, user_id))`,
			`CREATE TABLE IF NOT EXISTS relation_edge (id TEXT PRIMARY KEY, group_id TEXT NOT NULL, node_a TEXT NOT NULL, node_b TEXT NOT NULL, relation_type TEXT NOT NULL, strength REAL NOT NULL, evidence_count INTEGER NOT NULL, last_interaction_at TEXT NOT NULL)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uk_relation_edge_identity ON relation_edge (group_id, node_a, node_b, relation_type)`,
		}
	}
}

func migrationStatements(engine string) []string {
	switch engine {
	case "postgresql":
		return []string{
			`ALTER TABLE group_profile ADD COLUMN IF NOT EXISTS reflection_summary TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE ai_message_log ADD COLUMN IF NOT EXISTS group_name VARCHAR(255) NOT NULL DEFAULT ''`,
			`ALTER TABLE ai_message_log ADD COLUMN IF NOT EXISTS sender_nickname VARCHAR(128) NOT NULL DEFAULT ''`,
			`CREATE INDEX IF NOT EXISTS idx_raw_message_log_reflection ON raw_message_log (chat_type, group_id, user_id, reply_to_msg_id, created_at)`,
		}
	case "mysql":
		return []string{
			`ALTER TABLE group_profile ADD COLUMN reflection_summary TEXT NULL`,
			`ALTER TABLE ai_message_log ADD COLUMN group_name VARCHAR(255) NOT NULL DEFAULT ''`,
			`ALTER TABLE ai_message_log ADD COLUMN sender_nickname VARCHAR(128) NOT NULL DEFAULT ''`,
			`ALTER TABLE raw_message_log ADD INDEX idx_raw_message_log_reflection (chat_type, group_id, user_id, reply_to_msg_id, created_at)`,
			`ALTER TABLE candidate_memory ADD COLUMN content_hash VARCHAR(64) NOT NULL DEFAULT ''`,
			`UPDATE candidate_memory SET content_hash = SHA2(content, 256) WHERE content_hash = ''`,
			`ALTER TABLE candidate_memory ADD UNIQUE KEY uk_candidate_memory_identity_hash (scope, memory_type, subtype, subject_id, group_id, content_hash)`,
			`ALTER TABLE long_term_memory ADD COLUMN content_hash VARCHAR(64) NOT NULL DEFAULT ''`,
			`UPDATE long_term_memory SET content_hash = SHA2(content, 256) WHERE content_hash = ''`,
			`ALTER TABLE long_term_memory ADD UNIQUE KEY uk_long_term_memory_identity_hash (scope, memory_type, subtype, subject_id, group_id, content_hash)`,
		}
	default:
		return []string{
			`ALTER TABLE group_profile ADD COLUMN reflection_summary TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE ai_message_log ADD COLUMN group_name TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE ai_message_log ADD COLUMN sender_nickname TEXT NOT NULL DEFAULT ''`,
			`CREATE INDEX IF NOT EXISTS idx_raw_message_log_reflection ON raw_message_log (chat_type, group_id, user_id, reply_to_msg_id, created_at)`,
		}
	}
}

func isIgnorableMigrationError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column") ||
		strings.Contains(msg, "duplicate column name") ||
		strings.Contains(msg, "duplicate key name") ||
		strings.Contains(msg, "doesn't exist") ||
		strings.Contains(msg, "already exists")
}

func encodeJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("编码 JSON 失败: %w", err)
	}
	return string(payload), nil
}

func decodeStringList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func decodeJSONInto[T any](raw string, target *T) error {
	if target == nil {
		return nil
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), target)
}

func formatStoredTime(value time.Time) string {
	if value.IsZero() {
		return time.Now().Format(time.RFC3339Nano)
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseStoredTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func normalizeLocalStorageEngine(engine string) string {
	switch strings.TrimSpace(strings.ToLower(engine)) {
	case "", "sqlite":
		return "sqlite"
	case "mysql":
		return "mysql"
	case "postgres", "postgresql":
		return "postgresql"
	default:
		return strings.TrimSpace(strings.ToLower(engine))
	}
}
