package ai

import (
	"reflect"
	"testing"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
)

func TestSanitizeMemoryTextDropsCQAndVisionNoise(t *testing.T) {
	input := "图片识别：一张猫图\n[CQ:image,summary=&#91;动画表情&#93;,file=abc.gif,url=https://example.com/a?appid=1407&amp;rkey=abc]\n我喜欢东方Project"
	got := sanitizeMemoryText(input)
	if got != "我喜欢东方Project" {
		t.Fatalf("sanitizeMemoryText() = %q, want user text only", got)
	}
}

func TestTopKeywordsDropsCQNoise(t *testing.T) {
	got := topKeywords([]string{
		"[CQ:image,summary=&#91;动画表情&#93;,file=19CC7C8C328F2F293168B837DDD813AA.gif,url=https://multimedia.nt.qq.com.cn/download?appid=1407&amp;rkey=CAISMG]",
		"camsmlae_jf39whkwkben8ps conversation appid amp com cn",
		"今天继续红魔馆",
	}, 8)
	if !reflect.DeepEqual(got, []string{"今天继续红魔馆"}) {
		t.Fatalf("topKeywords() = %+v, want only useful text", got)
	}
}

func TestExtractActiveMemesDropsCQOnlyMessages(t *testing.T) {
	got := extractActiveMemes([]ConversationMessage{
		{Role: "user", Text: "[CQ:image,summary=&#91;动画表情&#93;,file=a.gif,url=https://example.com/a?appid=1]"},
		{Role: "user", Text: "[CQ:image,summary=&#91;动画表情&#93;,file=a.gif,url=https://example.com/a?appid=1]"},
		{Role: "user", Text: "红魔馆哈哈"},
		{Role: "user", Text: "红魔馆哈哈"},
	}, 4)
	if !reflect.DeepEqual(got, []string{"红魔馆哈哈"}) {
		t.Fatalf("extractActiveMemes() = %+v, want only useful meme", got)
	}
}

func TestCleanEventTextDoesNotFallbackToRawCQWhenSegmentsExist(t *testing.T) {
	got := cleanEventText(event.Event{
		Segments: []message.Segment{message.Image("a.gif")},
		RawText:  "[CQ:image,summary=&#91;动画表情&#93;,file=a.gif,url=https://example.com/a?appid=1]",
	})
	if got != "" {
		t.Fatalf("cleanEventText() = %q, want empty image-only text", got)
	}
}
