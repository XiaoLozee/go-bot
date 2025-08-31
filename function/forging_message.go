package function

import (
	"encoding/json"
	"fmt"
	"github.com/XiaoLuozee/go-bot/botapi"
	"github.com/XiaoLuozee/go-bot/handler"
	"github.com/XiaoLuozee/go-bot/registry"
	"html"
	"log"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ForgingMessage struct{}

var cqCodeRegex = regexp.MustCompile(`\[CQ:([^,]+)((,([^,]+)=([^,\]]+))*)]`)

func (p *ForgingMessage) Process(event interface{}) {
	msg, ok := event.(handler.OB11GroupMessage)
	if !ok {
		return
	}

	// 我们直接分析消息段数组，这是最可靠的数据源
	segments := msg.Message.Message
	if len(segments) == 0 {
		return
	}

	// 查找第一个文本段，并检查是否包含指令
	var firstTextSegment *handler.OB11Segment
	var firstTextIndex = -1
	for i, seg := range segments {
		if seg.Type == "text" {
			firstTextSegment = &segments[i]
			firstTextIndex = i
			break
		}
	}

	// 如果没有文本段，或者第一个文本段不包含指令，则不是我们的目标
	if firstTextSegment == nil {
		return
	}
	var textData botapi.TextData
	if json.Unmarshal(firstTextSegment.Data, &textData) != nil {
		return
	}

	trigger := "/伪造记录"
	if !strings.Contains(textData.Text, trigger) {
		return
	}

	// --- 开始全新的、更简单的解析逻辑 ---

	// 从第一个文本段中，去掉指令之前的部分（如果有的话）
	commandStr := textData.Text[strings.Index(textData.Text, trigger):]
	// 按空格分割指令和参数
	parts := strings.Fields(commandStr)

	// `parts` 可能是 ["/伪造记录"], ["/伪造记录", "all"], ["/伪造记录", "all", "图文测试"]

	// 检查是否是 "all" 指令
	if len(parts) > 1 && parts[1] == "all" {
		// --- 处理 "all" 指令 ---
		var contentSegments []handler.OB11Segment

		// 1. 处理指令段自身的剩余文本
		//    找到 "all" 在原始文本中的结束位置
		allEndIndex := strings.Index(commandStr, "all") + len("all")
		remainingText := strings.TrimSpace(commandStr[allEndIndex:])

		if remainingText != "" {
			newData, _ := json.Marshal(botapi.TextData{Text: remainingText})
			contentSegments = append(contentSegments, handler.OB11Segment{Type: "text", Data: newData})
		}

		// 2. 添加第一个文本段之后的所有段
		if firstTextIndex+1 < len(segments) {
			contentSegments = append(contentSegments, segments[firstTextIndex+1:]...)
		}

		if len(contentSegments) == 0 {
			botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, "请在 'all' 后面附带要伪造的消息内容哦（可以是文字或图片）！")
			return
		}

		p.handleForgeAll(msg.GroupId, contentSegments)
		return
	}

	// --- 如果不是 "all" 指令，则执行逐行伪造的旧逻辑 ---

	// 拼接所有文本内容用于解析
	var textContentForParsing string
	// 从指令所在的文本段开始拼接
	textContentForParsing += textData.Text
	// 拼接后续所有文本段
	for i := firstTextIndex + 1; i < len(segments); i++ {
		if segments[i].Type == "text" {
			var data botapi.TextData
			if json.Unmarshal(segments[i].Data, &data) == nil {
				textContentForParsing += data.Text
			}
		}
	}

	// 从拼接好的文本中，去掉指令本身
	finalContent := strings.TrimSpace(strings.TrimPrefix(textContentForParsing, trigger))

	if finalContent == "" {
		helpText := "使用方法：\n/伪造记录\nQQ号1:消息1\n或\n/伪造记录 all <内容>"
		botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, helpText)
		return
	}

	nodes, err := parseForgedContent(finalContent)
	if err != nil {
		botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, fmt.Sprintf("格式错误: %v", err))
		return
	}

	if len(nodes) == 0 {
		botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, "没有可以伪造的内容哦~")
		return
	}

	botapi.SendGroupForwardMsg(msg.GroupId, nodes, botapi.WithSource("群聊的聊天记录"))
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

func (p *ForgingMessage) handleForgeAll(groupId int64, segments []handler.OB11Segment) {
	// 步骤 0: 先给用户一个即时反馈
	botapi.SendTextMsg(botapi.GroupMessage, groupId, "收到全体伪造指令，正在获取群成员列表...")

	// 步骤 1: 获取群成员列表
	memberList, err := botapi.GetGroupMemberList(groupId)
	if err != nil {
		log.Printf("获取群成员列表失败: %v", err)
		botapi.SendTextMsg(botapi.GroupMessage, groupId, fmt.Sprintf("获取群成员列表失败了QAQ: %v", err))
		return
	}

	// 步骤 2: 检查并处理边界情况（如空群组）
	if len(memberList) == 0 {
		botapi.SendTextMsg(botapi.GroupMessage, groupId, "奇怪，这个群里好像没有人...")
		return
	}

	// 步骤 3: 如果群成员数量超过阈值，则进行随机抽样
	const maxMembers = 100
	var selectedMembers []botapi.GroupMemberInfo

	if len(memberList) > maxMembers {
		botapi.SendTextMsg(botapi.GroupMessage, groupId, fmt.Sprintf("群成员数量 (%d) 超过 %d，将随机抽取 %d 人", len(memberList), maxMembers, maxMembers))

		// 使用当前时间作为种子，创建新的随机数生成器
		r := rand.New(rand.NewSource(time.Now().UnixNano()))

		// 创建一个 memberList 的副本进行洗牌操作，避免修改原始列表
		shuffledList := make([]botapi.GroupMemberInfo, len(memberList))
		copy(shuffledList, memberList)

		// 使用 Fisher-Yates 算法（由 Go 标准库实现）打乱副本
		r.Shuffle(len(shuffledList), func(i, j int) {
			shuffledList[i], shuffledList[j] = shuffledList[j], shuffledList[i]
		})

		// 从打乱后的列表中截取前 100 个作为抽样结果
		selectedMembers = shuffledList[:maxMembers]
	} else {
		// 如果成员数量未超过阈值，则使用全部成员
		selectedMembers = memberList
	}

	// 步骤 4: 为每个被选中的成员构建一个 ForwardNode
	var nodes []botapi.ForwardNode
	for _, member := range selectedMembers {
		// 优先使用群名片作为昵称，如果为空，则使用 QQ 昵称
		nickname := member.Card
		if nickname == "" {
			nickname = member.Nickname
		}

		// 创建转发节点
		node := botapi.ForwardNode{
			Type: "node",
			Data: botapi.ForwardNodeData{
				UserID:   member.UserID,
				Nickname: nickname,
				// 核心：直接使用传入的 `segments` 作为消息内容
				Content: segments,
			},
		}
		nodes = append(nodes, node)
	}

	// 步骤 5: 最后的检查，确保有可发送的内容
	if len(nodes) == 0 {
		botapi.SendTextMsg(botapi.GroupMessage, groupId, "未能构建任何伪造消息")
		return
	}

	// 步骤 6: 发送最终的合并转发消息
	botapi.SendGroupForwardMsg(
		groupId,
		nodes,
		botapi.WithSource("来自群聊的消息"),
	)
}

func init() {
	registry.RegisterFactory(
		registry.CreatePluginFactory(&ForgingMessage{}, "bot.function.forging_message", true),
	)
}
