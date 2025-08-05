package function

import (
	"fmt"
	"github.com/XiaoLuozee/go-bot/botapi"
	"github.com/XiaoLuozee/go-bot/handler"
	"github.com/XiaoLuozee/go-bot/registry"
	"strconv"
	"strings"
)

type ForgingMessage struct{}

func (p *ForgingMessage) Process(event interface{}) {
	msg, ok := event.(handler.OB11GroupMessage)
	if !ok {
		return
	}

	trigger := "/伪造记录"
	if !strings.HasPrefix(msg.RawMessage, trigger) {
		return
	}

	content := strings.TrimSpace(strings.TrimPrefix(msg.RawMessage, trigger))

	if content == "" {
		// 如果用户只发送了指令，没有提供内容，可以发送一条帮助信息
		helpText := "使用方法：\n/伪造记录\nQQ号1:消息1\nQQ号2:消息2"
		botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, helpText)
		return
	}

	nodes, err := parseForgedContent(content)
	if err != nil {
		botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, fmt.Sprintf("格式错误: %v", err))
		return
	}

	if len(nodes) == 0 {
		botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, "没有可以伪造的内容哦~")
		return
	}

	botapi.SendGroupForwardMsg(
		msg.GroupId,
		nodes,
		botapi.WithSource("群聊的聊天记录"),
	)
}

// parseForgedContent 解析用户输入的伪造内容
func parseForgedContent(content string) ([]botapi.ForwardNode, error) {
	lines := strings.Split(content, "\n")

	var nodes []botapi.ForwardNode

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("第 %d 行格式不正确，缺少冒号", i+1)
		}

		// 解析 QQ 号
		qqStr := strings.TrimSpace(parts[0])
		userID, err := strconv.ParseInt(qqStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行的 QQ 号 '%s' 不是有效的数字", i+1, qqStr)
		}

		messageContent := strings.TrimSpace(parts[1])

		info, err := botapi.GetStrangerInfo(userID)

		node := botapi.ForwardNode{
			Type: "node",
			Data: botapi.ForwardNodeData{
				UserID:   userID,
				Nickname: info.Nickname,
				Content:  messageContent,
			},
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}

func init() {
	registry.RegisterFactory(
		registry.CreatePluginFactory(&ForgingMessage{}, "bot.function.forging_message", true),
	)
}
