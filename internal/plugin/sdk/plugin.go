package sdk

import (
	"context"

	"github.com/XiaoLozee/go-bot/internal/domain/event"
)

type Plugin interface {
	Manifest() Manifest
	Start(ctx context.Context, env Env) error
	Stop(ctx context.Context) error
	HandleEvent(ctx context.Context, evt event.Event) error
}
