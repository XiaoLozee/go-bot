package function

import (
	"fmt"
	"github.com/XiaoLuozee/go-bot/botapi"
	"github.com/XiaoLuozee/go-bot/handler"
	"github.com/XiaoLuozee/go-bot/registry"
)

type MenuHint struct{}

func (m *MenuHint) Process(event interface{}) {
	msg, ok := event.(handler.OB11GroupMessage)
	if !ok {
		return
	}

	// 使用 map 来存储触发词，更优雅
	triggers := map[string]bool{
		"菜单": true, "帮助": true, "/help": true,
		"/menu": true, "/菜单": true, "/帮助": true,
	}

	if _, triggered := triggers[msg.RawMessage]; triggered {
		// 调用新的发送菜单函数
		sendMenuAsForwardMsg(msg.GroupId, msg.SelfID)
	}
}

// sendMenuAsForwardMsg 使用合并转发消息来发送菜单
func sendMenuAsForwardMsg(groupId int64, selfID int64) {
	// 从配置中获取机器人的信息，用于伪造消息
	botInfo, err := botapi.GetStrangerInfo(selfID)
	if err != nil {
		fmt.Println("获取机器人信息失败:", err)
		return
	}
	botNickname := botInfo.Nickname
	if botNickname == "" {
		botNickname = "罗纸酱" // 提供一个默认值
	}

	// --- 构建菜单的每一个节点 (Node) ---
	var nodes []botapi.ForwardNode

	// 节点1: 标题
	nodes = append(nodes, createMenuNode(selfID, botNickname, "✨ 欢迎使用罗纸酱 ✨\n\n以下是我的功能列表"))

	// 节点2: 娱乐功能
	entertainmentMenu := `--- 🎉 娱乐功能 ---
/伪造记录
  - 格式1: /伪造记录 QQ:内容
  - 格式2: /伪造记录 all <内容>
幻影坦克 / 彩色幻影坦克
  - 发送指令并带上一张图开启
梅花 你要问的问题
  - 发送指令并带上问题，罗纸酱将调用ai为你算卦`
	nodes = append(nodes, createMenuNode(selfID, botNickname, entertainmentMenu))

	// 节点3: 工具功能
	toolsMenu := `--- 🛠️ 工具功能 ---
解析短视频
  - 支持抖音/B站/快手等
  - 直接发送分享链接即可
jm <ID>
  - 获取对应ID的禁漫漫画`
	nodes = append(nodes, createMenuNode(selfID, botNickname, toolsMenu))

	// 节点4: 结尾
	nodes = append(nodes, createMenuNode(selfID, botNickname, "有任何问题或建议，直接联系小罗纸~"))

	botapi.SendGroupForwardMsg(
		groupId,
		nodes,
		botapi.WithSource("罗纸酱的功能菜单"),
		botapi.WithPrompt(fmt.Sprintf("[%d条] 罗纸酱功能列表", len(nodes))),
	)
}

// createMenuNode 是一个辅助函数，用于快速创建菜单节点
func createMenuNode(userID int64, nickname, content string) botapi.ForwardNode {
	return botapi.ForwardNode{
		Type: "node",
		Data: botapi.ForwardNodeData{
			UserID:   userID,
			Nickname: nickname,
			Content:  content,
		},
	}
}

func init() {
	registry.RegisterFactory(
		registry.CreatePluginFactory(&MenuHint{}, "bot.function.menu_hint", true),
	)
}
