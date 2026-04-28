package builtin

import (
	"github.com/XiaoLozee/go-bot/internal/plugin/builtin/testplugin"
	"github.com/XiaoLozee/go-bot/internal/plugin/host"
)

func RegisterAll(h *host.Host) {
	h.Register(testplugin.New)
}
