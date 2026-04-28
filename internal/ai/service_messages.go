package ai

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
)

func (s *Service) messageStore(ctx context.Context) (Store, error) {
	return s.ensureStore(ctx)
}

func (s *Service) ImportRecentEvents(ctx context.Context, events []event.Event) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}

	store, err := s.ensureStore(ctx)
	if err != nil {
		return 0, err
	}

	s.mu.RLock()
	cfg := s.cfg
	window := maxInt(cfg.Memory.SessionWindow, cfg.Reply.MaxContextMsgs, 8)
	botName := strings.TrimSpace(firstNonEmpty(cfg.Prompt.BotName, "Go-bot"))
	s.mu.RUnlock()

	ordered := append([]event.Event(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return eventTimestampOrNow(ordered[i].Timestamp).Before(eventTimestampOrNow(ordered[j].Timestamp))
	})

	imported := 0
	errs := make([]error, 0)
	for _, evt := range ordered {
		if evt.Kind != "message" || (evt.ChatType != "group" && evt.ChatType != "private") {
			continue
		}
		text := cleanEventText(evt)
		if strings.TrimSpace(text) == "" && !eventHasImage(evt) {
			continue
		}

		messageID := ensureMessageID("user", evt.MessageID)
		raw := RawMessageLog{
			MessageID:        messageID,
			ConnectionID:     evt.ConnectionID,
			ChatType:         evt.ChatType,
			GroupID:          evt.GroupID,
			UserID:           evt.UserID,
			ContentText:      text,
			NormalizedHash:   hashNormalizedText(text),
			ReplyToMessageID: extractReplyReference(evt),
			CreatedAt:        eventTimestampOrNow(evt.Timestamp),
		}
		if err := store.AppendRawMessage(ctx, raw); err != nil {
			errs = append(errs, err)
			s.logger.Warn("同步 AI 原始消息日志失败", "error", err, "message_id", messageID)
		}

		messageLog := buildSyncedMessageLog(evt, text, botName)
		if err := store.AppendMessageLog(ctx, messageLog); err != nil {
			errs = append(errs, err)
			s.logger.Warn("同步 AI 聊天消息失败", "error", err, "message_id", messageID)
		}
		if images := buildInboundMessageImages(evt, "", nil); len(images) > 0 {
			if err := store.AppendMessageImages(ctx, images); err != nil {
				errs = append(errs, err)
				s.logger.Warn("同步 AI 聊天图片失败", "error", err, "message_id", messageID)
			}
		}

		session := s.importRecentSessionMessage(evt, text, botName, window)
		if session.Scope != "" {
			if err := store.SaveSession(ctx, session); err != nil {
				errs = append(errs, err)
				s.logger.Warn("同步 AI 会话状态失败", "error", err, "scope", session.Scope)
			}
		}
		imported++
	}
	return imported, errors.Join(errs...)
}

func (s *Service) importRecentSessionMessage(evt event.Event, text, botName string, window int) SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()

	scopeKey := buildScopeKey(evt)
	session := s.ensureSessionLocked(scopeKey, evt.GroupID)
	role := "user"
	userID := evt.UserID
	userName := eventSenderName(evt)
	if isAssistantEvent(evt) {
		role = "assistant"
		userName = botName
	}
	s.upsertConversationLocked(session, ConversationMessage{
		Role:      role,
		UserID:    userID,
		UserName:  userName,
		Text:      text,
		MessageID: evt.MessageID,
		At:        eventTimestampOrNow(evt.Timestamp),
	}, window)
	return cloneSession(*session)
}

func buildSyncedMessageLog(evt event.Event, text, botName string) MessageLog {
	item := buildInboundMessageLog(evt, text)
	if isAssistantEvent(evt) {
		item.SenderRole = "assistant"
		item.SenderName = strings.TrimSpace(botName)
		item.SenderNickname = strings.TrimSpace(botName)
	}
	return item
}

func isAssistantEvent(evt event.Event) bool {
	selfID := strings.TrimSpace(evt.Meta["self_id"])
	return selfID != "" && strings.TrimSpace(evt.UserID) == selfID
}

func (s *Service) ListMessageLogs(ctx context.Context, query MessageLogQuery) ([]MessageLog, error) {
	store, err := s.messageStore(ctx)
	if err != nil {
		return nil, err
	}
	return store.ListMessageLogs(ctx, query)
}

func (s *Service) ListMessageSearchSuggestions(ctx context.Context, query MessageSuggestionQuery) (MessageSearchSuggestions, error) {
	store, err := s.messageStore(ctx)
	if err != nil {
		return MessageSearchSuggestions{}, err
	}
	return store.ListMessageSearchSuggestions(ctx, query)
}

func (s *Service) RememberMessageDisplay(ctx context.Context, item MessageLog) error {
	store, err := s.messageStore(ctx)
	if err != nil {
		return err
	}
	return store.UpsertMessageDisplayHints(ctx, item)
}

func (s *Service) GetMessageDetail(ctx context.Context, messageID string) (MessageDetail, error) {
	store, err := s.messageStore(ctx)
	if err != nil {
		return MessageDetail{}, err
	}
	detail, err := store.GetMessageDetail(ctx, messageID)
	if err != nil {
		return MessageDetail{}, err
	}
	for i := range detail.Images {
		if s.canPreviewMessageImage(detail.Images[i]) {
			detail.Images[i].PreviewURL = buildMessageImagePreviewURL(detail.Message.MessageID, detail.Images[i].SegmentIndex)
		}
	}
	return detail, nil
}

func (s *Service) ResolveMessageImagePreview(ctx context.Context, messageID string, segmentIndex int) (MessageImagePreview, error) {
	detail, err := s.GetMessageDetail(ctx, messageID)
	if err != nil {
		return MessageImagePreview{}, err
	}
	for _, image := range detail.Images {
		if image.SegmentIndex != segmentIndex {
			continue
		}
		preview := MessageImagePreview{
			MessageID:    messageID,
			SegmentIndex: segmentIndex,
			MimeType:     image.MimeType,
		}
		if redirectURL := strings.TrimSpace(image.PublicURL); isPreviewRemoteURL(redirectURL) {
			preview.RedirectURL = redirectURL
			return preview, nil
		}
		if localPath, ok := s.resolveLocalPreviewPath(image); ok {
			preview.LocalPath = localPath
			return preview, nil
		}
		return MessageImagePreview{}, fmt.Errorf("图片资源尚未可预览")
	}
	return MessageImagePreview{}, fmt.Errorf("图片记录不存在: %s#%d", messageID, segmentIndex)
}

func buildMessageImagePreviewURL(messageID string, segmentIndex int) string {
	return "/api/admin/ai/messages/" + url.PathEscape(strings.TrimSpace(messageID)) + "/images/" + fmt.Sprintf("%d", segmentIndex) + "/content"
}

func buildInboundMessageLog(evt event.Event, text string) MessageLog {
	occurredAt := eventTimestampOrNow(evt.Timestamp)
	text = strings.TrimSpace(text)
	return MessageLog{
		MessageID:        ensureMessageID("user", evt.MessageID),
		ConnectionID:     evt.ConnectionID,
		ChatType:         evt.ChatType,
		GroupID:          evt.GroupID,
		GroupName:        eventGroupName(evt),
		UserID:           evt.UserID,
		SenderRole:       "user",
		SenderName:       eventSenderName(evt),
		SenderNickname:   eventSenderNickname(evt),
		ReplyToMessageID: extractReplyReference(evt),
		TextContent:      text,
		NormalizedHash:   hashNormalizedText(text),
		HasText:          text != "",
		HasImage:         eventHasImage(evt),
		MessageStatus:    "normal",
		OccurredAt:       occurredAt,
		CreatedAt:        occurredAt,
	}
}

func buildAssistantMessageLog(evt event.Event, messageID, response, senderName string) MessageLog {
	now := time.Now()
	response = strings.TrimSpace(response)
	return MessageLog{
		MessageID:        ensureMessageID("assistant", messageID),
		ConnectionID:     evt.ConnectionID,
		ChatType:         evt.ChatType,
		GroupID:          evt.GroupID,
		GroupName:        eventGroupName(evt),
		UserID:           strings.TrimSpace(evt.Meta["self_id"]),
		SenderRole:       "assistant",
		SenderName:       strings.TrimSpace(senderName),
		SenderNickname:   strings.TrimSpace(senderName),
		ReplyToMessageID: strings.TrimSpace(evt.MessageID),
		TextContent:      response,
		NormalizedHash:   hashNormalizedText(response),
		HasText:          response != "",
		HasImage:         false,
		MessageStatus:    "normal",
		OccurredAt:       now,
		CreatedAt:        now,
	}
}

func buildInboundMessageImages(evt event.Event, visionSummary string, visionErr error) []MessageImage {
	messageID := ensureMessageID("user", evt.MessageID)
	now := eventTimestampOrNow(evt.Timestamp)
	items := make([]MessageImage, 0, 2)
	summary := normalizeVisionSummaryForStorage(visionSummary)
	status := "skipped"
	if summary != "" {
		status = "ready"
	} else if visionErr != nil {
		status = "failed"
	}
	for index, seg := range evt.Segments {
		if strings.TrimSpace(seg.Type) != "image" {
			continue
		}
		originRef := firstNonEmpty(
			segmentString(seg.Data, "file"),
			segmentString(seg.Data, "url"),
			segmentString(seg.Data, "path"),
		)
		items = append(items, MessageImage{
			ID:            fmt.Sprintf("%s#%02d", messageID, index),
			MessageID:     messageID,
			SegmentIndex:  index,
			OriginRef:     strings.TrimSpace(originRef),
			VisionSummary: summary,
			VisionStatus:  status,
			CreatedAt:     now,
		})
	}
	return items
}

func eventSenderName(evt event.Event) string {
	return firstNonEmpty(
		strings.TrimSpace(evt.Meta["sender_card"]),
		strings.TrimSpace(evt.Meta["sender_nickname"]),
		strings.TrimSpace(evt.Meta["nickname"]),
		strings.TrimSpace(evt.UserID),
	)
}

func eventSenderNickname(evt event.Event) string {
	return firstNonEmpty(
		strings.TrimSpace(evt.Meta["sender_nickname"]),
		strings.TrimSpace(evt.Meta["nickname"]),
	)
}

func eventGroupName(evt event.Event) string {
	return strings.TrimSpace(evt.Meta["group_name"])
}

func extractReplyReference(evt event.Event) string {
	for _, seg := range evt.Segments {
		if strings.TrimSpace(seg.Type) != "reply" {
			continue
		}
		if replyTo := strings.TrimSpace(firstNonEmpty(segmentString(seg.Data, "id"), segmentString(seg.Data, "message_id"))); replyTo != "" {
			return replyTo
		}
	}
	return ""
}

func eventHasImage(evt event.Event) bool {
	for _, seg := range evt.Segments {
		if strings.TrimSpace(seg.Type) == "image" {
			return true
		}
	}
	return false
}

func normalizeVisionSummaryForStorage(summary string) string {
	summary = strings.TrimSpace(summary)
	summary = strings.TrimPrefix(summary, "图片识别：")
	return strings.TrimSpace(summary)
}

func (s *Service) canPreviewMessageImage(image MessageImage) bool {
	if redirectURL := strings.TrimSpace(image.PublicURL); isPreviewRemoteURL(redirectURL) {
		return true
	}
	_, ok := s.resolveLocalPreviewPath(image)
	return ok
}

func (s *Service) resolveLocalPreviewPath(image MessageImage) (string, bool) {
	if localPath, ok := normalizePreviewLocalPath(image.PublicURL); ok && s.isWithinLocalMediaDir(localPath) {
		return localPath, true
	}
	if strings.EqualFold(strings.TrimSpace(image.StorageBackend), "local") {
		mediaDir := strings.TrimSpace(s.storageCfg.Media.Local.Dir)
		storageKey := strings.TrimSpace(image.StorageKey)
		if mediaDir != "" && storageKey != "" {
			candidate := filepath.Join(mediaDir, filepath.FromSlash(storageKey))
			if s.isWithinLocalMediaDir(candidate) {
				return candidate, true
			}
		}
	}
	return "", false
}

func normalizePreviewLocalPath(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "file://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "", false
		}
		pathValue, err := url.PathUnescape(parsed.Path)
		if err != nil {
			pathValue = parsed.Path
		}
		if strings.TrimSpace(pathValue) == "" {
			return "", false
		}
		return filepath.Clean(pathValue), true
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw), true
	}
	return "", false
}

func (s *Service) isWithinLocalMediaDir(candidate string) bool {
	baseDir := strings.TrimSpace(s.storageCfg.Media.Local.Dir)
	if baseDir == "" || strings.TrimSpace(candidate) == "" {
		return false
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return false
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absBase, absCandidate)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func isPreviewRemoteURL(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}
