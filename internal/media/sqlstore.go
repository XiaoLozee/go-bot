package media

import (
	"context"
	"database/sql"
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
	engine := normalizeStorageEngine(cfg.Engine)
	driver, dsn, err := openDriverAndDSN(cfg, engine)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("打开媒体数据库连接失败: %w", err)
	}
	configureSQLDB(db, engine)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接媒体数据库失败: %w", err)
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
		pgCfg := cfg.PostgreSQL
		sslMode := strings.TrimSpace(pgCfg.SSLMode)
		if sslMode == "" {
			sslMode = "disable"
		}
		schema := strings.TrimSpace(pgCfg.Schema)
		if schema == "" {
			schema = "public"
		}
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s search_path=%s", pgCfg.Host, pgCfg.Port, pgCfg.Username, pgCfg.Password, pgCfg.Database, sslMode, schema)
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

func normalizeStorageEngine(engine string) string {
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

func (s *sqlStore) UpsertAsset(ctx context.Context, item Asset) error {
	query, args := s.assetUpsert(item)
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("写入媒体资源失败: %w", err)
	}
	return nil
}

func (s *sqlStore) initSchema(ctx context.Context) error {
	for _, stmt := range schemaStatements(s.engine) {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("初始化 %s 媒体表失败: %w", s.engine, err)
		}
	}
	return nil
}

func (s *sqlStore) assetUpsert(item Asset) (string, []any) {
	args := []any{
		item.ID,
		item.MessageID,
		item.ConnectionID,
		item.ChatType,
		item.GroupID,
		item.UserID,
		item.SegmentIndex,
		item.SegmentType,
		item.FileName,
		item.OriginRef,
		item.OriginURL,
		item.MimeType,
		item.SizeBytes,
		item.SHA256,
		item.StorageBackend,
		item.StorageKey,
		item.PublicURL,
		item.Status,
		item.Error,
		item.SegmentDataJSON,
		formatStoredTime(item.CreatedAt),
		formatStoredTime(item.UpdatedAt),
	}
	switch s.engine {
	case "postgresql":
		return `INSERT INTO media_asset (id, message_id, connection_id, chat_type, group_id, user_id, segment_index, segment_type, file_name, origin_ref, origin_url, mime_type, size_bytes, sha256, storage_backend, storage_key, public_url, status, error, segment_data_json, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22) ON CONFLICT (message_id, segment_index) DO UPDATE SET connection_id=EXCLUDED.connection_id, chat_type=EXCLUDED.chat_type, group_id=EXCLUDED.group_id, user_id=EXCLUDED.user_id, segment_type=EXCLUDED.segment_type, file_name=EXCLUDED.file_name, origin_ref=EXCLUDED.origin_ref, origin_url=EXCLUDED.origin_url, mime_type=EXCLUDED.mime_type, size_bytes=EXCLUDED.size_bytes, sha256=EXCLUDED.sha256, storage_backend=EXCLUDED.storage_backend, storage_key=EXCLUDED.storage_key, public_url=EXCLUDED.public_url, status=EXCLUDED.status, error=EXCLUDED.error, segment_data_json=EXCLUDED.segment_data_json, updated_at=EXCLUDED.updated_at`, args
	case "mysql":
		return `INSERT INTO media_asset (id, message_id, connection_id, chat_type, group_id, user_id, segment_index, segment_type, file_name, origin_ref, origin_url, mime_type, size_bytes, sha256, storage_backend, storage_key, public_url, status, error, segment_data_json, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE connection_id=VALUES(connection_id), chat_type=VALUES(chat_type), group_id=VALUES(group_id), user_id=VALUES(user_id), segment_type=VALUES(segment_type), file_name=VALUES(file_name), origin_ref=VALUES(origin_ref), origin_url=VALUES(origin_url), mime_type=VALUES(mime_type), size_bytes=VALUES(size_bytes), sha256=VALUES(sha256), storage_backend=VALUES(storage_backend), storage_key=VALUES(storage_key), public_url=VALUES(public_url), status=VALUES(status), error=VALUES(error), segment_data_json=VALUES(segment_data_json), updated_at=VALUES(updated_at)`, args
	default:
		return `INSERT INTO media_asset (id, message_id, connection_id, chat_type, group_id, user_id, segment_index, segment_type, file_name, origin_ref, origin_url, mime_type, size_bytes, sha256, storage_backend, storage_key, public_url, status, error, segment_data_json, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(message_id, segment_index) DO UPDATE SET connection_id=excluded.connection_id, chat_type=excluded.chat_type, group_id=excluded.group_id, user_id=excluded.user_id, segment_type=excluded.segment_type, file_name=excluded.file_name, origin_ref=excluded.origin_ref, origin_url=excluded.origin_url, mime_type=excluded.mime_type, size_bytes=excluded.size_bytes, sha256=excluded.sha256, storage_backend=excluded.storage_backend, storage_key=excluded.storage_key, public_url=excluded.public_url, status=excluded.status, error=excluded.error, segment_data_json=excluded.segment_data_json, updated_at=excluded.updated_at`, args
	}
}

func schemaStatements(engine string) []string {
	switch engine {
	case "mysql":
		return []string{
			`CREATE TABLE IF NOT EXISTS media_asset (id VARCHAR(160) PRIMARY KEY, message_id VARCHAR(128) NOT NULL, connection_id VARCHAR(128) NOT NULL, chat_type VARCHAR(32) NOT NULL, group_id VARCHAR(64) NOT NULL DEFAULT '', user_id VARCHAR(64) NOT NULL DEFAULT '', segment_index INT NOT NULL, segment_type VARCHAR(32) NOT NULL, file_name VARCHAR(255) NOT NULL DEFAULT '', origin_ref TEXT NOT NULL, origin_url TEXT NOT NULL, mime_type VARCHAR(128) NOT NULL DEFAULT '', size_bytes BIGINT NOT NULL DEFAULT 0, sha256 VARCHAR(128) NOT NULL DEFAULT '', storage_backend VARCHAR(32) NOT NULL, storage_key VARCHAR(512) NOT NULL DEFAULT '', public_url TEXT NOT NULL, status VARCHAR(32) NOT NULL, error TEXT NOT NULL, segment_data_json LONGTEXT NOT NULL, created_at VARCHAR(64) NOT NULL, updated_at VARCHAR(64) NOT NULL, UNIQUE KEY uk_media_asset_message_segment (message_id, segment_index))`,
		}
	case "postgresql":
		return []string{
			`CREATE TABLE IF NOT EXISTS media_asset (id VARCHAR(160) PRIMARY KEY, message_id VARCHAR(128) NOT NULL, connection_id VARCHAR(128) NOT NULL, chat_type VARCHAR(32) NOT NULL, group_id VARCHAR(64) NOT NULL DEFAULT '', user_id VARCHAR(64) NOT NULL DEFAULT '', segment_index INTEGER NOT NULL, segment_type VARCHAR(32) NOT NULL, file_name VARCHAR(255) NOT NULL DEFAULT '', origin_ref TEXT NOT NULL, origin_url TEXT NOT NULL, mime_type VARCHAR(128) NOT NULL DEFAULT '', size_bytes BIGINT NOT NULL DEFAULT 0, sha256 VARCHAR(128) NOT NULL DEFAULT '', storage_backend VARCHAR(32) NOT NULL, storage_key VARCHAR(512) NOT NULL DEFAULT '', public_url TEXT NOT NULL, status VARCHAR(32) NOT NULL, error TEXT NOT NULL, segment_data_json TEXT NOT NULL, created_at VARCHAR(64) NOT NULL, updated_at VARCHAR(64) NOT NULL)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uk_media_asset_message_segment ON media_asset (message_id, segment_index)`,
		}
	default:
		return []string{
			`CREATE TABLE IF NOT EXISTS media_asset (id TEXT PRIMARY KEY, message_id TEXT NOT NULL, connection_id TEXT NOT NULL, chat_type TEXT NOT NULL, group_id TEXT NOT NULL DEFAULT '', user_id TEXT NOT NULL DEFAULT '', segment_index INTEGER NOT NULL, segment_type TEXT NOT NULL, file_name TEXT NOT NULL DEFAULT '', origin_ref TEXT NOT NULL, origin_url TEXT NOT NULL, mime_type TEXT NOT NULL DEFAULT '', size_bytes INTEGER NOT NULL DEFAULT 0, sha256 TEXT NOT NULL DEFAULT '', storage_backend TEXT NOT NULL, storage_key TEXT NOT NULL DEFAULT '', public_url TEXT NOT NULL, status TEXT NOT NULL, error TEXT NOT NULL, segment_data_json TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
			`CREATE UNIQUE INDEX IF NOT EXISTS uk_media_asset_message_segment ON media_asset (message_id, segment_index)`,
		}
	}
}

func formatStoredTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
