package registry

import (
	"fmt"
	"github.com/spf13/viper"
	"reflect"
)

type Function interface {
	Process(event interface{})
}

// 保存所有注册函数
var functions []Function

// PluginFactory 插件工厂函数
// 检查配置，并返回一个插件实例（如果启用的话）
type PluginFactory func() Function

// factories 存储所有通过 init() 注册的工厂函数
var factories []PluginFactory

// RegisterFactory 允许插件在 init() 阶段注册它们的工厂函数。
func RegisterFactory(factory PluginFactory) {
	factories = append(factories, factory)
}

// LoadPlugins 在配置加载完毕后，由 main 函数调用
// 它会遍历所有工厂，执行它们，并将返回的插件实例加载到系统中
func LoadPlugins() {
	fmt.Println("--- 开始加载插件 ---")
	for _, factory := range factories {
		if plugin := factory(); plugin != nil {
			// 如果工厂函数返回了一个非 nil 的插件，就将其加入到活动插件列表
			functions = append(functions, plugin)
		}
	}
	fmt.Println("--- 插件加载完毕 ---")
}

// Dispatch 广播事件给所有已加载的活动插件
func Dispatch(event interface{}) {
	for _, f := range functions {
		f.Process(event)
	}
}

// CreatePluginFactory 插件工厂创建器
func CreatePluginFactory(plugin Function, configKey string, defaultEnabled bool) PluginFactory {
	return func() Function {
		viper.SetDefault(configKey, defaultEnabled)

		if viper.GetBool(configKey) {
			pluginName := reflect.TypeOf(plugin).Elem().Name()
			fmt.Printf("插件 [%s] 已加载。\n", pluginName)

			return plugin
		}

		return nil
	}
}
