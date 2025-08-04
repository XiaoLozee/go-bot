package function

import (
	"fmt"
	"github.com/XiaoLuozee/go-bot/handler"
	"github.com/XiaoLuozee/go-bot/registry"
)

type RepeaterPlugin struct{}

// Process 是 RepeaterPlugin 实现 Function 接口的方法。
func (p *RepeaterPlugin) Process(event interface{}) {
	// 1. 使用类型断言，判断事件是否是我们关心的群聊消息
	msg, ok := event.(handler.OB11PrivateMessage)
	if !ok {
		// 如果不是群聊消息，直接忽略
		return
	}

	// 2. 实现复读逻辑
	if msg.RawMessage == "测试" {
		// 实际上这里应该调用发送消息的 API
		// 我们用打印来模拟
		fmt.Printf("[Repeater Plugin] 测试成功")
	}
}

// init() 函数是 Go 的一个特性，它会在 main 函数执行前被自动调用。
// 我们在这里将插件实例注册到我们的注册表中。
func init() {
	// 创建插件实例
	plugin := &RepeaterPlugin{}
	// 将其注册到总线
	registry.Register(plugin)
	fmt.Println("插件 [测试] 已加载。")
}
