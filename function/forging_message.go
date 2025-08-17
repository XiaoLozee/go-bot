package function

import (
	"encoding/json"
	"fmt"
	"github.com/XiaoLuozee/go-bot/botapi"
	"github.com/XiaoLuozee/go-bot/handler"
	"github.com/XiaoLuozee/go-bot/registry"
	"html"
	"regexp"
	"strconv"
	"strings"
)

type ForgingMessage struct{}

var cqCodeRegex = regexp.MustCompile(`\[CQ:([^,]+)((,([^,]+)=([^,\]]+))*)]`)

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
		helpText := "使用方法：\n/伪造记录\nQQ号1:消息1\nQQ号2:消息2\n\n消息支持CQ码，例如：\n[CQ:image,file=http://...]\n[CQ:face,id=178]"
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

// parseContentToSegments 将CQ码的字符串解析成消息段数组
func parseContentToSegments(content string) []handler.OB11Segment {
	var segments []handler.OB11Segment

	matches := cqCodeRegex.FindAllStringSubmatchIndex(content, -1)

	lastIndex := 0
	for _, match := range matches {

		if match[0] > lastIndex {
			text := content[lastIndex:match[0]]
			textData, _ := json.Marshal(botapi.TextData{Text: text})
			segments = append(segments, handler.OB11Segment{
				Type: "text",
				Data: textData,
			})
		}

		cqType := content[match[2]:match[3]]
		paramsStr := content[match[4]:match[5]]

		dataMap := make(map[string]interface{})
		params := strings.Split(paramsStr, ",")
		for _, param := range params {
			if param == "" {
				continue
			}
			parts := strings.SplitN(param, "=", 2)
			if len(parts) == 2 {
				dataMap[parts[0]] = parts[1]
			}
		}

		jsonData, _ := json.Marshal(dataMap)
		segments = append(segments, handler.OB11Segment{
			Type: cqType,
			Data: jsonData,
		})

		lastIndex = match[1]
	}

	if lastIndex < len(content) {
		text := content[lastIndex:]
		textData, _ := json.Marshal(botapi.TextData{Text: text})
		segments = append(segments, handler.OB11Segment{
			Type: "text",
			Data: textData,
		})
	}

	return segments
}

// parseForgedContent 解析用户输入的伪造内容
func parseForgedContent(content string) ([]botapi.ForwardNode, error) {
	decodedContent := html.UnescapeString(content)

	lines := strings.Split(decodedContent, "\n")
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

		qqStr := strings.TrimSpace(parts[0])
		userID, err := strconv.ParseInt(qqStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行的 QQ 号 '%s' 不是有效的数字", i+1, qqStr)
		}

		messageContent := strings.TrimSpace(parts[1])

		segments := parseContentToSegments(messageContent)

		info, err := botapi.GetStrangerInfo(userID)
		nickname := fmt.Sprintf("User %d", userID) // 默认昵称
		if err == nil && info.Nickname != "" {
			nickname = info.Nickname
		}

		node := botapi.ForwardNode{
			Type: "node",
			Data: botapi.ForwardNodeData{
				UserID:   userID,
				Nickname: nickname,
				Content:  segments,
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
