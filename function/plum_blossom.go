package function

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/XiaoLuozee/go-bot/botapi"
	"github.com/XiaoLuozee/go-bot/handler"
	"github.com/XiaoLuozee/go-bot/registry"
	"github.com/spf13/viper"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// ChatRequest API请求结构体
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

// Message 消息结构体
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatResponse API响应结构体
type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// 八卦名称数组，索引从1开始
var guaNames = []string{"", "乾", "兑", "离", "震", "巽", "坎", "艮", "坤"}

// 八卦对应的先天八卦数
var xianTianShu = map[string]int{
	"乾": 1, "兑": 2, "离": 3, "震": 4,
	"巽": 5, "坎": 6, "艮": 7, "坤": 8,
}

// getGuaNameByNum 根据数字（1-8）获取卦名。如果数字为0，则视为8（坤）。
func getGuaNameByNum(num int64) string {
	if num == 0 {
		num = 8
	}
	if num > 0 && num <= 8 {
		return guaNames[num]
	}
	return "未知"
}

type PlumBlossom struct{}

func (p *PlumBlossom) Process(event interface{}) {
	msg, ok := event.(handler.OB11GroupMessage)
	if !ok {
		return
	}

	fields := strings.Fields(msg.RawMessage)
	if len(fields) != 2 || strings.ToLower(fields[0]) != "梅花" {
		return
	}

	// 获取问题
	question := strings.Join(fields[1:], " ")
	botapi.SendReplyMsg(botapi.GroupMessage, msg.GroupId, msg.MessageId, "正在为你计算卦象...")
	// 问题字符数
	questionRuneCount := int64(len([]rune(question)))
	// 取QQ号的最后五位作为常量
	constant := msg.UserID % 100000
	// 获取当前时间
	currentTime := time.Now()
	// 获取当前时间的Unix时间戳（秒）
	timestamp := currentTime.Unix()
	// 获取格式化的当前日期和时间
	currentDate := currentTime.Format("2006-01-02 15:04:05")
	// 3. 实现起卦算法
	// 上卦数 = (问题字数 + QQ号后五位) % 8
	shangGuaNum := (questionRuneCount + constant) % 8
	shangGuaName := getGuaNameByNum(shangGuaNum)

	// 下卦数 = (问题字数 + QQ号后五位 + 时间戳) % 8
	xiaGuaNum := (questionRuneCount + constant + timestamp) % 8
	xiaGuaName := getGuaNameByNum(xiaGuaNum)

	// 变爻 = (问题字数 + QQ号后五位 + 时间戳) % 6
	bianYao := (questionRuneCount + constant + timestamp) % 6
	if bianYao == 0 {
		bianYao = 6
	}

	huGuaXiaName, huGuaShangName, err := getHuGua(shangGuaName, xiaGuaName)
	var huGuaInfo string
	if err != nil {
		log.Printf("计算互卦失败: %v", err)
		huGuaInfo = "无" // 如果出错，显示无
	} else {
		huGuaInfo = fmt.Sprintf("上互卦为 %s，下互卦为 %s", huGuaShangName, huGuaXiaName)
	}

	// 计算体卦和用卦，根据变爻的位置来确定
	var tiGua, yongGua string
	// 变爻在 1, 2, 3 爻，则下卦为用卦，上卦为体卦
	if bianYao >= 1 && bianYao <= 3 {
		tiGua = shangGuaName
		yongGua = xiaGuaName
	} else { // 变爻在 4, 5, 6 爻，则上卦为用卦，下卦为体卦
		tiGua = xiaGuaName
		yongGua = shangGuaName
	}

	hexagramInfo := fmt.Sprintf(
		"公历时间：%s\n问题：%s\n本卦：上%s下%s\n体卦：%s，用卦：%s\n互卦：%s\n变爻在第 %d 爻",
		currentDate,
		question,
		shangGuaName,
		xiaGuaName,
		tiGua,   // 新增
		yongGua, // 新增
		huGuaInfo,
		bianYao,
	)

	// 调用 AI 解读并以合并转发的方式发送结果
	go func(gid int64, originalMsgId int64, info string, botId int64, botName string, userId int64, userName string) {
		// 调用 AI API，获取解读
		interpretation := interpretTheHexagram(info)
		fmt.Printf("开始解读卦象")
		if interpretation == "" {
			botapi.SendReplyMsg(botapi.GroupMessage, gid, originalMsgId, "解读卦象失败，可能是服务器问题")
			return
		}

		var nodes []botapi.ForwardNode

		node1 := botapi.ForwardNode{
			Type: "node",
			Data: botapi.ForwardNodeData{
				UserID:   botId,
				Nickname: botName,
				Content:  fmt.Sprintf("根据您的问题，得到的卦象信息如下：\n%s", info),
			},
		}
		nodes = append(nodes, node1)

		node2 := botapi.ForwardNode{
			Type: "node",
			Data: botapi.ForwardNodeData{
				UserID:   botId,
				Nickname: botName,
				Content:  fmt.Sprintf("【卦象解读】\n%s", interpretation),
			},
		}
		nodes = append(nodes, node2)

		// 一个友好的结尾。
		node3 := botapi.ForwardNode{
			Type: "node",
			Data: botapi.ForwardNodeData{
				UserID:   botId,
				Nickname: botName,
				Content:  fmt.Sprintf("小罗纸提醒您：卦象仅为一片微光，照亮可能的路，但前方的轨迹始终由您亲手描绘，有些事情无论结果如何，都放手去做吧。仅供娱乐参考，愿您放下顾虑，从容前行，祝您好运！"),
			},
		}
		nodes = append(nodes, node3)

		botapi.SendGroupAtWithTextMsg(gid, userId, "卦象解读成功")

		botapi.SendGroupForwardMsg(
			gid,
			nodes,
			botapi.WithSource(fmt.Sprintf("%s 的卜卦结果", userName)),
		)

	}(msg.GroupId, msg.MessageId, hexagramInfo, msg.SelfID, "罗纸酱", msg.UserID, msg.NikeName) // 将需要的信息传入 goroutine
}

// 调用ai解读卦象
func interpretTheHexagram(hexagram string) string {
	apiKey := viper.GetString("bot.function.plum_blossom.api_key")
	apiURL := "https://api.deepseek.com/chat/completions"

	systemPrompt := `你是一位深谙《梅花易数》的解卦大师。
你的任务是根据用户提供的卦象信息（包含时间、问题、本卦、互卦、变爻），进行一次完整、专业且清晰易懂的解读。

**输出格式与解读逻辑，必须严格遵循以下规则：**

1.  **【格式要求】**
    -   **绝对禁止**使用任何 Markdown 格式 (如 ` + "``, **, #, -, >" + ` 等)。
    -   使用换行来分隔段落，保持排版清晰。
    -   **不要复述**用户已经提供给你的卦象信息，直接开始解读。

2.  **【核心解读逻辑 (必须按此顺序)】**
    -   **第一步：分析本卦 (体用关系)**
        -   简要说明本卦（例如“泽天夬”）的整体含义，它代表了事情的**初始状态**和**基本格局**。
        -   明确指出哪个是体卦，哪个是用卦。**体卦代表问卜者或主体，用卦代表所问之事或客体。**
        -   分析体用之间的生克关系（例如，体克用、用生体等），并解释这对事情的吉凶有何初步影响。

    -   **第二步：分析互卦 (过程演变)**
        -   说明互卦（例如“乾为天”）的含义，它揭示了事物发展的**中间过程、内部状况或隐藏的变数**。
        -   分析互卦中的体用生克关系，这能反映出过程的顺利与否。

    -   **第三步：分析变卦 (最终结果)**
        -   根据变爻，说明本卦变成了哪个变卦（例如“泽风大过”）。
        -   变卦代表了事情的**最终结局**或**未来的发展趋势**。
        -   分析变卦的体用生克关系，这是判断最终吉凶的关键。

    -   **第四步：综合结论与建议**
        -   **总结**：综合本卦、互卦、变卦的分析，对用户提出的**具体问题**给出一个明确的结论
        -   **建议**：基于整个卦象的启示，为用户提供1-2条切实可行的行动建议或注意事项。
        -   给出最准确的结论，无论结局是好是坏，不要给出模棱两可的回答，要给出明确的吉凶结论。
        -   结论和建议不要使用太专业的术语，使用通俗易懂的语言。

3.  **【语言风格】**
    -   请使用一位智慧、沉稳且友善的解卦师的口吻。
    -   语言要通俗易懂，避免使用过于晦涩的术语，但在关键之处（如体用生克）要体现专业性。
`

	// 创建请求数据
	requestData := ChatRequest{
		Model: "deepseek-reasoner",
		Messages: []Message{
			{
				Role:    "system",
				Content: systemPrompt,
			},
			{
				Role:    "user",
				Content: hexagram,
			},
		},
		Stream: false,
	}
	// 将请求数据转换为JSON
	jsonData, err := json.Marshal(requestData)
	if err != nil {
		fmt.Printf("JSON编码错误: %v\n", err)
		return ""
	}

	// 创建HTTP请求
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("创建请求错误: %v\n", err)
		return ""
	}
	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("请求错误: %v\n", err)
		return ""
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("读取响应错误: %v\n", err)
		return ""
	}

	// 检查HTTP状态码
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("API返回错误状态码: %d\n", resp.StatusCode)
		fmt.Printf("响应内容: %s\n", string(body))
		return ""
	}

	// 解析响应
	var chatResponse ChatResponse
	err = json.Unmarshal(body, &chatResponse)
	if err != nil {
		fmt.Printf("JSON解析错误: %v\n", err)
		fmt.Printf("原始响应: %s\n", string(body))
		return ""
	}

	return chatResponse.Choices[0].Message.Content
}

// getHuGua 根据本卦的上卦和下卦，计算出互卦
// 返回互卦的下卦名、上卦名和一个错误（如果输入无效）
func getHuGua(shangGua, xiaGua string) (string, string, error) {
	// 定义卦象到二进制表示的映射
	guaToBin := map[string]string{
		"乾": "111", "兑": "011", "离": "101", "震": "001",
		"巽": "110", "坎": "010", "艮": "100", "坤": "000",
	}

	// 定义二进制到卦象的映射 (一次性构建)
	binToGua := make(map[string]string)
	for gua, bin := range guaToBin {
		binToGua[bin] = gua
	}

	// 获取上卦和下卦的二进制表示，并同时进行输入验证
	shangBin, ok1 := guaToBin[shangGua]
	xiaBin, ok2 := guaToBin[xiaGua]
	if !ok1 || !ok2 {
		return "", "", fmt.Errorf("无效的卦名输入: 上卦=%s, 下卦=%s", shangGua, xiaGua)
	}

	// 构建本卦的六个爻（从下到上：初爻 -> 上爻）
	benGuaSixYao := xiaBin + shangBin

	// 使用切片操作提取互卦的爻，更简洁高效
	// 互卦的下卦 = 本卦的二(1)、三(2)、四(3)爻
	huGuaXiaBin := benGuaSixYao[1:4]

	// 互卦的上卦 = 本卦的三(2)、四(3)、五(4)爻
	huGuaShangBin := benGuaSixYao[2:5]

	// 将二进制表示转换回卦名
	huGuaXiaName, ok1 := binToGua[huGuaXiaBin]
	huGuaShangName, ok2 := binToGua[huGuaShangBin]
	if !ok1 || !ok2 {
		return "", "", fmt.Errorf("互卦转换失败: 下卦二进制=%s, 上卦二进制=%s", huGuaXiaBin, huGuaShangBin)
	}

	return huGuaXiaName, huGuaShangName, nil
}

func init() {
	registry.RegisterFactory(
		registry.CreatePluginFactory(&PlumBlossom{}, "bot.function.plum_blossom.enabled", true),
	)
}
