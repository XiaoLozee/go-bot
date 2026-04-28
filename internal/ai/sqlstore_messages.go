package ai

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func (s *sqlStore) ListMessageLogs(ctx context.Context, query MessageLogQuery) ([]MessageLog, error) {
	query = normalizeMessageLogQuery(query)
	whereClause, args := s.messageLogWhereClause(query, 1)
	sqlQuery := `SELECT l.message_id, l.connection_id, l.chat_type, l.group_id, COALESCE(NULLIF(gc.group_name, ''), NULLIF(l.group_name, ''), '') AS group_name, l.user_id, l.sender_role, COALESCE(NULLIF(uc.display_name, ''), NULLIF(l.sender_name, ''), '') AS sender_name, COALESCE(NULLIF(uc.nickname, ''), NULLIF(l.sender_nickname, ''), NULLIF(uc.display_name, ''), '') AS sender_nickname, l.reply_to_message_id, l.text_content, l.normalized_hash, l.has_text, l.has_image, l.message_status, l.occurred_at, l.created_at, COALESCE((SELECT COUNT(1) FROM ai_message_image img WHERE img.message_id = l.message_id), 0) AS image_count FROM ai_message_log l LEFT JOIN ai_group_display_cache gc ON gc.connection_id = l.connection_id AND gc.group_id = l.group_id LEFT JOIN ai_user_display_cache uc ON uc.connection_id = l.connection_id AND uc.chat_type = l.chat_type AND uc.group_id = l.group_id AND uc.user_id = l.user_id`
	if whereClause != "" {
		sqlQuery += " WHERE " + whereClause
	}
	sqlQuery += " ORDER BY l.occurred_at DESC LIMIT " + strconv.Itoa(query.Limit)

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("查询 AI 聊天消息失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]MessageLog, 0, minInt(query.Limit, 64))
	for rows.Next() {
		var item MessageLog
		var occurredAt string
		var createdAt string
		var hasText int64
		var hasImage int64
		if err := rows.Scan(
			&item.MessageID,
			&item.ConnectionID,
			&item.ChatType,
			&item.GroupID,
			&item.GroupName,
			&item.UserID,
			&item.SenderRole,
			&item.SenderName,
			&item.SenderNickname,
			&item.ReplyToMessageID,
			&item.TextContent,
			&item.NormalizedHash,
			&hasText,
			&hasImage,
			&item.MessageStatus,
			&occurredAt,
			&createdAt,
			&item.ImageCount,
		); err != nil {
			return nil, fmt.Errorf("读取 AI 聊天消息失败: %w", err)
		}
		item.HasText = hasText != 0
		item.HasImage = hasImage != 0
		item.OccurredAt = parseStoredTime(occurredAt)
		item.CreatedAt = parseStoredTime(createdAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) ListMessageSearchSuggestions(ctx context.Context, query MessageSuggestionQuery) (MessageSearchSuggestions, error) {
	query = normalizeMessageSuggestionQuery(query)
	filters := normalizeMessageLogQuery(MessageLogQuery{
		ChatType: query.ChatType,
		GroupID:  query.GroupID,
		UserID:   query.UserID,
		Limit:    query.Limit,
	})

	result := MessageSearchSuggestions{
		Groups: []string{},
		Users:  []string{},
	}
	if query.ChatType == "" || query.ChatType == "group" {
		groups, err := s.listDistinctMessageLogValues(ctx, "group_id", filters, query.Limit)
		if err != nil {
			return MessageSearchSuggestions{}, err
		}
		result.Groups = groups
	}

	if query.ChatType == "" || query.ChatType == "private" || query.ChatType == "group" {
		users, err := s.listDistinctMessageLogValues(ctx, "user_id", filters, query.Limit)
		if err != nil {
			return MessageSearchSuggestions{}, err
		}
		result.Users = users
	}
	return result, nil
}

func (s *sqlStore) GetMessageDetail(ctx context.Context, messageID string) (MessageDetail, error) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return MessageDetail{}, fmt.Errorf("messageID 不能为空")
	}

	item, err := s.loadMessageLogByID(ctx, messageID)
	if err != nil {
		return MessageDetail{}, err
	}
	images, err := s.loadMessageImages(ctx, messageID)
	if err != nil {
		return MessageDetail{}, err
	}
	assetMap, err := s.loadMediaAssetsByMessageID(ctx, messageID)
	if err != nil {
		return MessageDetail{}, err
	}
	for i := range images {
		if asset, ok := assetMap[images[i].SegmentIndex]; ok {
			images[i].AssetStatus = asset.AssetStatus
			images[i].AssetError = asset.AssetError
			images[i].StorageBackend = asset.StorageBackend
			images[i].StorageKey = asset.StorageKey
			images[i].PublicURL = asset.PublicURL
			images[i].FileName = asset.FileName
			images[i].MimeType = asset.MimeType
			images[i].SizeBytes = asset.SizeBytes
			images[i].SHA256 = asset.SHA256
		}
	}
	item.ImageCount = len(images)
	return MessageDetail{Message: item, Images: images}, nil
}

func (s *sqlStore) AppendMessageLog(ctx context.Context, item MessageLog) error {
	query, args := s.messageLogUpsert(item)
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("写入 AI 聊天消息失败: %w", err)
	}
	if err := s.UpsertMessageDisplayHints(ctx, item); err != nil {
		s.logger.Warn("写入 AI 显示缓存失败", "error", err, "message_id", item.MessageID)
	}
	return nil
}

func (s *sqlStore) AppendMessageImages(ctx context.Context, items []MessageImage) error {
	for _, item := range items {
		query, args := s.messageImageUpsert(item)
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("写入 AI 聊天图片失败: %w", err)
		}
	}
	return nil
}

func (s *sqlStore) loadMessageLogByID(ctx context.Context, messageID string) (MessageLog, error) {
	query := `SELECT l.message_id, l.connection_id, l.chat_type, l.group_id, COALESCE(NULLIF(gc.group_name, ''), NULLIF(l.group_name, ''), '') AS group_name, l.user_id, l.sender_role, COALESCE(NULLIF(uc.display_name, ''), NULLIF(l.sender_name, ''), '') AS sender_name, COALESCE(NULLIF(uc.nickname, ''), NULLIF(l.sender_nickname, ''), NULLIF(uc.display_name, ''), '') AS sender_nickname, l.reply_to_message_id, l.text_content, l.normalized_hash, l.has_text, l.has_image, l.message_status, l.occurred_at, l.created_at, COALESCE((SELECT COUNT(1) FROM ai_message_image img WHERE img.message_id = l.message_id), 0) AS image_count FROM ai_message_log l LEFT JOIN ai_group_display_cache gc ON gc.connection_id = l.connection_id AND gc.group_id = l.group_id LEFT JOIN ai_user_display_cache uc ON uc.connection_id = l.connection_id AND uc.chat_type = l.chat_type AND uc.group_id = l.group_id AND uc.user_id = l.user_id WHERE l.message_id = `
	args := []any{messageID}
	if s.engine == "postgresql" {
		query += `$1`
	} else {
		query += `?`
	}

	var item MessageLog
	var occurredAt string
	var createdAt string
	var hasText int64
	var hasImage int64
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&item.MessageID,
		&item.ConnectionID,
		&item.ChatType,
		&item.GroupID,
		&item.GroupName,
		&item.UserID,
		&item.SenderRole,
		&item.SenderName,
		&item.SenderNickname,
		&item.ReplyToMessageID,
		&item.TextContent,
		&item.NormalizedHash,
		&hasText,
		&hasImage,
		&item.MessageStatus,
		&occurredAt,
		&createdAt,
		&item.ImageCount,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return MessageLog{}, fmt.Errorf("聊天消息不存在: %s", messageID)
		}
		return MessageLog{}, fmt.Errorf("读取 AI 聊天消息失败: %w", err)
	}
	item.HasText = hasText != 0
	item.HasImage = hasImage != 0
	item.OccurredAt = parseStoredTime(occurredAt)
	item.CreatedAt = parseStoredTime(createdAt)
	return item, nil
}

func (s *sqlStore) UpsertMessageDisplayHints(ctx context.Context, item MessageLog) error {
	if groupCache, ok := buildMessageGroupDisplayCache(item); ok {
		query, args := s.groupDisplayCacheUpsert(groupCache)
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("写入群显示缓存失败: %w", err)
		}
	}
	if userCache, ok := buildMessageUserDisplayCache(item); ok {
		query, args := s.userDisplayCacheUpsert(userCache)
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("写入用户显示缓存失败: %w", err)
		}
	}
	return nil
}

func (s *sqlStore) loadMessageImages(ctx context.Context, messageID string) ([]MessageImage, error) {
	query := `SELECT id, message_id, segment_index, origin_ref, vision_summary, vision_status, created_at FROM ai_message_image WHERE message_id = `
	args := []any{messageID}
	if s.engine == "postgresql" {
		query += `$1`
	} else {
		query += `?`
	}
	query += ` ORDER BY segment_index ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("读取 AI 聊天图片失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]MessageImage, 0, 4)
	for rows.Next() {
		var item MessageImage
		var createdAt string
		if err := rows.Scan(&item.ID, &item.MessageID, &item.SegmentIndex, &item.OriginRef, &item.VisionSummary, &item.VisionStatus, &createdAt); err != nil {
			return nil, fmt.Errorf("读取 AI 聊天图片失败: %w", err)
		}
		item.CreatedAt = parseStoredTime(createdAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) loadMediaAssetsByMessageID(ctx context.Context, messageID string) (map[int]MessageImage, error) {
	query := `SELECT segment_index, file_name, mime_type, size_bytes, sha256, storage_backend, storage_key, public_url, status, error FROM media_asset WHERE message_id = `
	args := []any{messageID}
	if s.engine == "postgresql" {
		query += `$1`
	} else {
		query += `?`
	}
	query += ` ORDER BY segment_index ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		if isIgnorableMissingTableError(err) {
			return map[int]MessageImage{}, nil
		}
		return nil, fmt.Errorf("读取媒体资源失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make(map[int]MessageImage)
	for rows.Next() {
		var segmentIndex int
		var item MessageImage
		if err := rows.Scan(&segmentIndex, &item.FileName, &item.MimeType, &item.SizeBytes, &item.SHA256, &item.StorageBackend, &item.StorageKey, &item.PublicURL, &item.AssetStatus, &item.AssetError); err != nil {
			return nil, fmt.Errorf("读取媒体资源失败: %w", err)
		}
		item.SegmentIndex = segmentIndex
		items[segmentIndex] = item
	}
	return items, rows.Err()
}

func (s *sqlStore) listDistinctMessageLogValues(ctx context.Context, field string, query MessageLogQuery, limit int) ([]string, error) {
	var column string
	switch strings.TrimSpace(field) {
	case "group_id":
		column = "group_id"
	case "user_id":
		column = "user_id"
	default:
		return nil, fmt.Errorf("不支持的聊天记录联想字段: %s", field)
	}

	query = normalizeMessageLogQuery(query)
	whereClause, args := s.messageLogWhereClause(query, 1)
	nonEmptyClause := "l." + column + " <> ''"
	if whereClause == "" {
		whereClause = nonEmptyClause
	} else {
		whereClause += " AND " + nonEmptyClause
	}

	sqlQuery := `SELECT l.` + column + `, MAX(l.occurred_at) AS latest_occurred_at FROM ai_message_log l`
	if whereClause != "" {
		sqlQuery += ` WHERE ` + whereClause
	}
	sqlQuery += ` GROUP BY l.` + column + ` ORDER BY latest_occurred_at DESC, l.` + column + ` ASC LIMIT ` + strconv.Itoa(maxInt(1, limit))

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("查询 AI 聊天记录联想失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	values := make([]string, 0, minInt(limit, 16))
	for rows.Next() {
		var value string
		var latestOccurredAt string
		if err := rows.Scan(&value, &latestOccurredAt); err != nil {
			return nil, fmt.Errorf("读取 AI 聊天记录联想失败: %w", err)
		}
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}
	return values, rows.Err()
}

func (s *sqlStore) messageLogUpsert(item MessageLog) (string, []any) {
	occurredAt := formatStoredTime(item.OccurredAt)
	createdAt := formatStoredTime(item.CreatedAt)
	args := []any{
		item.MessageID,
		item.ConnectionID,
		item.ChatType,
		item.GroupID,
		item.GroupName,
		item.UserID,
		item.SenderRole,
		item.SenderName,
		item.SenderNickname,
		item.ReplyToMessageID,
		item.TextContent,
		item.NormalizedHash,
		boolToInt(item.HasText),
		boolToInt(item.HasImage),
		firstNonEmpty(strings.TrimSpace(item.MessageStatus), "normal"),
		occurredAt,
		createdAt,
	}
	switch s.engine {
	case "postgresql":
		return `INSERT INTO ai_message_log (message_id, connection_id, chat_type, group_id, group_name, user_id, sender_role, sender_name, sender_nickname, reply_to_message_id, text_content, normalized_hash, has_text, has_image, message_status, occurred_at, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) ON CONFLICT (message_id) DO UPDATE SET connection_id=EXCLUDED.connection_id, chat_type=EXCLUDED.chat_type, group_id=EXCLUDED.group_id, group_name=EXCLUDED.group_name, user_id=EXCLUDED.user_id, sender_role=EXCLUDED.sender_role, sender_name=EXCLUDED.sender_name, sender_nickname=EXCLUDED.sender_nickname, reply_to_message_id=EXCLUDED.reply_to_message_id, text_content=EXCLUDED.text_content, normalized_hash=EXCLUDED.normalized_hash, has_text=EXCLUDED.has_text, has_image=EXCLUDED.has_image, message_status=EXCLUDED.message_status, occurred_at=EXCLUDED.occurred_at, created_at=EXCLUDED.created_at`, args
	case "mysql":
		return `INSERT INTO ai_message_log (message_id, connection_id, chat_type, group_id, group_name, user_id, sender_role, sender_name, sender_nickname, reply_to_message_id, text_content, normalized_hash, has_text, has_image, message_status, occurred_at, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE connection_id=VALUES(connection_id), chat_type=VALUES(chat_type), group_id=VALUES(group_id), group_name=VALUES(group_name), user_id=VALUES(user_id), sender_role=VALUES(sender_role), sender_name=VALUES(sender_name), sender_nickname=VALUES(sender_nickname), reply_to_message_id=VALUES(reply_to_message_id), text_content=VALUES(text_content), normalized_hash=VALUES(normalized_hash), has_text=VALUES(has_text), has_image=VALUES(has_image), message_status=VALUES(message_status), occurred_at=VALUES(occurred_at), created_at=VALUES(created_at)`, args
	default:
		return `INSERT INTO ai_message_log (message_id, connection_id, chat_type, group_id, group_name, user_id, sender_role, sender_name, sender_nickname, reply_to_message_id, text_content, normalized_hash, has_text, has_image, message_status, occurred_at, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(message_id) DO UPDATE SET connection_id=excluded.connection_id, chat_type=excluded.chat_type, group_id=excluded.group_id, group_name=excluded.group_name, user_id=excluded.user_id, sender_role=excluded.sender_role, sender_name=excluded.sender_name, sender_nickname=excluded.sender_nickname, reply_to_message_id=excluded.reply_to_message_id, text_content=excluded.text_content, normalized_hash=excluded.normalized_hash, has_text=excluded.has_text, has_image=excluded.has_image, message_status=excluded.message_status, occurred_at=excluded.occurred_at, created_at=excluded.created_at`, args
	}
}

func buildMessageGroupDisplayCache(item MessageLog) (messageGroupDisplayCache, bool) {
	connectionID := strings.TrimSpace(item.ConnectionID)
	groupID := strings.TrimSpace(item.GroupID)
	groupName := strings.TrimSpace(item.GroupName)
	if connectionID == "" || groupID == "" || groupName == "" {
		return messageGroupDisplayCache{}, false
	}
	return messageGroupDisplayCache{
		ConnectionID: connectionID,
		GroupID:      groupID,
		GroupName:    groupName,
		UpdatedAt:    messageDisplayUpdatedAt(item),
	}, true
}

func buildMessageUserDisplayCache(item MessageLog) (messageUserDisplayCache, bool) {
	connectionID := strings.TrimSpace(item.ConnectionID)
	chatType := strings.ToLower(strings.TrimSpace(item.ChatType))
	groupID := strings.TrimSpace(item.GroupID)
	userID := strings.TrimSpace(item.UserID)
	senderRole := strings.ToLower(strings.TrimSpace(item.SenderRole))
	displayName := strings.TrimSpace(item.SenderName)
	nickname := strings.TrimSpace(item.SenderNickname)
	if senderRole == "assistant" || connectionID == "" || userID == "" {
		return messageUserDisplayCache{}, false
	}
	if chatType != "group" && chatType != "private" {
		return messageUserDisplayCache{}, false
	}
	if chatType == "group" && groupID == "" {
		return messageUserDisplayCache{}, false
	}
	if displayName == "" {
		displayName = nickname
	}
	if nickname == "" {
		nickname = displayName
	}
	if displayName == "" && nickname == "" {
		return messageUserDisplayCache{}, false
	}
	return messageUserDisplayCache{
		ConnectionID: connectionID,
		ChatType:     chatType,
		GroupID:      groupID,
		UserID:       userID,
		DisplayName:  displayName,
		Nickname:     nickname,
		UpdatedAt:    messageDisplayUpdatedAt(item),
	}, true
}

func messageDisplayUpdatedAt(item MessageLog) time.Time {
	if !item.OccurredAt.IsZero() {
		return item.OccurredAt
	}
	if !item.CreatedAt.IsZero() {
		return item.CreatedAt
	}
	return time.Now()
}

func (s *sqlStore) groupDisplayCacheUpsert(item messageGroupDisplayCache) (string, []any) {
	args := []any{
		item.ConnectionID,
		item.GroupID,
		item.GroupName,
		formatStoredTime(item.UpdatedAt),
	}
	switch s.engine {
	case "postgresql":
		return `INSERT INTO ai_group_display_cache (connection_id, group_id, group_name, updated_at) VALUES ($1,$2,$3,$4) ON CONFLICT (connection_id, group_id) DO UPDATE SET group_name=EXCLUDED.group_name, updated_at=EXCLUDED.updated_at`, args
	case "mysql":
		return `INSERT INTO ai_group_display_cache (connection_id, group_id, group_name, updated_at) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE group_name=VALUES(group_name), updated_at=VALUES(updated_at)`, args
	default:
		return `INSERT INTO ai_group_display_cache (connection_id, group_id, group_name, updated_at) VALUES (?,?,?,?) ON CONFLICT(connection_id, group_id) DO UPDATE SET group_name=excluded.group_name, updated_at=excluded.updated_at`, args
	}
}

func (s *sqlStore) userDisplayCacheUpsert(item messageUserDisplayCache) (string, []any) {
	args := []any{
		item.ConnectionID,
		item.ChatType,
		item.GroupID,
		item.UserID,
		item.DisplayName,
		item.Nickname,
		formatStoredTime(item.UpdatedAt),
	}
	switch s.engine {
	case "postgresql":
		return `INSERT INTO ai_user_display_cache (connection_id, chat_type, group_id, user_id, display_name, nickname, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (connection_id, chat_type, group_id, user_id) DO UPDATE SET display_name=EXCLUDED.display_name, nickname=EXCLUDED.nickname, updated_at=EXCLUDED.updated_at`, args
	case "mysql":
		return `INSERT INTO ai_user_display_cache (connection_id, chat_type, group_id, user_id, display_name, nickname, updated_at) VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE display_name=VALUES(display_name), nickname=VALUES(nickname), updated_at=VALUES(updated_at)`, args
	default:
		return `INSERT INTO ai_user_display_cache (connection_id, chat_type, group_id, user_id, display_name, nickname, updated_at) VALUES (?,?,?,?,?,?,?) ON CONFLICT(connection_id, chat_type, group_id, user_id) DO UPDATE SET display_name=excluded.display_name, nickname=excluded.nickname, updated_at=excluded.updated_at`, args
	}
}

func (s *sqlStore) messageImageUpsert(item MessageImage) (string, []any) {
	args := []any{
		item.ID,
		item.MessageID,
		item.SegmentIndex,
		item.OriginRef,
		item.VisionSummary,
		item.VisionStatus,
		formatStoredTime(item.CreatedAt),
	}
	switch s.engine {
	case "postgresql":
		return `INSERT INTO ai_message_image (id, message_id, segment_index, origin_ref, vision_summary, vision_status, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (message_id, segment_index) DO UPDATE SET origin_ref=EXCLUDED.origin_ref, vision_summary=EXCLUDED.vision_summary, vision_status=EXCLUDED.vision_status, created_at=EXCLUDED.created_at`, args
	case "mysql":
		return `INSERT INTO ai_message_image (id, message_id, segment_index, origin_ref, vision_summary, vision_status, created_at) VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE origin_ref=VALUES(origin_ref), vision_summary=VALUES(vision_summary), vision_status=VALUES(vision_status), created_at=VALUES(created_at)`, args
	default:
		return `INSERT INTO ai_message_image (id, message_id, segment_index, origin_ref, vision_summary, vision_status, created_at) VALUES (?,?,?,?,?,?,?) ON CONFLICT(message_id, segment_index) DO UPDATE SET origin_ref=excluded.origin_ref, vision_summary=excluded.vision_summary, vision_status=excluded.vision_status, created_at=excluded.created_at`, args
	}
}

func (s *sqlStore) messageLogWhereClause(query MessageLogQuery, startIndex int) (string, []any) {
	var clauses []string
	args := make([]any, 0, 6)
	index := startIndex
	addClause := func(clause string, values ...any) {
		clauses = append(clauses, clause)
		args = append(args, values...)
		index += len(values)
	}

	if chatType := strings.TrimSpace(strings.ToLower(query.ChatType)); chatType == "group" || chatType == "private" {
		addClause("l.chat_type = "+s.bindVar(index), chatType)
	}
	if groupID := strings.TrimSpace(query.GroupID); groupID != "" {
		addClause("l.group_id LIKE "+s.bindVar(index), "%"+groupID+"%")
	}
	if userID := strings.TrimSpace(query.UserID); userID != "" {
		addClause("l.user_id LIKE "+s.bindVar(index), "%"+userID+"%")
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		pattern := "%" + keyword + "%"
		clause := fmt.Sprintf("(l.text_content LIKE %s OR l.sender_name LIKE %s OR l.user_id LIKE %s)", s.bindVar(index), s.bindVar(index+1), s.bindVar(index+2))
		addClause(clause, pattern, pattern, pattern)
	}
	return strings.Join(clauses, " AND "), args
}

func (s *sqlStore) bindVar(index int) string {
	if s.engine == "postgresql" {
		return fmt.Sprintf("$%d", index)
	}
	return "?"
}

func normalizeMessageLogQuery(query MessageLogQuery) MessageLogQuery {
	query.ChatType = strings.TrimSpace(strings.ToLower(query.ChatType))
	query.GroupID = strings.TrimSpace(query.GroupID)
	query.UserID = strings.TrimSpace(query.UserID)
	query.Keyword = strings.TrimSpace(query.Keyword)
	if query.Limit <= 0 {
		query.Limit = 30
	} else {
		query.Limit = maxInt(1, minInt(query.Limit, 200))
	}
	if query.ChatType != "" && query.ChatType != "group" && query.ChatType != "private" {
		query.ChatType = ""
	}
	return query
}

func normalizeMessageSuggestionQuery(query MessageSuggestionQuery) MessageSuggestionQuery {
	query.ChatType = strings.TrimSpace(strings.ToLower(query.ChatType))
	query.GroupID = strings.TrimSpace(query.GroupID)
	query.UserID = strings.TrimSpace(query.UserID)
	if query.Limit <= 0 {
		query.Limit = 8
	} else {
		query.Limit = maxInt(1, minInt(query.Limit, 20))
	}
	if query.ChatType != "" && query.ChatType != "group" && query.ChatType != "private" {
		query.ChatType = ""
	}
	return query
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isIgnorableMissingTableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "doesn't exist") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "undefined table")
}
