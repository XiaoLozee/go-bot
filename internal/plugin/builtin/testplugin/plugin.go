package testplugin

import (
	"context"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

type Plugin struct {
	messenger sdk.Messenger
	api       sdk.BotAPI
}

func New() sdk.Plugin {
	return &Plugin{}
}

func (p *Plugin) Manifest() sdk.Manifest {
	return sdk.Manifest{
		ID:          "test",
		Name:        "测试插件",
		Version:     "0.1.0",
		Description: "私聊测试消息与基础用户信息查询",
		Author:      "Go-bot",
		Builtin:     true,
	}
}

func (p *Plugin) Start(_ context.Context, env sdk.Env) error {
	p.messenger = env.Messenger
	p.api = env.BotAPI
	return nil
}

func (p *Plugin) Stop(context.Context) error {
	return nil
}

func (p *Plugin) HandleEvent(ctx context.Context, evt event.Event) error {
	if evt.Kind != "message" || evt.ChatType != "private" || evt.RawText != "测试" {
		return nil
	}

	target := message.Target{
		ConnectionID: evt.ConnectionID,
		ChatType:     "private",
		UserID:       evt.UserID,
	}

	if err := p.messenger.SendText(ctx, target, "测试消息发送"); err != nil {
		return err
	}

	if p.api == nil {
		return nil
	}

	info, err := p.api.GetStrangerInfo(ctx, evt.ConnectionID, evt.UserID)
	if err != nil || info == nil || info.Nickname == "" {
		return nil
	}
	return p.messenger.SendText(ctx, target, info.Nickname)
}
