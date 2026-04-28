package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
)

const (
	relationAnalysisMaxUsers    = 48
	relationAnalysisMaxEdges    = 128
	relationAnalysisMaxMemories = 80
	relationAnalysisMaxTokens   = 1800
	relationAnalysisTimeoutMS   = 120000
	relationAnalysisCacheTTL    = 30 * time.Minute
	relationAnalysisMaxCache    = 16
)

type RelationAnalysisResult struct {
	GroupID     string    `json:"group_id,omitempty"`
	Markdown    string    `json:"markdown"`
	GeneratedAt time.Time `json:"generated_at"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	UserCount   int       `json:"user_count"`
	EdgeCount   int       `json:"edge_count"`
	MemoryCount int       `json:"memory_count"`
	InputHash   string    `json:"input_hash,omitempty"`
	CacheHit    bool      `json:"cache_hit"`
}

type relationAnalysisGroup struct {
	GroupID           string   `json:"group_id"`
	StyleTags         []string `json:"style_tags,omitempty"`
	TopicFocus        []string `json:"topic_focus,omitempty"`
	ActiveMemes       []string `json:"active_memes,omitempty"`
	SoftRules         []string `json:"soft_rules,omitempty"`
	HardRules         []string `json:"hard_rules,omitempty"`
	ReflectionSummary string   `json:"reflection_summary,omitempty"`
	HumorDensity      float64  `json:"humor_density"`
	EmojiRate         float64  `json:"emoji_rate"`
	Formality         float64  `json:"formality"`
}

type relationAnalysisUser struct {
	GroupID          string   `json:"group_id,omitempty"`
	UserID           string   `json:"user_id"`
	DisplayName      string   `json:"display_name,omitempty"`
	TopicPreferences []string `json:"topic_preferences,omitempty"`
	StyleTags        []string `json:"style_tags,omitempty"`
	TabooTopics      []string `json:"taboo_topics,omitempty"`
	InteractionLevel int      `json:"interaction_level_with_bot"`
	TeasingTolerance float64  `json:"teasing_tolerance"`
	TrustScore       float64  `json:"trust_score"`
	LastActiveAt     string   `json:"last_active_at,omitempty"`
}

type relationAnalysisEdge struct {
	GroupID       string  `json:"group_id,omitempty"`
	NodeA         string  `json:"node_a"`
	NodeB         string  `json:"node_b"`
	RelationType  string  `json:"relation_type"`
	RelationLabel string  `json:"relation_label"`
	Strength      float64 `json:"strength"`
	EvidenceCount int     `json:"evidence_count"`
	LastAt        string  `json:"last_interaction_at,omitempty"`
}

type relationAnalysisMemberMetric struct {
	GroupID            string         `json:"group_id,omitempty"`
	UserID             string         `json:"user_id"`
	DisplayName        string         `json:"display_name,omitempty"`
	Degree             int            `json:"degree"`
	TotalStrength      float64        `json:"total_strength"`
	EvidenceCount      int            `json:"evidence_count"`
	RelationTypeCounts map[string]int `json:"relation_type_counts,omitempty"`
	TopPeers           []string       `json:"top_peers,omitempty"`
}

type relationAnalysisSummary struct {
	RelationTypeCounts map[string]int                 `json:"relation_type_counts,omitempty"`
	TopMembers         []relationAnalysisMemberMetric `json:"top_members,omitempty"`
	StrongestEdges     []relationAnalysisEdge         `json:"strongest_edges,omitempty"`
	GroupUserCounts    map[string]int                 `json:"group_user_counts,omitempty"`
	GroupEdgeCounts    map[string]int                 `json:"group_edge_counts,omitempty"`
}

type relationAnalysisMemory struct {
	GroupID       string  `json:"group_id,omitempty"`
	SubjectID     string  `json:"subject_id,omitempty"`
	MemoryType    string  `json:"memory_type"`
	Subtype       string  `json:"subtype,omitempty"`
	Content       string  `json:"content"`
	Confidence    float64 `json:"confidence"`
	EvidenceCount int     `json:"evidence_count"`
}

type relationAnalysisInput struct {
	GroupID  string                   `json:"group_id,omitempty"`
	Groups   []relationAnalysisGroup  `json:"groups,omitempty"`
	Users    []relationAnalysisUser   `json:"users"`
	Edges    []relationAnalysisEdge   `json:"relation_edges"`
	Summary  relationAnalysisSummary  `json:"summary"`
	Memories []relationAnalysisMemory `json:"long_term_memories,omitempty"`
}

func (s *Service) GenerateRelationAnalysis(ctx context.Context, groupID string, force bool) (RelationAnalysisResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	groupID = strings.TrimSpace(groupID)
	cfg, gen, input, err := s.relationAnalysisInput(groupID)
	if err != nil {
		return RelationAnalysisResult{}, err
	}
	if gen == nil {
		return RelationAnalysisResult{}, fmt.Errorf("未配置可用的 AI 服务商")
	}
	payload, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return RelationAnalysisResult{}, fmt.Errorf("关系分析数据编码失败: %w", err)
	}
	inputHash := relationAnalysisInputHash(payload)
	now := time.Now()
	if !force {
		if cached, ok := s.cachedRelationAnalysis(groupID, inputHash, now); ok {
			cached.CacheHit = true
			return cached, nil
		}
	}
	cfg = relationAnalysisRuntimeConfig(cfg)
	analysisGen, err := s.relationAnalysisGenerator(gen, cfg)
	if err != nil {
		return RelationAnalysisResult{}, err
	}
	result, err := analysisGen.RunTurn(ctx, buildRelationAnalysisMessages(string(payload)), nil, cfg)
	if err != nil {
		return RelationAnalysisResult{}, relationAnalysisError(err)
	}
	markdown := strings.TrimSpace(result.Text)
	if markdown == "" {
		return RelationAnalysisResult{}, fmt.Errorf("AI 关系分析返回为空")
	}
	output := RelationAnalysisResult{
		GroupID:     groupID,
		Markdown:    markdown + "\n",
		GeneratedAt: now,
		ExpiresAt:   now.Add(relationAnalysisCacheTTL),
		UserCount:   len(input.Users),
		EdgeCount:   len(input.Edges),
		MemoryCount: len(input.Memories),
		InputHash:   inputHash,
	}
	s.storeRelationAnalysis(groupID, output)
	return output, nil
}

func relationAnalysisRuntimeConfig(cfg config.AIConfig) config.AIConfig {
	cfg.Provider.TimeoutMS = maxInt(cfg.Provider.TimeoutMS, relationAnalysisTimeoutMS)
	cfg.Reply.MaxOutputTokens = maxInt(cfg.Reply.MaxOutputTokens, relationAnalysisMaxTokens)
	return cfg
}

func (s *Service) relationAnalysisGenerator(current generator, cfg config.AIConfig) (generator, error) {
	if current == nil {
		return nil, fmt.Errorf("未配置可用的 AI 服务商")
	}
	if _, ok := current.(*openAICompatibleClient); !ok {
		return current, nil
	}
	gen, err := newGenerator(cfg)
	if err != nil {
		return nil, err
	}
	if gen == nil {
		return nil, fmt.Errorf("未配置可用的 AI 服务商")
	}
	return gen, nil
}

func relationAnalysisError(err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	if strings.Contains(text, "context deadline exceeded") || strings.Contains(text, "Client.Timeout") {
		return fmt.Errorf("AI 关系分析超时: %w；本次分析输入较大或模型响应较慢，已为关系分析使用更长超时，可稍后重试或减少分析范围", err)
	}
	return err
}

func (s *Service) relationAnalysisInput(groupID string) (config.AIConfig, generator, relationAnalysisInput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cfg := s.cfg
	if !cfg.Enabled {
		return cfg, nil, relationAnalysisInput{}, fmt.Errorf("AI 未启用，无法执行关系分析")
	}
	if !cfg.Memory.Enabled {
		return cfg, nil, relationAnalysisInput{}, fmt.Errorf("AI 记忆未启用，无法执行关系分析")
	}

	groups := make([]relationAnalysisGroup, 0, len(s.groupProfiles))
	for _, profile := range s.groupProfiles {
		if profile == nil {
			continue
		}
		if groupID != "" && strings.TrimSpace(profile.GroupID) != groupID {
			continue
		}
		groups = append(groups, relationAnalysisGroup{
			GroupID:           strings.TrimSpace(profile.GroupID),
			StyleTags:         append([]string(nil), profile.StyleTags...),
			TopicFocus:        append([]string(nil), profile.TopicFocus...),
			ActiveMemes:       append([]string(nil), profile.ActiveMemes...),
			SoftRules:         append([]string(nil), profile.SoftRules...),
			HardRules:         append([]string(nil), profile.HardRules...),
			ReflectionSummary: strings.TrimSpace(profile.ReflectionSummary),
			HumorDensity:      profile.HumorDensity,
			EmojiRate:         profile.EmojiRate,
			Formality:         profile.Formality,
		})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].GroupID < groups[j].GroupID })

	users := make([]relationAnalysisUser, 0, len(s.userProfiles))
	for _, profile := range s.userProfiles {
		if profile == nil {
			continue
		}
		if groupID != "" && strings.TrimSpace(profile.GroupID) != groupID {
			continue
		}
		users = append(users, relationAnalysisUser{
			GroupID:          strings.TrimSpace(profile.GroupID),
			UserID:           strings.TrimSpace(profile.UserID),
			DisplayName:      strings.TrimSpace(profile.DisplayName),
			TopicPreferences: append([]string(nil), profile.TopicPreferences...),
			StyleTags:        append([]string(nil), profile.StyleTags...),
			TabooTopics:      append([]string(nil), profile.TabooTopics...),
			InteractionLevel: profile.InteractionLevelWithBot,
			TeasingTolerance: profile.TeasingTolerance,
			TrustScore:       profile.TrustScore,
			LastActiveAt:     formatAnalysisTime(profile.LastActiveAt),
		})
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].GroupID != users[j].GroupID {
			return users[i].GroupID < users[j].GroupID
		}
		return users[i].UserID < users[j].UserID
	})
	if len(users) > relationAnalysisMaxUsers {
		users = users[:relationAnalysisMaxUsers]
	}

	edges := make([]relationAnalysisEdge, 0, len(s.relationEdges))
	for _, edge := range s.relationEdges {
		if edge == nil {
			continue
		}
		if groupID != "" && strings.TrimSpace(edge.GroupID) != groupID {
			continue
		}
		edges = append(edges, relationAnalysisEdge{
			GroupID:       strings.TrimSpace(edge.GroupID),
			NodeA:         strings.TrimSpace(edge.NodeA),
			NodeB:         strings.TrimSpace(edge.NodeB),
			RelationType:  strings.TrimSpace(edge.RelationType),
			RelationLabel: relationTypePromptLabel(edge.RelationType),
			Strength:      edge.Strength,
			EvidenceCount: edge.EvidenceCount,
			LastAt:        formatAnalysisTime(edge.LastInteractionAt),
		})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Strength != edges[j].Strength {
			return edges[i].Strength > edges[j].Strength
		}
		if edges[i].EvidenceCount != edges[j].EvidenceCount {
			return edges[i].EvidenceCount > edges[j].EvidenceCount
		}
		return edges[i].LastAt > edges[j].LastAt
	})
	if len(edges) > relationAnalysisMaxEdges {
		edges = edges[:relationAnalysisMaxEdges]
	}

	memories := make([]relationAnalysisMemory, 0, len(s.longTermMemories))
	for _, memory := range s.longTermMemories {
		if memory == nil {
			continue
		}
		if groupID != "" && strings.TrimSpace(memory.GroupID) != "" && strings.TrimSpace(memory.GroupID) != groupID {
			continue
		}
		if !relationAnalysisMemoryIsRelevant(*memory) {
			continue
		}
		memories = append(memories, relationAnalysisMemory{
			GroupID:       strings.TrimSpace(memory.GroupID),
			SubjectID:     strings.TrimSpace(memory.SubjectID),
			MemoryType:    strings.TrimSpace(memory.MemoryType),
			Subtype:       strings.TrimSpace(memory.Subtype),
			Content:       strings.TrimSpace(memory.Content),
			Confidence:    memory.Confidence,
			EvidenceCount: memory.EvidenceCount,
		})
	}
	sort.Slice(memories, func(i, j int) bool {
		if memories[i].Confidence != memories[j].Confidence {
			return memories[i].Confidence > memories[j].Confidence
		}
		return memories[i].EvidenceCount > memories[j].EvidenceCount
	})
	if len(memories) > relationAnalysisMaxMemories {
		memories = memories[:relationAnalysisMaxMemories]
	}

	if len(users) == 0 && len(edges) == 0 {
		return cfg, s.generator, relationAnalysisInput{}, fmt.Errorf("暂无足够用户画像或关系边可供分析")
	}
	return cfg, s.generator, relationAnalysisInput{
		GroupID:  groupID,
		Groups:   groups,
		Users:    users,
		Edges:    edges,
		Summary:  buildRelationAnalysisSummary(users, edges),
		Memories: memories,
	}, nil
}

func formatAnalysisTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func relationAnalysisMemoryIsRelevant(item LongTermMemory) bool {
	switch strings.TrimSpace(item.MemoryType) {
	case "preference", "profile", "relationship", "topic", "fact":
		return true
	default:
		return memoryIsPositivePreference(item)
	}
}

func buildRelationAnalysisSummary(users []relationAnalysisUser, edges []relationAnalysisEdge) relationAnalysisSummary {
	userLabels := make(map[string]string, len(users))
	groupUserCounts := map[string]int{}
	for _, user := range users {
		key := relationAnalysisUserKey(user.GroupID, user.UserID)
		userLabels[key] = firstNonEmpty(user.DisplayName, user.UserID)
		groupUserCounts[user.GroupID]++
	}

	relationTypeCounts := map[string]int{}
	groupEdgeCounts := map[string]int{}
	metrics := map[string]*relationAnalysisMemberMetric{}
	peerScores := map[string]map[string]float64{}
	for _, edge := range edges {
		relationTypeCounts[edge.RelationType]++
		groupEdgeCounts[edge.GroupID]++
		for _, endpoint := range []struct {
			userID string
			peerID string
		}{
			{userID: edge.NodeA, peerID: edge.NodeB},
			{userID: edge.NodeB, peerID: edge.NodeA},
		} {
			key := relationAnalysisUserKey(edge.GroupID, endpoint.userID)
			metric := metrics[key]
			if metric == nil {
				metric = &relationAnalysisMemberMetric{
					GroupID:            edge.GroupID,
					UserID:             endpoint.userID,
					DisplayName:        userLabels[key],
					RelationTypeCounts: map[string]int{},
				}
				metrics[key] = metric
			}
			metric.Degree++
			metric.TotalStrength += edge.Strength
			metric.EvidenceCount += edge.EvidenceCount
			metric.RelationTypeCounts[edge.RelationType]++

			if peerScores[key] == nil {
				peerScores[key] = map[string]float64{}
			}
			peerKey := relationAnalysisUserKey(edge.GroupID, endpoint.peerID)
			peerScores[key][peerKey] += edge.Strength + float64(edge.EvidenceCount)*0.03
		}
	}

	topMembers := make([]relationAnalysisMemberMetric, 0, len(metrics))
	for key, metric := range metrics {
		metric.TopPeers = relationAnalysisTopPeers(peerScores[key], userLabels, 3)
		topMembers = append(topMembers, *metric)
	}
	sort.Slice(topMembers, func(i, j int) bool {
		if topMembers[i].TotalStrength != topMembers[j].TotalStrength {
			return topMembers[i].TotalStrength > topMembers[j].TotalStrength
		}
		if topMembers[i].EvidenceCount != topMembers[j].EvidenceCount {
			return topMembers[i].EvidenceCount > topMembers[j].EvidenceCount
		}
		return relationAnalysisUserKey(topMembers[i].GroupID, topMembers[i].UserID) < relationAnalysisUserKey(topMembers[j].GroupID, topMembers[j].UserID)
	})
	if len(topMembers) > 12 {
		topMembers = topMembers[:12]
	}

	strongestEdges := append([]relationAnalysisEdge(nil), edges...)
	if len(strongestEdges) > 12 {
		strongestEdges = strongestEdges[:12]
	}

	return relationAnalysisSummary{
		RelationTypeCounts: relationTypeCounts,
		TopMembers:         topMembers,
		StrongestEdges:     strongestEdges,
		GroupUserCounts:    groupUserCounts,
		GroupEdgeCounts:    groupEdgeCounts,
	}
}

func relationAnalysisUserKey(groupID, userID string) string {
	return strings.TrimSpace(groupID) + ":" + strings.TrimSpace(userID)
}

func relationAnalysisTopPeers(scores map[string]float64, labels map[string]string, limit int) []string {
	if len(scores) == 0 || limit <= 0 {
		return nil
	}
	type peerScore struct {
		key   string
		score float64
	}
	items := make([]peerScore, 0, len(scores))
	for key, score := range scores {
		items = append(items, peerScore{key: key, score: score})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].key < items[j].key
	})
	result := make([]string, 0, minInt(limit, len(items)))
	for _, item := range items {
		label := strings.TrimSpace(labels[item.key])
		if label == "" {
			label = item.key
		}
		result = append(result, label)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func relationAnalysisInputHash(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])[:16]
}

func relationAnalysisCacheKey(groupID string) string {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return "__all__"
	}
	return groupID
}

func (s *Service) cachedRelationAnalysis(groupID, inputHash string, now time.Time) (RelationAnalysisResult, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.relationAnalysisCache == nil {
		return RelationAnalysisResult{}, false
	}
	item := s.relationAnalysisCache[relationAnalysisCacheKey(groupID)]
	if item == nil || strings.TrimSpace(item.InputHash) != strings.TrimSpace(inputHash) {
		return RelationAnalysisResult{}, false
	}
	if !item.ExpiresAt.IsZero() && now.After(item.ExpiresAt) {
		return RelationAnalysisResult{}, false
	}
	return *item, true
}

func (s *Service) storeRelationAnalysis(groupID string, result RelationAnalysisResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.relationAnalysisCache == nil {
		s.relationAnalysisCache = make(map[string]*RelationAnalysisResult)
	}
	result.CacheHit = false
	s.relationAnalysisCache[relationAnalysisCacheKey(groupID)] = &result
	if len(s.relationAnalysisCache) <= relationAnalysisMaxCache {
		return
	}
	oldestKey := ""
	var oldest time.Time
	for key, item := range s.relationAnalysisCache {
		if item == nil {
			delete(s.relationAnalysisCache, key)
			continue
		}
		if oldestKey == "" || item.GeneratedAt.Before(oldest) {
			oldestKey = key
			oldest = item.GeneratedAt
		}
	}
	if oldestKey != "" && len(s.relationAnalysisCache) > relationAnalysisMaxCache {
		delete(s.relationAnalysisCache, oldestKey)
	}
}

func buildRelationAnalysisMessages(payload string) []chatMessage {
	return []chatMessage{
		{
			Role: "system",
			Content: strings.Join([]string{
				"You are an analyst for a group-chat memory graph.",
				"Write a concise Simplified Chinese Markdown report.",
				"Use only the provided data. Do not invent private facts.",
				"Separate observed evidence from inference.",
				"Prefer actionable relationship and communication suggestions.",
			}, "\n"),
		},
		{
			Role: "user",
			Content: strings.Join([]string{
				"Analyze the relationship graph and member personalities from this JSON data.",
				"Output Markdown with these sections:",
				"1. # 群友关系与性格分析",
				"2. ## 总览",
				"3. ## 关系网络洞察",
				"4. ## 成员性格画像",
				"5. ## 风险与不确定性",
				"6. ## 互动建议",
				"For every member note the main evidence: style tags, preferences, relation types, strength, evidence_count.",
				"Use summary.top_members and summary.strongest_edges to discuss central members and strong ties.",
				"When evidence is weak, explicitly say it is low-confidence instead of over-claiming.",
				"JSON data:",
				"```json",
				payload,
				"```",
			}, "\n"),
		},
	}
}
