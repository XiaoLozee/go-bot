package ai

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
)

var (
	profileTokenPattern   = regexp.MustCompile(`[\p{Han}A-Za-z0-9_]{2,24}`)
	profileRulePatterns   = []string{"别刷屏", "不要剧透", "不要发广告", "记得改名", "先看公告", "文明聊天", "不要开盒", "别开车"}
	profileHardRuleTokens = []string{"禁言", "违规", "封禁", "踢出", "广告", "政治", "色情", "辱骂"}
	profileStopwords      = map[string]struct{}{
		"什么": {}, "怎么": {}, "这个": {}, "那个": {}, "就是": {}, "然后": {}, "因为": {}, "所以": {},
		"我们": {}, "你们": {}, "他们": {}, "已经": {}, "一下": {}, "一下子": {}, "今天": {}, "现在": {},
		"真的": {}, "感觉": {}, "可以": {}, "还是": {}, "一个": {}, "不是": {}, "不会": {}, "自己": {},
		"机器人": {}, "群友": {}, "这里": {}, "那个群": {}, "这个群": {}, "你在吗": {}, "收到": {},
	}
	relationSignalStopwords = map[string]struct{}{
		"我也": {}, "我喜": {}, "也喜": {}, "喜欢": {}, "也在": {}, "在打": {}, "正在": {}, "想要": {},
		"需要": {}, "这个": {}, "那个": {}, "活动": {}, "怎么": {}, "什么": {}, "一下": {}, "今天": {},
		"现在": {}, "真的": {}, "感觉": {}, "可以": {}, "还是": {}, "一个": {}, "不是": {}, "不会": {},
		"哈哈": {}, "笑死": {}, "太草": {}, "这个梗": {}, "也太": {}, "梗太": {},
	}
)

func (s *Service) updateProfilesLocked(evt event.Event, session *SessionState) {
	if session == nil || strings.TrimSpace(evt.GroupID) == "" || strings.TrimSpace(evt.UserID) == "" {
		return
	}

	groupProfile := s.ensureGroupProfileLocked(evt.GroupID)
	applyGroupProfile(groupProfile, session)

	userProfile := s.ensureUserProfileLocked(evt.GroupID, evt.UserID)
	applyUserProfile(userProfile, evt, session, s.userTopicPreferencesLocked(evt.GroupID, evt.UserID))

	s.updateRelationEdgesLocked(evt, session)
}

func (s *Service) ensureGroupProfileLocked(groupID string) *GroupProfile {
	key := strings.TrimSpace(groupID)
	if item, ok := s.groupProfiles[key]; ok && item != nil {
		return item
	}
	item := &GroupProfile{GroupID: key}
	s.groupProfiles[key] = item
	return item
}

func (s *Service) ensureUserProfileLocked(groupID, userID string) *UserInGroupProfile {
	key := userProfileKey(groupID, userID)
	if item, ok := s.userProfiles[key]; ok && item != nil {
		return item
	}
	item := &UserInGroupProfile{
		GroupID: strings.TrimSpace(groupID),
		UserID:  strings.TrimSpace(userID),
	}
	s.userProfiles[key] = item
	return item
}

func (s *Service) ensureRelationEdgeLocked(groupID, nodeA, nodeB, relationType string) *RelationEdge {
	left, right := normalizeRelationNodes(nodeA, nodeB)
	key := relationEdgeKey(groupID, left, right, relationType)
	if item, ok := s.relationEdges[key]; ok && item != nil {
		return item
	}
	item := &RelationEdge{
		ID:           key,
		GroupID:      strings.TrimSpace(groupID),
		NodeA:        left,
		NodeB:        right,
		RelationType: strings.TrimSpace(relationType),
	}
	s.relationEdges[key] = item
	return item
}

func applyGroupProfile(profile *GroupProfile, session *SessionState) {
	if profile == nil || session == nil {
		return
	}
	texts := collectSessionTexts(session.Recent, "")
	profile.StyleTags = inferStyleTags(texts, len(session.ActiveUsers), len(session.Recent))
	profile.TopicFocus = topKeywords(texts, 5)
	profile.ActiveMemes = extractActiveMemes(session.Recent, 4)
	profile.SoftRules, profile.HardRules = extractRuleCandidates(session.Recent)
	profile.HumorDensity = clampRatio(countHumorTexts(texts), len(texts))
	profile.EmojiRate = clampRatio(countEmojiTexts(texts), len(texts))
	profile.Formality = clampRatio(countFormalTexts(texts), len(texts))
	profile.ReflectionSummary = buildGroupReflectionSummary(*profile)
	profile.UpdatedAt = time.Now()
}

func applyUserProfile(profile *UserInGroupProfile, evt event.Event, session *SessionState, memoryPreferences []string) {
	if profile == nil || session == nil {
		return
	}
	selfID := strings.TrimSpace(evt.Meta["self_id"])
	texts := collectSessionTexts(session.Recent, evt.UserID)
	profile.DisplayName = firstNonEmpty(
		strings.TrimSpace(evt.Meta["sender_card"]),
		strings.TrimSpace(evt.Meta["sender_nickname"]),
		strings.TrimSpace(evt.Meta["nickname"]),
		profile.DisplayName,
		evt.UserID,
	)
	profile.Nicknames = appendUnique(profile.Nicknames, profile.DisplayName, 4)
	profile.TopicPreferences = mergeOrderedUnique(memoryPreferences, topKeywords(texts, 4), 6)
	profile.StyleTags = inferStyleTags(texts, 0, len(texts))
	profile.TabooTopics = mergeOrderedUnique(profile.TabooTopics, extractRuleTopics(texts), 4)
	profile.LastActiveAt = eventTimestampOrNow(evt.Timestamp)
	profile.InteractionLevelWithBot = estimateInteractionLevel(profile.InteractionLevelWithBot, evt, selfID)
	profile.TeasingTolerance = estimateTeasingTolerance(texts)
	profile.TrustScore = estimateTrustScore(profile.InteractionLevelWithBot, len(profile.TopicPreferences), profile.TeasingTolerance)
	profile.UpdatedAt = time.Now()
}

func (s *Service) userTopicPreferencesLocked(groupID, userID string) []string {
	items := make([]string, 0, 6)
	for _, item := range s.longTermMemories {
		if item == nil || strings.TrimSpace(item.SubjectID) != strings.TrimSpace(userID) {
			continue
		}
		if !memoryIsPositivePreference(*item) {
			continue
		}
		if strings.TrimSpace(item.GroupID) != "" && strings.TrimSpace(item.GroupID) != strings.TrimSpace(groupID) {
			continue
		}
		preference := memoryPreferenceText(item.Content)
		if preference == "" {
			preference = strings.TrimSpace(item.Content)
		}
		items = appendUnique(items, preference, 6)
	}
	return items
}

func (s *Service) updateRelationEdgesLocked(evt event.Event, session *SessionState) {
	if session == nil || strings.TrimSpace(evt.GroupID) == "" || strings.TrimSpace(evt.UserID) == "" {
		return
	}
	now := eventTimestampOrNow(evt.Timestamp)
	currentText := cleanEventText(evt)
	for _, peerID := range extractMentionedPeerIDs(evt) {
		if peerID == "" || peerID == evt.UserID {
			continue
		}
		s.incrementRelationEdgeLocked(evt.GroupID, evt.UserID, peerID, "mention", 0.18, now)
	}

	if replyTo := extractReplyReference(evt); replyTo != "" {
		if peerID := findRepliedUserIDFromSession(session, replyTo, evt.UserID); peerID != "" {
			s.incrementRelationEdgeLocked(evt.GroupID, evt.UserID, peerID, "reply", 0.22, now)
		}
	}

	for _, item := range recentPeerMessages(session, evt.UserID, now, 20*time.Minute, 3) {
		s.incrementRelationEdgeLocked(evt.GroupID, evt.UserID, item.UserID, "conversation", 0.12, now)
	}

	currentTopicSignals := relationTopicSignals(currentText)
	if len(currentTopicSignals) > 0 {
		matched := 0
		for _, item := range recentPeerMessages(session, evt.UserID, now, 20*time.Minute, 8) {
			if sharedKeywordCount(currentTopicSignals, relationTopicSignals(item.Text)) == 0 {
				continue
			}
			s.incrementRelationEdgeLocked(evt.GroupID, evt.UserID, item.UserID, "co_topic", 0.10, now)
			matched++
			if matched >= 3 {
				break
			}
		}
	}

	if countHumorTexts([]string{currentText}) > 0 {
		matched := 0
		for _, item := range recentPeerMessages(session, evt.UserID, now, 10*time.Minute, 6) {
			if countHumorTexts([]string{item.Text}) == 0 {
				continue
			}
			s.incrementRelationEdgeLocked(evt.GroupID, evt.UserID, item.UserID, "banter", 0.16, now)
			matched++
			if matched >= 2 {
				break
			}
		}
	}

	for _, match := range s.sharedPreferenceRelationMatchesLocked(evt.GroupID, evt.UserID, 3) {
		s.incrementRelationEdgeLocked(evt.GroupID, evt.UserID, match.userID, "shared_preference", 0.14, now)
	}
}

func (s *Service) incrementRelationEdgeLocked(groupID, left, right, relationType string, base float64, at time.Time) {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if strings.TrimSpace(groupID) == "" || left == "" || right == "" || left == right || strings.TrimSpace(relationType) == "" {
		return
	}
	edge := s.ensureRelationEdgeLocked(groupID, left, right, relationType)
	edge.EvidenceCount++
	edge.Strength = relationStrength(edge.EvidenceCount, base)
	edge.LastInteractionAt = at
}

func findRepliedUserIDFromSession(session *SessionState, replyTo, currentUserID string) string {
	if session == nil {
		return ""
	}
	replyTo = strings.TrimSpace(replyTo)
	currentUserID = strings.TrimSpace(currentUserID)
	if replyTo == "" {
		return ""
	}
	for i := len(session.Recent) - 2; i >= 0; i-- {
		item := session.Recent[i]
		if item.Role != "user" || strings.TrimSpace(item.MessageID) != replyTo {
			continue
		}
		peerID := strings.TrimSpace(item.UserID)
		if peerID == "" || peerID == currentUserID {
			return ""
		}
		return peerID
	}
	return ""
}

func recentPeerMessages(session *SessionState, currentUserID string, now time.Time, maxAge time.Duration, maxPeers int) []ConversationMessage {
	if session == nil || maxPeers <= 0 {
		return nil
	}
	currentUserID = strings.TrimSpace(currentUserID)
	seen := map[string]struct{}{}
	items := make([]ConversationMessage, 0, maxPeers)
	for i := len(session.Recent) - 2; i >= 0 && len(items) < maxPeers; i-- {
		item := session.Recent[i]
		if item.Role != "user" {
			continue
		}
		peerID := strings.TrimSpace(item.UserID)
		if peerID == "" || peerID == currentUserID {
			continue
		}
		if _, ok := seen[peerID]; ok {
			continue
		}
		if maxAge > 0 && !item.At.IsZero() && now.Sub(item.At) > maxAge {
			continue
		}
		item.UserID = peerID
		seen[peerID] = struct{}{}
		items = append(items, item)
	}
	return items
}

func relationTopicSignals(text string) []string {
	text = sanitizeMemoryText(text)
	if text == "" {
		return nil
	}
	signals := make([]string, 0, 16)
	for _, raw := range profileTokenPattern.FindAllString(text, -1) {
		token := normalizeProfileToken(raw)
		if !isUsefulRelationSignal(token) {
			continue
		}
		signals = appendUnique(signals, token, 0)
		for _, gram := range relationHanNgrams(token, 4) {
			signals = appendUnique(signals, gram, 0)
		}
	}
	return signals
}

func relationHanNgrams(value string, maxSize int) []string {
	if maxSize < 2 {
		return nil
	}
	result := []string{}
	run := []rune{}
	flush := func() {
		if len(run) < 2 {
			run = run[:0]
			return
		}
		for size := 2; size <= minInt(maxSize, len(run)); size++ {
			for i := 0; i+size <= len(run); i++ {
				token := string(run[i : i+size])
				if isUsefulRelationSignal(token) {
					result = appendUnique(result, token, 0)
				}
			}
		}
		run = run[:0]
	}
	for _, char := range value {
		if unicode.Is(unicode.Han, char) {
			run = append(run, char)
			continue
		}
		flush()
	}
	flush()
	return result
}

func isUsefulRelationSignal(value string) bool {
	if !isUsefulProfileToken(value) {
		return false
	}
	_, blocked := relationSignalStopwords[value]
	return !blocked
}

func sharedKeywordCount(left, right []string) int {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(left))
	for _, item := range left {
		item = strings.TrimSpace(item)
		if item != "" {
			seen[item] = struct{}{}
		}
	}
	count := 0
	for _, item := range right {
		item = strings.TrimSpace(item)
		if _, ok := seen[item]; ok {
			count++
		}
	}
	return count
}

type sharedPreferenceRelationMatch struct {
	userID       string
	overlap      int
	lastActiveAt time.Time
}

func (s *Service) sharedPreferenceRelationMatchesLocked(groupID, userID string, limit int) []sharedPreferenceRelationMatch {
	if limit <= 0 {
		return nil
	}
	groupID = strings.TrimSpace(groupID)
	userID = strings.TrimSpace(userID)
	current := s.userProfiles[userProfileKey(groupID, userID)]
	if current == nil || len(current.TopicPreferences) == 0 {
		return nil
	}
	matches := make([]sharedPreferenceRelationMatch, 0, limit)
	for _, profile := range s.userProfiles {
		if profile == nil || strings.TrimSpace(profile.GroupID) != groupID || strings.TrimSpace(profile.UserID) == "" || strings.TrimSpace(profile.UserID) == userID {
			continue
		}
		overlap := userPreferenceOverlap(current, profile)
		if overlap == 0 {
			continue
		}
		matches = append(matches, sharedPreferenceRelationMatch{
			userID:       strings.TrimSpace(profile.UserID),
			overlap:      overlap,
			lastActiveAt: profile.LastActiveAt,
		})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].overlap != matches[j].overlap {
			return matches[i].overlap > matches[j].overlap
		}
		if !matches[i].lastActiveAt.Equal(matches[j].lastActiveAt) {
			return matches[i].lastActiveAt.After(matches[j].lastActiveAt)
		}
		return matches[i].userID < matches[j].userID
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches
}

func userPreferenceOverlap(left, right *UserInGroupProfile) int {
	if left == nil || right == nil {
		return 0
	}
	return sharedKeywordCount(relationPreferenceSignals(left.TopicPreferences), relationPreferenceSignals(right.TopicPreferences))
}

func relationPreferenceSignals(items []string) []string {
	signals := []string{}
	for _, item := range items {
		for _, signal := range relationTopicSignals(item) {
			signals = appendUnique(signals, signal, 0)
		}
	}
	return signals
}

func relationTypePromptLabel(value string) string {
	switch strings.TrimSpace(value) {
	case "mention":
		return "提到"
	case "conversation":
		return "常聊"
	case "reply":
		return "回复"
	case "co_topic":
		return "共同话题"
	case "shared_preference":
		return "共同偏好"
	case "banter":
		return "玩笑互动"
	default:
		return strings.TrimSpace(value)
	}
}

func (s *Service) retrieveGroupProfile(evt event.Event) (*GroupProfile, bool) {
	groupID := strings.TrimSpace(evt.GroupID)
	if groupID == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.groupProfiles[groupID]
	if !ok || item == nil {
		return nil, false
	}
	copied := *item
	copied.StyleTags = append([]string(nil), item.StyleTags...)
	copied.TopicFocus = append([]string(nil), item.TopicFocus...)
	copied.ActiveMemes = append([]string(nil), item.ActiveMemes...)
	copied.SoftRules = append([]string(nil), item.SoftRules...)
	copied.HardRules = append([]string(nil), item.HardRules...)
	return &copied, true
}

func (s *Service) retrieveUserProfile(evt event.Event) (*UserInGroupProfile, bool) {
	groupID := strings.TrimSpace(evt.GroupID)
	userID := strings.TrimSpace(evt.UserID)
	if groupID == "" || userID == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.userProfiles[userProfileKey(groupID, userID)]
	if !ok || item == nil {
		return nil, false
	}
	copied := *item
	copied.Nicknames = append([]string(nil), item.Nicknames...)
	copied.TopicPreferences = append([]string(nil), item.TopicPreferences...)
	copied.StyleTags = append([]string(nil), item.StyleTags...)
	copied.TabooTopics = append([]string(nil), item.TabooTopics...)
	return &copied, true
}

func (s *Service) retrieveRelationEdges(evt event.Event) []RelationEdge {
	groupID := strings.TrimSpace(evt.GroupID)
	if groupID == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]RelationEdge, 0, len(s.relationEdges))
	for _, item := range s.relationEdges {
		if item == nil || strings.TrimSpace(item.GroupID) != groupID {
			continue
		}
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].LastInteractionAt.Equal(items[j].LastInteractionAt) {
			if items[i].Strength == items[j].Strength {
				return items[i].ID < items[j].ID
			}
			return items[i].Strength > items[j].Strength
		}
		return items[i].LastInteractionAt.After(items[j].LastInteractionAt)
	})
	if len(items) > 12 {
		items = items[:12]
	}
	return items
}

func collectSessionTexts(items []ConversationMessage, userID string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item.Role != "user" {
			continue
		}
		if userID != "" && strings.TrimSpace(item.UserID) != strings.TrimSpace(userID) {
			continue
		}
		text := sanitizeMemoryText(item.Text)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

func inferStyleTags(texts []string, activeUsers, recentCount int) []string {
	if len(texts) == 0 {
		return nil
	}
	tags := make([]string, 0, 5)
	if countHumorTexts(texts) >= maxInt(2, len(texts)/3) {
		tags = append(tags, "轻松玩梗")
	}
	if countQuestionTexts(texts) >= maxInt(2, len(texts)/3) {
		tags = append(tags, "问答驱动")
	}
	if countFormalTexts(texts) >= maxInt(2, len(texts)/2) {
		tags = append(tags, "礼貌克制")
	}
	if countShortTexts(texts) >= maxInt(2, len(texts)/2) {
		tags = append(tags, "短句快聊")
	}
	if activeUsers >= 4 || recentCount >= 10 {
		tags = append(tags, "高互动")
	}
	if len(tags) == 0 {
		tags = append(tags, "日常闲聊")
	}
	return tags
}

func topKeywords(texts []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	counts := map[string]int{}
	for _, text := range texts {
		text = sanitizeMemoryText(text)
		if text == "" {
			continue
		}
		for _, raw := range profileTokenPattern.FindAllString(text, -1) {
			token := normalizeProfileToken(raw)
			if !isUsefulProfileToken(token) {
				continue
			}
			counts[token]++
		}
	}
	type keywordStat struct {
		keyword string
		count   int
	}
	items := make([]keywordStat, 0, len(counts))
	for keyword, count := range counts {
		items = append(items, keywordStat{keyword: keyword, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].keyword < items[j].keyword
		}
		return items[i].count > items[j].count
	})
	result := make([]string, 0, minInt(limit, len(items)))
	for _, item := range items {
		result = append(result, item.keyword)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func extractActiveMemes(items []ConversationMessage, limit int) []string {
	if limit <= 0 {
		return nil
	}
	counts := map[string]int{}
	for _, item := range items {
		if item.Role != "user" {
			continue
		}
		text := normalizeMemeCandidate(sanitizeMemoryText(item.Text))
		if !isUsefulMemeCandidate(text) {
			continue
		}
		counts[text]++
	}
	type memeStat struct {
		text  string
		count int
	}
	stats := make([]memeStat, 0, len(counts))
	for text, count := range counts {
		if count < 2 {
			continue
		}
		stats = append(stats, memeStat{text: text, count: count})
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].count == stats[j].count {
			return stats[i].text < stats[j].text
		}
		return stats[i].count > stats[j].count
	})
	result := make([]string, 0, minInt(limit, len(stats)))
	for _, item := range stats {
		result = append(result, item.text)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func extractRuleCandidates(items []ConversationMessage) ([]string, []string) {
	softRules := make([]string, 0, 4)
	hardRules := make([]string, 0, 4)
	for _, item := range items {
		if item.Role != "user" {
			continue
		}
		text := sanitizeMemoryText(item.Text)
		if text == "" {
			continue
		}
		for _, pattern := range profileRulePatterns {
			if strings.Contains(text, pattern) {
				softRules = appendUnique(softRules, pattern, 4)
			}
		}
		for _, token := range profileHardRuleTokens {
			if strings.Contains(text, token) {
				hardRules = appendUnique(hardRules, token, 4)
			}
		}
	}
	return softRules, hardRules
}

func extractRuleTopics(texts []string) []string {
	items := make([]string, 0, 4)
	for _, text := range texts {
		for _, token := range profileHardRuleTokens {
			if strings.Contains(text, token) {
				items = appendUnique(items, token, 4)
			}
		}
	}
	return items
}

func extractMentionedPeerIDs(evt event.Event) []string {
	selfID := strings.TrimSpace(evt.Meta["self_id"])
	items := make([]string, 0, 2)
	for _, seg := range evt.Segments {
		if strings.TrimSpace(seg.Type) != "at" {
			continue
		}
		qq := strings.TrimSpace(segmentString(seg.Data, "qq"))
		if qq == "" || qq == selfID || qq == evt.UserID {
			continue
		}
		items = appendUnique(items, qq, 3)
	}
	return items
}

func estimateInteractionLevel(current int, evt event.Event, selfID string) int {
	next := current
	if hasAtSelf(evt.Segments, selfID) {
		next += 2
	}
	if evt.ChatType == "private" {
		next += 2
	}
	if looksLikeQuestion(cleanEventText(evt)) {
		next++
	}
	if next < 0 {
		next = 0
	}
	if next > 100 {
		next = 100
	}
	return next
}

func estimateTeasingTolerance(texts []string) float64 {
	if len(texts) == 0 {
		return 0.25
	}
	base := 0.2 + clampRatio(countHumorTexts(texts), len(texts))*0.55 + clampRatio(countEmojiTexts(texts), len(texts))*0.2
	if base > 0.95 {
		return 0.95
	}
	return base
}

func estimateTrustScore(interactionLevel, preferenceCount int, teasingTolerance float64) float64 {
	score := 0.18 + minFloat(0.55, float64(interactionLevel)*0.018) + minFloat(0.18, float64(preferenceCount)*0.04) + teasingTolerance*0.14
	if score > 0.98 {
		return 0.98
	}
	return score
}

func relationStrength(evidenceCount int, base float64) float64 {
	if evidenceCount <= 0 {
		return base
	}
	value := base + float64(evidenceCount)*0.11
	if value > 0.99 {
		return 0.99
	}
	return value
}

func normalizeRelationNodes(left, right string) (string, string) {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left <= right {
		return left, right
	}
	return right, left
}

func userProfileKey(groupID, userID string) string {
	return strings.TrimSpace(groupID) + "|" + strings.TrimSpace(userID)
}

func relationEdgeKey(groupID, nodeA, nodeB, relationType string) string {
	return strings.Join([]string{strings.TrimSpace(groupID), strings.TrimSpace(nodeA), strings.TrimSpace(nodeB), strings.TrimSpace(relationType)}, "|")
}

func clampRatio(numerator, denominator int) float64 {
	if numerator <= 0 || denominator <= 0 {
		return 0
	}
	value := float64(numerator) / float64(denominator)
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func countHumorTexts(texts []string) int {
	count := 0
	for _, text := range texts {
		lower := strings.ToLower(text)
		if strings.Contains(lower, "哈哈") || strings.Contains(lower, "233") || strings.Contains(lower, "笑死") || strings.Contains(lower, "草") || strings.Contains(lower, "www") || strings.Contains(lower, "hh") {
			count++
		}
	}
	return count
}

func countQuestionTexts(texts []string) int {
	count := 0
	for _, text := range texts {
		if looksLikeQuestion(text) {
			count++
		}
	}
	return count
}

func countFormalTexts(texts []string) int {
	count := 0
	for _, text := range texts {
		if strings.Contains(text, "请") || strings.Contains(text, "谢谢") || strings.Contains(text, "麻烦") || strings.Contains(text, "您好") {
			count++
		}
	}
	return count
}

func countEmojiTexts(texts []string) int {
	count := 0
	for _, text := range texts {
		if strings.ContainsAny(text, "😀😁😂🤣😅😭🥹🥰😊😉😎🤔🤖❤️✨🎉👍👀") || strings.Contains(text, "[表情]") || strings.Contains(text, "awa") {
			count++
		}
	}
	return count
}

func countShortTexts(texts []string) int {
	count := 0
	for _, text := range texts {
		if len([]rune(strings.TrimSpace(text))) <= 8 {
			count++
		}
	}
	return count
}

func normalizeProfileToken(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	trimmed = strings.Trim(trimmed, "，。！？；：,.!?;:")
	return trimmed
}

func isUsefulProfileToken(token string) bool {
	if token == "" {
		return false
	}
	if _, ok := profileStopwords[token]; ok {
		return false
	}
	if _, err := strconv.Atoi(token); err == nil {
		return false
	}
	if isOpaqueMemoryToken(token) {
		return false
	}
	runeCount := len([]rune(token))
	return runeCount >= 2 && runeCount <= 24
}

func normalizeMemeCandidate(value string) string {
	trimmed := strings.Join(strings.Fields(sanitizeMemoryText(value)), " ")
	trimmed = strings.Trim(trimmed, "，。！？；：,.!?;:\"“”")
	if len([]rune(trimmed)) > 14 {
		runes := []rune(trimmed)
		trimmed = string(runes[:14])
	}
	return trimmed
}

func isUsefulMemeCandidate(value string) bool {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) < 2 || len(runes) > 14 {
		return false
	}
	if looksLikeQuestion(value) {
		return false
	}
	if isOpaqueMemoryToken(value) {
		return false
	}
	return true
}

func memoryPreferenceText(content string) string {
	trimmed := strings.TrimSpace(content)
	trimmed = strings.TrimPrefix(trimmed, "用户喜欢 ")
	trimmed = strings.TrimPrefix(trimmed, "用户喜欢看 ")
	trimmed = strings.TrimPrefix(trimmed, "用户喜欢玩 ")
	trimmed = strings.TrimPrefix(trimmed, "用户偏好 ")
	return strings.TrimSpace(trimmed)
}

func memoryIsPositivePreference(item LongTermMemory) bool {
	subtype := strings.ToLower(strings.TrimSpace(item.Subtype))
	if subtype != "" && subtype != "preference" && subtype != "interest" {
		return false
	}
	content := strings.TrimSpace(item.Content)
	return strings.HasPrefix(content, "用户喜欢 ") ||
		strings.HasPrefix(content, "用户喜欢看 ") ||
		strings.HasPrefix(content, "用户喜欢玩 ") ||
		strings.HasPrefix(content, "用户偏好 ")
}

func mergeOrderedUnique(base, extra []string, maxSize int) []string {
	out := append([]string(nil), base...)
	for _, item := range extra {
		out = appendUnique(out, item, maxSize)
	}
	return out
}

func buildGroupReflectionSummary(profile GroupProfile) string {
	parts := make([]string, 0, 4)
	if len(profile.TopicFocus) > 0 {
		parts = append(parts, "最近常聊 "+strings.Join(profile.TopicFocus[:minInt(3, len(profile.TopicFocus))], "、"))
	}
	if len(profile.StyleTags) > 0 {
		parts = append(parts, "整体风格偏"+strings.Join(profile.StyleTags[:minInt(2, len(profile.StyleTags))], " / "))
	}
	if len(profile.ActiveMemes) > 0 {
		parts = append(parts, "高频群梗 "+strings.Join(profile.ActiveMemes[:minInt(2, len(profile.ActiveMemes))], "、"))
	}
	ruleItems := make([]string, 0, 2)
	if len(profile.SoftRules) > 0 {
		ruleItems = append(ruleItems, strings.Join(profile.SoftRules[:minInt(2, len(profile.SoftRules))], "、"))
	}
	if len(profile.HardRules) > 0 {
		ruleItems = append(ruleItems, strings.Join(profile.HardRules[:minInt(2, len(profile.HardRules))], "、"))
	}
	if len(ruleItems) > 0 {
		parts = append(parts, "注意事项 "+strings.Join(ruleItems, "；"))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "。") + "。"
}
