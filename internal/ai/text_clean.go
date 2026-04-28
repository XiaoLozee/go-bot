package ai

import (
	"html"
	"regexp"
	"strings"
)

var (
	cqCodePattern = regexp.MustCompile(`(?i)\[CQ:[^\]]*\]`)
	urlPattern    = regexp.MustCompile(`(?i)https?://\S+`)

	mediaPlaceholderTexts = map[string]struct{}{
		"[动画表情]":   {},
		"[图片]":     {},
		"[表情]":     {},
		"[语音]":     {},
		"[视频]":     {},
		"[文件]":     {},
		"[image]":  {},
		"[face]":   {},
		"[record]": {},
		"[audio]":  {},
		"[voice]":  {},
		"[video]":  {},
		"[file]":   {},
	}
	opaqueMemoryTokens = map[string]struct{}{
		"amp":          {},
		"appid":        {},
		"app":          {},
		"cq":           {},
		"image":        {},
		"summary":      {},
		"file":         {},
		"fileid":       {},
		"file_id":      {},
		"sub_type":     {},
		"file_size":    {},
		"url":          {},
		"http":         {},
		"https":        {},
		"rkey":         {},
		"cn":           {},
		"com":          {},
		"qq":           {},
		"nt":           {},
		"multimedia":   {},
		"download":     {},
		"conversation": {},
	}
)

func cleanRawFallbackText(value string) string {
	text := stripCQCodes(value)
	text = strings.Join(strings.Fields(text), " ")
	return strings.TrimSpace(text)
}

func sanitizeMemoryText(value string) string {
	text := stripCQCodes(value)
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "图片识别：") {
			continue
		}
		if _, ok := mediaPlaceholderTexts[strings.ToLower(line)]; ok {
			continue
		}
		kept = append(kept, line)
	}
	text = strings.Join(kept, " ")
	text = urlPattern.ReplaceAllString(text, " ")
	text = strings.Join(strings.Fields(text), " ")
	if !isUsefulMemoryText(text) {
		return ""
	}
	return text
}

func stripCQCodes(value string) string {
	text := cqCodePattern.ReplaceAllString(value, " ")
	text = html.UnescapeString(text)
	text = cqCodePattern.ReplaceAllString(text, " ")
	return text
}

func isUsefulMemoryText(value string) bool {
	text := strings.TrimSpace(value)
	if text == "" {
		return false
	}
	if _, ok := mediaPlaceholderTexts[strings.ToLower(text)]; ok {
		return false
	}
	return !strings.Contains(strings.ToLower(text), "[cq:")
}

func isOpaqueMemoryToken(value string) bool {
	token := strings.TrimSpace(strings.ToLower(value))
	if token == "" {
		return true
	}
	if _, ok := opaqueMemoryTokens[token]; ok {
		return true
	}
	if strings.HasPrefix(token, "cq") || strings.HasPrefix(token, "fileid") {
		return true
	}
	hasASCII := false
	hasDigit := false
	hasOpaqueSeparator := false
	hasHan := false
	for _, r := range token {
		switch {
		case r >= '0' && r <= '9':
			hasASCII = true
			hasDigit = true
		case r >= 'a' && r <= 'z':
			hasASCII = true
		case r == '_' || r == '-':
			hasOpaqueSeparator = true
		case r >= '\u4e00' && r <= '\u9fff':
			hasHan = true
		}
	}
	return !hasHan && hasASCII && len([]rune(token)) >= 12 && (hasDigit || hasOpaqueSeparator)
}
