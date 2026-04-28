package externalexec

import (
	"sort"

	"github.com/XiaoLozee/go-bot/internal/plugin/builtin/testplugin"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

// embeddedFactories 仅服务于 external_exec 的 embedded runner。
// 它们不会参与 builtin 插件注册；当前 runtime 中真正的 builtin 仅保留 testplugin。
var embeddedFactories = map[string]func() sdk.Plugin{
	"test": testplugin.New,
}

func EmbeddedFactory(id string) (func() sdk.Plugin, bool) {
	factory, ok := embeddedFactories[id]
	return factory, ok
}

func EmbeddedPluginIDs() []string {
	out := make([]string, 0, len(embeddedFactories))
	for id := range embeddedFactories {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
