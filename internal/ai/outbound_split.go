package ai

import (
	"context"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/config"
)

func splitOutboundMessages(response string, plan ReplyPlan, cfg config.AIConfig) []string {
	response = strings.TrimSpace(response)
	if response == "" {
		return nil
	}

	splitCfg := config.NormalizeAIReplySplitConfig(cfg.Reply.Split)
	if !splitCfg.Enabled || splitCfg.MaxParts <= 1 {
		return []string{response}
	}
	if splitCfg.OnlyCasual && !isCasualReplyMode(plan.ReplyMode) {
		return []string{response}
	}
	if strings.Contains(response, "[CQ:") || runeLen(response) <= splitCfg.MaxChars {
		return []string{response}
	}

	parts := splitNaturalReply(response)
	if len(parts) <= 1 {
		parts = chunkByRunes(response, splitCfg.MaxChars)
	}
	out := packReplyParts(parts, splitCfg.MaxChars, splitCfg.MaxParts)
	if len(out) <= 1 {
		return []string{response}
	}
	return out
}

func sleepBeforeSplitMessage(ctx context.Context, delayMS int) error {
	if delayMS <= 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(delayMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isCasualReplyMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case "banter", "ambient_chat", "casual_chat":
		return true
	default:
		return false
	}
}

func splitNaturalReply(text string) []string {
	var parts []string
	var builder strings.Builder
	pendingSentenceEnd := false
	for _, r := range text {
		if r == '\r' {
			continue
		}
		if r == '\n' {
			appendSplitPart(&parts, &builder)
			if len(parts) > 0 {
				builder.WriteRune('\n')
			}
			pendingSentenceEnd = false
			continue
		}
		if pendingSentenceEnd && !isSentenceTailRune(r) {
			appendSplitPart(&parts, &builder)
			pendingSentenceEnd = false
		}
		builder.WriteRune(r)
		if isSentenceTerminator(r) {
			pendingSentenceEnd = true
		}
	}
	appendSplitPart(&parts, &builder)
	return mergeLoosePunctuation(parts)
}

func appendSplitPart(parts *[]string, builder *strings.Builder) {
	part := trimHorizontalSpace(builder.String())
	builder.Reset()
	if strings.TrimSpace(part) != "" {
		*parts = append(*parts, part)
	}
}

func isSentenceTerminator(r rune) bool {
	switch r {
	case '。', '！', '？', '!', '?', '…':
		return true
	default:
		return false
	}
}

func isSentenceTailRune(r rune) bool {
	if isSentenceTerminator(r) {
		return true
	}
	switch r {
	case '"', '\'', '”', '’', ')', '）', ']', '】', '}', '》', '」', '』':
		return true
	default:
		return false
	}
}

func mergeLoosePunctuation(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if isPunctuationOnly(part) && len(out) > 0 {
			out[len(out)-1] += part
			continue
		}
		out = append(out, part)
	}
	return out
}

func isPunctuationOnly(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if !isSentenceTailRune(r) {
			return false
		}
	}
	return true
}

func trimHorizontalSpace(text string) string {
	return strings.Trim(text, " \t\r")
}

func packReplyParts(parts []string, maxChars, maxParts int) []string {
	if maxChars <= 0 {
		maxChars = 80
	}
	if maxParts <= 1 {
		joined := strings.TrimSpace(strings.Join(parts, ""))
		if joined == "" {
			return nil
		}
		return []string{joined}
	}

	pieces := make([]string, 0, len(parts))
	for _, part := range parts {
		part = trimHorizontalSpace(part)
		if strings.TrimSpace(part) == "" {
			continue
		}
		if runeLen(part) <= maxChars {
			pieces = append(pieces, part)
			continue
		}
		pieces = append(pieces, chunkReplyPart(part, maxChars)...)
	}
	if len(pieces) == 0 {
		return nil
	}

	out := make([]string, 0, len(pieces))
	current := ""
	for _, piece := range pieces {
		if current == "" {
			current = piece
			continue
		}
		if runeLen(current)+runeLen(piece) <= maxChars {
			current += piece
			continue
		}
		appendPackedReplyPart(&out, current)
		current = piece
	}
	appendPackedReplyPart(&out, current)

	if len(out) <= maxParts {
		return out
	}
	capped := append([]string(nil), out[:maxParts-1]...)
	capped = append(capped, strings.TrimSpace(strings.Join(out[maxParts-1:], "")))
	return capped
}

func appendPackedReplyPart(parts *[]string, text string) {
	text = strings.TrimSpace(text)
	if text != "" {
		*parts = append(*parts, text)
	}
}

func chunkReplyPart(text string, maxChars int) []string {
	separator := ""
	for strings.HasPrefix(text, "\n") {
		separator = "\n"
		text = strings.TrimPrefix(text, "\n")
	}
	chunks := chunkByRunes(text, maxChars)
	if separator != "" && len(chunks) > 0 {
		chunks[0] = separator + chunks[0]
	}
	return chunks
}

func chunkByRunes(text string, maxChars int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxChars <= 0 {
		return []string{text}
	}
	runes := []rune(text)
	out := make([]string, 0, len(runes)/maxChars+1)
	for len(runes) > maxChars {
		out = append(out, strings.TrimSpace(string(runes[:maxChars])))
		runes = runes[maxChars:]
	}
	if len(runes) > 0 {
		out = append(out, strings.TrimSpace(string(runes)))
	}
	return out
}

func runeLen(text string) int {
	return len([]rune(text))
}
