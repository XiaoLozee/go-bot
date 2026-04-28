package ai

import (
	"sort"
	"strconv"
	"strings"
)

func cloneReflectionStats(stats ReflectionStats) *ReflectionStats {
	if stats == (ReflectionStats{}) {
		return nil
	}
	copied := stats
	return &copied
}

func buildGroupObservations(limit int, sessions []SessionDebugView, candidates []CandidateMemory, longTerms []LongTermMemory, profiles []GroupProfile, relations []RelationEdge) []GroupObservation {
	if limit <= 0 {
		limit = 8
	}

	type aggregate struct {
		observation GroupObservation
	}

	groups := map[string]*aggregate{}
	ensureGroup := func(groupID string) *aggregate {
		key := strings.TrimSpace(groupID)
		if key == "" {
			return nil
		}
		if item, ok := groups[key]; ok && item != nil {
			return item
		}
		item := &aggregate{observation: GroupObservation{GroupID: key}}
		groups[key] = item
		return item
	}

	for _, session := range sessions {
		groupID := strings.TrimSpace(session.GroupID)
		if groupID == "" {
			continue
		}
		group := ensureGroup(groupID)
		if group == nil {
			continue
		}
		if session.UpdatedAt.After(group.observation.UpdatedAt) {
			group.observation.UpdatedAt = session.UpdatedAt
		}
		if group.observation.TopicSummary == "" {
			group.observation.TopicSummary = strings.TrimSpace(session.TopicSummary)
		}
		if session.RecentCount > group.observation.SessionMessageCount {
			group.observation.SessionMessageCount = session.RecentCount
		}
		group.observation.ActiveUsers = mergeOrderedUnique(group.observation.ActiveUsers, session.ActiveUsers, 8)
	}

	for _, profile := range profiles {
		groupID := strings.TrimSpace(profile.GroupID)
		if groupID == "" {
			continue
		}
		group := ensureGroup(groupID)
		if group == nil {
			continue
		}
		group.observation.StyleTags = append([]string(nil), profile.StyleTags...)
		group.observation.TopicFocus = append([]string(nil), profile.TopicFocus...)
		group.observation.ActiveMemes = append([]string(nil), profile.ActiveMemes...)
		if summary := strings.TrimSpace(profile.ReflectionSummary); summary != "" {
			group.observation.Summary = summary
		}
		if profile.UpdatedAt.After(group.observation.UpdatedAt) {
			group.observation.UpdatedAt = profile.UpdatedAt
		}
		if group.observation.TopicSummary == "" && len(profile.TopicFocus) > 0 {
			group.observation.TopicSummary = strings.Join(profile.TopicFocus[:minInt(3, len(profile.TopicFocus))], "、")
		}
	}

	for _, memory := range candidates {
		groupID := strings.TrimSpace(memory.GroupID)
		if groupID == "" {
			continue
		}
		group := ensureGroup(groupID)
		if group == nil {
			continue
		}
		group.observation.CandidateHighlights = append(group.observation.CandidateHighlights, cloneCandidateMemory(memory))
		if memory.LastSeenAt.After(group.observation.UpdatedAt) {
			group.observation.UpdatedAt = memory.LastSeenAt
		}
	}

	for _, memory := range longTerms {
		groupID := strings.TrimSpace(memory.GroupID)
		if groupID == "" {
			continue
		}
		group := ensureGroup(groupID)
		if group == nil {
			continue
		}
		group.observation.LongTermHighlights = append(group.observation.LongTermHighlights, cloneLongTermMemory(memory))
		if memory.UpdatedAt.After(group.observation.UpdatedAt) {
			group.observation.UpdatedAt = memory.UpdatedAt
		}
	}

	for _, edge := range relations {
		groupID := strings.TrimSpace(edge.GroupID)
		if groupID == "" {
			continue
		}
		group := ensureGroup(groupID)
		if group == nil {
			continue
		}
		group.observation.RelationHighlights = append(group.observation.RelationHighlights, edge)
		if edge.LastInteractionAt.After(group.observation.UpdatedAt) {
			group.observation.UpdatedAt = edge.LastInteractionAt
		}
	}

	out := make([]GroupObservation, 0, len(groups))
	for _, item := range groups {
		if item == nil {
			continue
		}
		obs := item.observation
		sort.Slice(obs.CandidateHighlights, func(i, j int) bool {
			left := obs.CandidateHighlights[i]
			right := obs.CandidateHighlights[j]
			if candidateObservationPriority(left.Status) != candidateObservationPriority(right.Status) {
				return candidateObservationPriority(left.Status) < candidateObservationPriority(right.Status)
			}
			if !left.LastSeenAt.Equal(right.LastSeenAt) {
				return left.LastSeenAt.After(right.LastSeenAt)
			}
			return left.EvidenceCount > right.EvidenceCount
		})
		sort.Slice(obs.LongTermHighlights, func(i, j int) bool {
			return obs.LongTermHighlights[i].UpdatedAt.After(obs.LongTermHighlights[j].UpdatedAt)
		})
		sort.Slice(obs.RelationHighlights, func(i, j int) bool {
			if obs.RelationHighlights[i].Strength != obs.RelationHighlights[j].Strength {
				return obs.RelationHighlights[i].Strength > obs.RelationHighlights[j].Strength
			}
			return obs.RelationHighlights[i].LastInteractionAt.After(obs.RelationHighlights[j].LastInteractionAt)
		})
		if len(obs.CandidateHighlights) > 4 {
			obs.CandidateHighlights = obs.CandidateHighlights[:4]
		}
		if len(obs.LongTermHighlights) > 3 {
			obs.LongTermHighlights = obs.LongTermHighlights[:3]
		}
		if len(obs.RelationHighlights) > 3 {
			obs.RelationHighlights = obs.RelationHighlights[:3]
		}

		obs.RiskFlags = buildGroupObservationRiskFlags(obs)
		if strings.TrimSpace(obs.Summary) == "" {
			obs.Summary = buildGroupObservationSummary(obs)
		}
		out = append(out, obs)
	}

	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		leftScore := len(out[i].CandidateHighlights) + len(out[i].LongTermHighlights) + out[i].SessionMessageCount
		rightScore := len(out[j].CandidateHighlights) + len(out[j].LongTermHighlights) + out[j].SessionMessageCount
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		return out[i].GroupID < out[j].GroupID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func buildGroupObservationSummary(obs GroupObservation) string {
	parts := make([]string, 0, 4)
	if obs.TopicSummary != "" {
		parts = append(parts, "最近热点 "+obs.TopicSummary)
	} else if len(obs.TopicFocus) > 0 {
		parts = append(parts, "最近热点 "+strings.Join(obs.TopicFocus[:minInt(3, len(obs.TopicFocus))], "、"))
	}
	if len(obs.StyleTags) > 0 {
		parts = append(parts, "群风格偏"+strings.Join(obs.StyleTags[:minInt(2, len(obs.StyleTags))], " / "))
	}
	if len(obs.ActiveMemes) > 0 {
		parts = append(parts, "高频群梗 "+strings.Join(obs.ActiveMemes[:minInt(2, len(obs.ActiveMemes))], "、"))
	}
	if len(parts) == 0 && obs.SessionMessageCount > 0 {
		parts = append(parts, "最近窗口记录 "+strconv.Itoa(obs.SessionMessageCount)+" 条消息")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "。") + "。"
}

func buildGroupObservationRiskFlags(obs GroupObservation) []string {
	flags := make([]string, 0, 4)
	conflictCount := 0
	warmingMemes := 0
	coolingCount := 0
	for _, item := range obs.CandidateHighlights {
		switch strings.ToLower(strings.TrimSpace(item.Status)) {
		case "conflict":
			conflictCount++
		case "warming":
			if isGroupMemeCandidate(item) {
				warmingMemes++
			}
		case "cooling":
			coolingCount++
		}
	}
	if conflictCount > 0 {
		flags = append(flags, "存在冲突候选记忆，默认不直接用于回复")
	}
	if warmingMemes > 0 {
		flags = append(flags, "群梗仍在观察期，建议低权重使用")
	}
	if coolingCount > 0 {
		flags = append(flags, "部分记忆或群梗正在降温，建议减少主动引用")
	}
	if obs.SessionMessageCount < reflectionMinGroupMessages && strings.TrimSpace(obs.Summary) == "" {
		flags = append(flags, "群画像仍在形成中，需要更多样本")
	}
	return flags
}

func candidateObservationPriority(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "conflict":
		return 0
	case "stable":
		return 1
	case "warming":
		return 2
	case "observed":
		return 3
	case "cooling":
		return 4
	case "pending":
		return 5
	default:
		return 6
	}
}

func cloneCandidateMemory(item CandidateMemory) CandidateMemory {
	item.SourceMsgIDs = append([]string(nil), item.SourceMsgIDs...)
	return item
}

func cloneLongTermMemory(item LongTermMemory) LongTermMemory {
	item.SourceRefs = append([]string(nil), item.SourceRefs...)
	return item
}
