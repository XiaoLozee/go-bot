package ai

import (
	"strconv"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/domain/event"
)

func (s *Service) shouldJoinAmbientChatLocked(cfg config.AIConfig, evt event.Event, text string, session *SessionState, scopeKey string, cooldownActive bool) bool {
	proactive := config.NormalizeAIProactiveConfig(cfg.Proactive)
	if !proactive.Enabled {
		return false
	}
	if strings.TrimSpace(evt.ChatType) != "group" || !cfg.Reply.EnabledInGroup {
		return false
	}
	if cooldownActive || session == nil {
		return false
	}
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return false
	}
	if containsSensitiveConflict(trimmedText) || looksLikeQuestion(trimmedText) {
		return false
	}

	now := eventTimestampOrNow(evt.Timestamp)
	if inProactiveQuietHours(now, proactive.QuietHours) {
		return false
	}

	window := time.Duration(proactive.RecentWindowSeconds) * time.Second
	if recentGroupUserMessageCount(session, now, window) < proactive.MinRecentMessages {
		return false
	}

	if lastAt := s.proactiveLastAtByScope[scopeKey]; !lastAt.IsZero() {
		minInterval := time.Duration(proactive.MinIntervalSeconds) * time.Second
		if now.Sub(lastAt) < minInterval {
			return false
		}
	}
	if s.proactiveCountByScopeDay[proactiveDayKey(scopeKey, now)] >= proactive.DailyLimitPerGroup {
		return false
	}

	seed := strings.Join([]string{
		scopeKey,
		strings.TrimSpace(evt.MessageID),
		strings.TrimSpace(evt.UserID),
		now.Format(time.RFC3339Nano),
		trimmedText,
	}, "|")
	return deterministicProbability(seed) < proactive.Probability
}

func (s *Service) markProactiveAcceptedLocked(scopeKey string, now time.Time) {
	s.proactiveLastAtByScope[scopeKey] = now
	s.proactiveCountByScopeDay[proactiveDayKey(scopeKey, now)]++
}

func proactiveDayKey(scopeKey string, now time.Time) string {
	return scopeKey + "|" + now.Format("2006-01-02")
}

func recentGroupUserMessageCount(session *SessionState, now time.Time, window time.Duration) int {
	if session == nil || window <= 0 {
		return 0
	}
	count := 0
	cutoff := now.Add(-window)
	for _, item := range session.Recent {
		if item.Role != "user" || strings.TrimSpace(item.Text) == "" {
			continue
		}
		at := item.At
		if at.IsZero() {
			at = now
		}
		if at.Before(cutoff) || at.After(now.Add(time.Second)) {
			continue
		}
		count++
	}
	return count
}

func deterministicProbability(seed string) float64 {
	hash := hashNormalizedText(seed)
	if len(hash) < 8 {
		return 1
	}
	value, err := strconv.ParseUint(hash[:8], 16, 32)
	if err != nil {
		return 1
	}
	return float64(value) / float64(uint64(1)<<32)
}

func inProactiveQuietHours(now time.Time, quietHours []string) bool {
	current := now.Hour()*60 + now.Minute()
	for _, item := range quietHours {
		start, end, ok := parseProactiveQuietHourRange(item)
		if !ok || start == end {
			continue
		}
		if start < end {
			if current >= start && current < end {
				return true
			}
			continue
		}
		if current >= start || current < end {
			return true
		}
	}
	return false
}

func parseProactiveQuietHourRange(value string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	start, ok := parseProactiveClockMinute(parts[0])
	if !ok {
		return 0, 0, false
	}
	end, ok := parseProactiveClockMinute(parts[1])
	if !ok {
		return 0, 0, false
	}
	return start, end, true
}

func parseProactiveClockMinute(value string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, false
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, false
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}
