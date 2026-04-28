package ai

import (
	"context"
	"strings"
	"time"
)

func (s *Service) governCandidateMemories(ctx context.Context, now time.Time, threshold int) (ReflectionStats, error) {
	s.mu.RLock()
	items := make([]CandidateMemory, 0, len(s.candidateMemories))
	for _, item := range s.candidateMemories {
		if item == nil {
			continue
		}
		items = append(items, cloneCandidateMemory(*item))
	}
	s.mu.RUnlock()

	if len(items) == 0 {
		return ReflectionStats{}, nil
	}

	conflictBuckets := map[string]map[string]struct{}{}
	for _, item := range items {
		bucket := candidateConflictBucket(item)
		if bucket == "" {
			continue
		}
		content := normalizedCandidateContent(item.Content)
		if content == "" {
			continue
		}
		if _, ok := conflictBuckets[bucket]; !ok {
			conflictBuckets[bucket] = map[string]struct{}{}
		}
		conflictBuckets[bucket][content] = struct{}{}
	}

	var stats ReflectionStats
	for _, item := range items {
		next := applyCandidateGovernance(item, now, threshold, conflictBuckets)
		switch strings.ToLower(strings.TrimSpace(next.Status)) {
		case "conflict":
			stats.ConflictCandidateCount++
		case "stable":
			stats.StableCandidateCount++
		case "cooling":
			stats.CoolingCandidateCount++
		}
		if !candidateGovernanceChanged(item, next) {
			continue
		}
		if err := s.persistCandidateMemory(ctx, next); err != nil {
			return stats, err
		}
		stats.AdjustedCandidateCount++
	}
	return stats, nil
}

func (s *Service) persistCandidateMemory(ctx context.Context, item CandidateMemory) error {
	key := candidateMemoryKey(item)
	s.mu.RLock()
	store := s.store
	s.mu.RUnlock()
	if store != nil {
		if err := store.UpsertCandidateMemory(ctx, item); err != nil {
			return err
		}
	}
	copied := cloneCandidateMemory(item)
	s.mu.Lock()
	s.candidateMemories[key] = &copied
	s.mu.Unlock()
	return nil
}

func applyCandidateGovernance(item CandidateMemory, now time.Time, threshold int, conflictBuckets map[string]map[string]struct{}) CandidateMemory {
	next := cloneCandidateMemory(item)
	if next.CreatedAt.IsZero() {
		next.CreatedAt = now
	}
	if next.LastSeenAt.IsZero() {
		next.LastSeenAt = next.CreatedAt
	}
	if next.LastSeenAt.IsZero() {
		next.LastSeenAt = now
	}
	if strings.EqualFold(strings.TrimSpace(next.Status), "promoted") {
		next.Confidence = clampCandidateConfidence(next.Confidence)
		return next
	}

	if bucket := candidateConflictBucket(next); bucket != "" && len(conflictBuckets[bucket]) > 1 {
		next.Status = "conflict"
		next.Confidence = blendCandidateConfidence(next.Confidence, 0.32)
		return next
	}

	if isGroupMemeCandidate(next) {
		next = applyGroupMemeGovernance(next, now, threshold)
	} else {
		next = applyGenericCandidateGovernance(next, now, threshold)
	}
	next.Confidence = clampCandidateConfidence(next.Confidence)
	return next
}

func applyGroupMemeGovernance(item CandidateMemory, now time.Time, threshold int) CandidateMemory {
	lastSeenAt := item.LastSeenAt
	stableThreshold := maxInt(3, threshold+1)
	switch {
	case now.After(lastSeenAt.Add(10 * 24 * time.Hour)):
		item.Status = "cooling"
		item.Confidence = blendCandidateConfidence(item.Confidence, 0.38)
	case item.EvidenceCount >= stableThreshold:
		item.Status = "stable"
		item.Confidence = blendCandidateConfidence(item.Confidence, 0.79)
	case item.EvidenceCount >= 2:
		item.Status = "warming"
		item.Confidence = blendCandidateConfidence(item.Confidence, 0.66)
	default:
		item.Status = "observed"
		item.Confidence = blendCandidateConfidence(item.Confidence, 0.56)
	}
	return item
}

func applyGenericCandidateGovernance(item CandidateMemory, now time.Time, threshold int) CandidateMemory {
	lastSeenAt := item.LastSeenAt
	switch {
	case now.After(lastSeenAt.Add(21*24*time.Hour)) && item.EvidenceCount < threshold+1:
		item.Status = "cooling"
		item.Confidence = blendCandidateConfidence(item.Confidence, 0.46)
	case item.EvidenceCount >= threshold && item.Confidence >= 0.68:
		item.Status = "stable"
		item.Confidence = blendCandidateConfidence(item.Confidence, 0.82)
	case item.EvidenceCount >= maxInt(1, threshold-1):
		item.Status = "observed"
		item.Confidence = blendCandidateConfidence(item.Confidence, 0.62)
	default:
		item.Status = "pending"
		item.Confidence = blendCandidateConfidence(item.Confidence, 0.48)
	}
	return item
}

func shouldPromoteCandidate(item *CandidateMemory, threshold int, now time.Time) bool {
	if item == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(item.Status))
	if status == "promoted" || status == "conflict" || status == "cooling" {
		return false
	}
	lastSeenAt := item.LastSeenAt
	if lastSeenAt.IsZero() {
		lastSeenAt = item.CreatedAt
	}
	if isGroupMemeCandidate(*item) {
		if item.EvidenceCount < maxInt(3, threshold+1) || item.Confidence < 0.72 {
			return false
		}
		if !lastSeenAt.IsZero() && now.After(lastSeenAt.Add(5*24*time.Hour)) {
			return false
		}
		return status == "stable"
	}
	if item.EvidenceCount < threshold || item.Confidence < 0.68 {
		return false
	}
	if status == "stable" {
		return true
	}
	return status == "observed" && item.EvidenceCount >= threshold+1
}

func candidateEligibleForPrompt(item CandidateMemory, threshold int, now time.Time) bool {
	status := strings.ToLower(strings.TrimSpace(item.Status))
	if status == "promoted" || status == "conflict" || status == "cooling" || status == "pending" {
		return false
	}
	lastSeenAt := item.LastSeenAt
	if lastSeenAt.IsZero() {
		lastSeenAt = item.CreatedAt
	}
	if isGroupMemeCandidate(item) {
		if status != "stable" {
			return false
		}
		if item.EvidenceCount < maxInt(3, threshold+1) || item.Confidence < 0.72 {
			return false
		}
		return lastSeenAt.IsZero() || !now.After(lastSeenAt.Add(5*24*time.Hour))
	}
	if item.Confidence < 0.58 {
		return false
	}
	if status == "stable" {
		return item.EvidenceCount >= threshold
	}
	return status == "observed" && item.EvidenceCount >= maxInt(2, threshold-1)
}

func candidatePromptPriority(item CandidateMemory) int {
	switch strings.ToLower(strings.TrimSpace(item.Status)) {
	case "stable":
		return 0
	case "observed":
		return 1
	case "warming":
		return 2
	case "cooling":
		return 3
	case "conflict":
		return 4
	case "pending":
		return 5
	default:
		return 6
	}
}

func candidateConflictBucket(item CandidateMemory) string {
	if isGroupMemeCandidate(item) {
		return ""
	}
	subtype := strings.TrimSpace(item.Subtype)
	if subtype == "" || strings.TrimSpace(item.SubjectID) == "" {
		return ""
	}
	return strings.Join([]string{
		strings.TrimSpace(item.Scope),
		strings.TrimSpace(item.GroupID),
		strings.TrimSpace(item.SubjectID),
		strings.TrimSpace(item.MemoryType),
		subtype,
	}, "|")
}

func normalizedCandidateContent(content string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(content)), " "))
}

func isGroupMemeCandidate(item CandidateMemory) bool {
	return strings.EqualFold(strings.TrimSpace(item.MemoryType), "group_meme") || strings.EqualFold(strings.TrimSpace(item.Subtype), "group_meme") || strings.EqualFold(strings.TrimSpace(item.Subtype), "meme")
}

func clampCandidateConfidence(value float64) float64 {
	switch {
	case value < 0.05:
		return 0.05
	case value > 0.98:
		return 0.98
	default:
		return value
	}
}

func blendCandidateConfidence(current, target float64) float64 {
	if current <= 0 {
		return clampCandidateConfidence(target)
	}
	return clampCandidateConfidence(current*0.65 + target*0.35)
}

func candidateGovernanceChanged(left, right CandidateMemory) bool {
	if strings.TrimSpace(left.Status) != strings.TrimSpace(right.Status) {
		return true
	}
	const epsilon = 0.0001
	diff := left.Confidence - right.Confidence
	if diff < 0 {
		diff = -diff
	}
	return diff > epsilon
}
