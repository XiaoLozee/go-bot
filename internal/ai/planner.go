package ai

import (
	"strings"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
)

func enrichReplyPlan(plan ReplyPlan, evt event.Event, text string, groupProfile *GroupProfile, userProfile *UserInGroupProfile, memories []LongTermMemory) ReplyPlan {
	if len(memories) == 0 {
		plan.UseMemory = false
	} else {
		plan.UseMemory = true
		plan.MemoryRefs = make([]string, 0, minInt(3, len(memories)))
		for _, item := range memories {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			plan.MemoryRefs = append(plan.MemoryRefs, item.ID)
			if len(plan.MemoryRefs) >= 3 {
				break
			}
		}
	}

	if groupProfile != nil {
		switch {
		case groupProfile.Formality >= 0.55:
			plan.Tone = "polite"
			if plan.ReplyMode == "direct_answer" {
				plan.ResponseGoal = "回答要清晰、礼貌、克制"
			}
		case groupProfile.HumorDensity >= 0.34 && plan.ReplyMode != "utility" && plan.ReplyMode != "reject_or_defuse" && plan.ReplyMode != "ambient_chat":
			plan.ReplyMode = "banter"
			plan.Tone = "light_teasing"
			plan.Length = "short"
			plan.ResponseGoal = "顺着群里的语气自然接一句，不抢主话题"
		}
		if len(groupProfile.ActiveMemes) > 0 && plan.ReplyMode == "banter" {
			plan.ResponseGoal = "可以轻微借用群梗，但不要刻意复读"
		}
	}

	if userProfile != nil {
		if userProfile.TrustScore >= 0.52 && plan.ReplyMode == "utility" {
			plan.Tone = "warm_helpful"
		}
		if userProfile.TeasingTolerance >= 0.48 && groupProfile != nil && groupProfile.HumorDensity >= 0.28 && plan.ReplyMode != "utility" {
			plan.Tone = "light_teasing"
			plan.Length = "short"
		}
		if len(userProfile.TopicPreferences) > 0 && !looksLikeQuestion(text) && plan.ReplyMode == "direct_answer" {
			plan.ResponseGoal = "若合适可轻度结合成员偏好，让回复更像群聊"
		}
	}

	if containsSensitiveConflict(text) {
		plan.RiskLevel = "high"
	}
	if evt.ChatType == "private" && plan.ReplyMode == "direct_answer" {
		plan.Length = "medium"
	}
	return plan
}
