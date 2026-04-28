package ai

import (
	"reflect"
	"testing"

	"github.com/XiaoLozee/go-bot/internal/config"
)

func TestSplitOutboundMessages_PreservesNewlineBoundaries(t *testing.T) {
	cfg := testAIConfig()
	cfg.Reply.Split = config.AIReplySplitConfig{
		Enabled:    true,
		OnlyCasual: true,
		MaxChars:   8,
		MaxParts:   3,
		DelayMS:    0,
	}
	text := "第一行\n第二行。第三行。"

	got := splitOutboundMessages(text, ReplyPlan{ReplyMode: "banter"}, cfg)
	want := []string{"第一行\n第二行。", "第三行。"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitOutboundMessages() = %#v, want %#v", got, want)
	}
}

func TestSplitNaturalReply_KeepsTrailingSentencePunctuation(t *testing.T) {
	text := "她说：好呀！”然后笑了。"

	got := splitNaturalReply(text)
	want := []string{"她说：好呀！”", "然后笑了。"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitNaturalReply() = %#v, want %#v", got, want)
	}
}
