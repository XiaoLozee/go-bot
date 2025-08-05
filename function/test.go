package function

import (
	"fmt"
	"github.com/XiaoLuozee/go-bot/botapi"
	"github.com/XiaoLuozee/go-bot/handler"
	"github.com/XiaoLuozee/go-bot/registry"
)

type RepeaterPlugin struct{}

func (p *RepeaterPlugin) Process(event interface{}) {
	msg, ok := event.(handler.OB11PrivateMessage)
	if !ok {
		return
	}

	// 2. 实现复读逻辑
	if msg.RawMessage == "测试" {
		botapi.SendImgMsg(botapi.PrivateMessage, msg.UserID, "https://raw.gitcode.com/qq_44112897/images/raw/master/comic/14.jpg")
		//botapi(botapi.PrivateMessage, msg.UserID, "测试消息发送")
	}
}

func init() {
	// 创建插件实例
	plugin := &RepeaterPlugin{}
	// 将其注册到总线
	registry.Register(plugin)
	fmt.Println("插件 [测试] 已加载。")
}
