package ai

import (
	"context"
	"sort"
	"strings"
	"time"
)

const (
	reflectionRawMessageLimit   = 768
	reflectionPerGroupMsgWindow = 36
	reflectionMinGroupMessages  = 4
	reflectionPerGroupMemeLimit = 3
)

func (s *Service) reflectGroupProfiles(ctx context.Context) (int, error) {
	sessions, err := s.loadReflectionSessions(ctx)
	if err != nil {
		return 0, err
	}
	return s.reflectGroupProfilesFromSessions(ctx, sessions)
}

func (s *Service) reflectGroupProfilesFromSessions(ctx context.Context, sessions []SessionState) (int, error) {
	if len(sessions) == 0 {
		return 0, nil
	}

	updated := 0
	for _, session := range sessions {
		if ctx != nil && ctx.Err() != nil {
			return updated, ctx.Err()
		}
		if len(collectSessionTexts(session.Recent, "")) < reflectionMinGroupMessages {
			continue
		}
		profile := GroupProfile{GroupID: strings.TrimSpace(session.GroupID)}
		applyGroupProfile(&profile, &session)
		if profile.GroupID == "" {
			continue
		}

		merged := s.mergeReflectedGroupProfile(profile)
		s.mu.RLock()
		store := s.store
		s.mu.RUnlock()
		if store != nil {
			if err := store.UpsertGroupProfile(ctx, merged); err != nil {
				return updated, err
			}
		}

		copied := merged
		s.mu.Lock()
		s.groupProfiles[merged.GroupID] = &copied
		s.mu.Unlock()
		updated++
	}
	return updated, nil
}

func (s *Service) reflectGroupMemeCandidates(ctx context.Context) (int, error) {
	sessions, err := s.loadReflectionSessions(ctx)
	if err != nil {
		return 0, err
	}
	return s.reflectGroupMemeCandidatesFromSessions(ctx, sessions)
}

func (s *Service) reflectGroupMemeCandidatesFromSessions(ctx context.Context, sessions []SessionState) (int, error) {
	if len(sessions) == 0 {
		return 0, nil
	}
	updated := 0
	for _, session := range sessions {
		if ctx != nil && ctx.Err() != nil {
			return updated, ctx.Err()
		}
		groupID := strings.TrimSpace(session.GroupID)
		if groupID == "" {
			continue
		}
		for _, stat := range extractReflectionMemeStats(session.Recent, reflectionPerGroupMemeLimit) {
			item := buildReflectedGroupMemeCandidate(groupID, stat, session.UpdatedAt)
			if item.ID == "" {
				continue
			}
			if err := s.upsertReflectedCandidateMemory(ctx, item); err != nil {
				return updated, err
			}
			updated++
		}
	}
	return updated, nil
}

func (s *Service) loadReflectionSessions(ctx context.Context) ([]SessionState, error) {
	s.mu.RLock()
	store := s.store
	rawLimit := s.cfg.Memory.ReflectionRawLimit
	perGroupLimit := s.cfg.Memory.ReflectionPerGroupLimit
	s.mu.RUnlock()
	if rawLimit <= 0 {
		rawLimit = reflectionRawMessageLimit
	}
	if perGroupLimit <= 0 {
		perGroupLimit = reflectionPerGroupMsgWindow
	}

	if store != nil {
		items, err := store.LoadRecentRawMessages(ctx, rawLimit)
		if err != nil {
			return nil, err
		}
		if len(items) > 0 {
			return buildReflectionSessionsFromRawMessages(items, perGroupLimit), nil
		}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]SessionState, 0, len(s.sessions))
	for _, session := range s.sessions {
		if session == nil || strings.TrimSpace(session.GroupID) == "" {
			continue
		}
		items = append(items, cloneSession(*session))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return items, nil
}

func buildReflectionSessionsFromRawMessages(items []RawMessageLog, perGroupLimit int) []SessionState {
	if perGroupLimit <= 0 {
		perGroupLimit = reflectionPerGroupMsgWindow
	}

	grouped := make(map[string][]ConversationMessage)
	activeUsers := make(map[string][]string)
	updatedAt := make(map[string]time.Time)

	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		groupID := strings.TrimSpace(item.GroupID)
		userID := strings.TrimSpace(item.UserID)
		text := sanitizeMemoryText(item.ContentText)
		if groupID == "" || userID == "" || text == "" {
			continue
		}
		if len(grouped[groupID]) >= perGroupLimit {
			continue
		}
		grouped[groupID] = append(grouped[groupID], ConversationMessage{
			Role:      "user",
			UserID:    userID,
			Text:      text,
			MessageID: item.MessageID,
			At:        item.CreatedAt,
		})
		activeUsers[groupID] = appendUnique(activeUsers[groupID], userID, 12)
		if item.CreatedAt.After(updatedAt[groupID]) {
			updatedAt[groupID] = item.CreatedAt
		}
	}

	out := make([]SessionState, 0, len(grouped))
	for groupID, recent := range grouped {
		if len(recent) == 0 {
			continue
		}
		texts := collectSessionTexts(recent, "")
		summary := strings.Join(topKeywords(texts, 3), " / ")
		out = append(out, SessionState{
			Scope:        buildSyntheticGroupScope(groupID),
			GroupID:      groupID,
			Recent:       recent,
			TopicSummary: summary,
			ActiveUsers:  append([]string(nil), activeUsers[groupID]...),
			UpdatedAt:    updatedAt[groupID],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

func (s *Service) mergeReflectedGroupProfile(next GroupProfile) GroupProfile {
	s.mu.RLock()
	current := s.groupProfiles[strings.TrimSpace(next.GroupID)]
	s.mu.RUnlock()
	if current == nil {
		next.UpdatedAt = time.Now()
		return next
	}

	merged := next
	merged.StyleTags = mergeOrderedUnique(next.StyleTags, current.StyleTags, 6)
	merged.TopicFocus = mergeOrderedUnique(next.TopicFocus, current.TopicFocus, 8)
	merged.ActiveMemes = mergeOrderedUnique(next.ActiveMemes, current.ActiveMemes, 6)
	merged.SoftRules = mergeOrderedUnique(next.SoftRules, current.SoftRules, 6)
	merged.HardRules = mergeOrderedUnique(next.HardRules, current.HardRules, 6)
	merged.HumorDensity = smoothReflectionMetric(current.HumorDensity, next.HumorDensity)
	merged.EmojiRate = smoothReflectionMetric(current.EmojiRate, next.EmojiRate)
	merged.Formality = smoothReflectionMetric(current.Formality, next.Formality)
	merged.ReflectionSummary = buildGroupReflectionSummary(merged)
	merged.UpdatedAt = time.Now()
	return merged
}

func smoothReflectionMetric(previous, current float64) float64 {
	if previous <= 0 {
		return current
	}
	if current <= 0 {
		return previous * 0.82
	}
	value := previous*0.35 + current*0.65
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func buildSyntheticGroupScope(groupID string) string {
	return "group:" + strings.TrimSpace(groupID)
}

type reflectionMemeStat struct {
	Text       string
	Count      int
	MessageIDs []string
}

func extractReflectionMemeStats(items []ConversationMessage, limit int) []reflectionMemeStat {
	if limit <= 0 {
		return nil
	}
	counts := map[string]int{}
	messageIDs := map[string][]string{}
	for _, item := range items {
		if item.Role != "user" {
			continue
		}
		text := normalizeMemeCandidate(sanitizeMemoryText(item.Text))
		if !isUsefulMemeCandidate(text) {
			continue
		}
		counts[text]++
		if msgID := strings.TrimSpace(item.MessageID); msgID != "" {
			messageIDs[text] = appendUnique(messageIDs[text], msgID, 8)
		}
	}
	stats := make([]reflectionMemeStat, 0, len(counts))
	for text, count := range counts {
		if count < 2 {
			continue
		}
		stats = append(stats, reflectionMemeStat{
			Text:       text,
			Count:      count,
			MessageIDs: append([]string(nil), messageIDs[text]...),
		})
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Count == stats[j].Count {
			return stats[i].Text < stats[j].Text
		}
		return stats[i].Count > stats[j].Count
	})
	if len(stats) > limit {
		stats = stats[:limit]
	}
	return stats
}

func buildReflectedGroupMemeCandidate(groupID string, stat reflectionMemeStat, lastSeenAt time.Time) CandidateMemory {
	text := strings.TrimSpace(stat.Text)
	groupID = strings.TrimSpace(groupID)
	if groupID == "" || text == "" {
		return CandidateMemory{}
	}
	if lastSeenAt.IsZero() {
		lastSeenAt = time.Now()
	}
	content := "群梗：" + text
	confidence := 0.45 + float64(stat.Count)*0.12
	if confidence > 0.95 {
		confidence = 0.95
	}
	return CandidateMemory{
		ID:            "candidate-group-meme-" + hashNormalizedText(groupID+"|"+text),
		Scope:         "group",
		MemoryType:    "group_meme",
		Subtype:       "meme",
		GroupID:       groupID,
		Content:       content,
		Confidence:    confidence,
		EvidenceCount: stat.Count,
		SourceMsgIDs:  append([]string(nil), stat.MessageIDs...),
		Status:        "observed",
		TTLDays:       21,
		CreatedAt:     lastSeenAt,
		LastSeenAt:    lastSeenAt,
	}
}

func (s *Service) upsertReflectedCandidateMemory(ctx context.Context, item CandidateMemory) error {
	key := candidateMemoryKey(item)
	now := item.LastSeenAt
	if now.IsZero() {
		now = time.Now()
		item.LastSeenAt = now
	}

	s.mu.RLock()
	existing := s.candidateMemories[key]
	store := s.store
	threshold := 2
	if s.cfg.Memory.PromoteThreshold >= 2 {
		threshold = s.cfg.Memory.PromoteThreshold
	}
	s.mu.RUnlock()

	if existing != nil {
		merged := *existing
		merged.MemoryType = firstNonEmpty(strings.TrimSpace(merged.MemoryType), strings.TrimSpace(item.MemoryType))
		merged.Subtype = firstNonEmpty(strings.TrimSpace(merged.Subtype), strings.TrimSpace(item.Subtype))
		merged.GroupID = firstNonEmpty(strings.TrimSpace(merged.GroupID), strings.TrimSpace(item.GroupID))
		merged.Content = firstNonEmpty(strings.TrimSpace(merged.Content), strings.TrimSpace(item.Content))
		if item.Confidence > merged.Confidence {
			merged.Confidence = item.Confidence
		}
		merged.EvidenceCount = maxInt(merged.EvidenceCount, item.EvidenceCount)
		merged.SourceMsgIDs = appendManyUnique(merged.SourceMsgIDs, item.SourceMsgIDs, 8)
		merged.TTLDays = maxInt(merged.TTLDays, item.TTLDays)
		merged.LastSeenAt = now
		if merged.CreatedAt.IsZero() {
			merged.CreatedAt = item.CreatedAt
		}
		item = merged
	}
	if isGroupMemeCandidate(item) {
		item = applyGroupMemeGovernance(item, now, threshold)
		item.Confidence = clampCandidateConfidence(item.Confidence)
	}

	if store != nil {
		if err := store.UpsertCandidateMemory(ctx, item); err != nil {
			return err
		}
	}
	copied := item
	s.mu.Lock()
	s.candidateMemories[key] = &copied
	s.mu.Unlock()
	return nil
}
