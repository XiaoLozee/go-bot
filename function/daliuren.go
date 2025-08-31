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

// DaLiuRen 插件主结构体
type DaLiuRen struct{}

// LiuRenKe 大六壬课式结构体
type LiuRenKe struct {
	Date      string   // 占卜时间
	Question  string   // 问题
	YueJiang  string   // 月将
	ZhanShi   string   // 占时
	TianPan   []string // 天盘
	SiKe      []string // 四课
	SanChuan  []string // 三传
	QiFa      string   // 起法（九宗门）
	DayGanZhi string   // 日干支
}

// 地支列表
var diZhi = []string{"子", "丑", "寅", "卯", "辰", "巳", "午", "未", "申", "酉", "戌", "亥"}

// 天干列表
var tianGan = []string{"甲", "乙", "丙", "丁", "戊", "己", "庚", "辛", "壬", "癸"}

// 月将列表 (注意顺序，正月建寅)
var yueJiang = []string{
	"寅-功曹", "卯-太冲", "辰-天罡", "巳-太乙", "午-胜光", "未-小吉",
	"申-传送", "酉-从魁", "戌-河魁", "亥-登明", "子-神后", "丑-大吉",
}

// 天干寄宫表 (每个天干对应的地支)
var jiGong = map[string]string{
	"甲": "寅", "乙": "辰", "丙": "巳", "丁": "未", "戊": "巳",
	"己": "未", "庚": "申", "辛": "戌", "壬": "亥", "癸": "丑",
}

// 五行属性表
var wuXing = map[string]string{
	// 天干
	"甲": "木", "乙": "木", "丙": "火", "丁": "火", "戊": "土",
	"己": "土", "庚": "金", "辛": "金", "壬": "水", "癸": "水",
	// 地支
	"子": "水", "丑": "土", "寅": "木", "卯": "木", "辰": "土",
	"巳": "火", "午": "火", "未": "土", "申": "金", "酉": "金",
	"戌": "土", "亥": "水",
}

// 五行相克关系 (克我者)
var wuXingKe = map[string]string{
	"木": "金", "火": "水", "土": "木", "金": "火", "水": "土",
}

func (p *DaLiuRen) Process(event interface{}) {
	msg, ok := event.(handler.OB11GroupMessage)
	if !ok {
		return
	}

	fields := strings.Fields(msg.RawMessage)
	if len(fields) < 2 || strings.ToLower(fields[0]) != "大六壬" {
		return
	}

	question := strings.Join(fields[1:], " ")
	botapi.SendReplyMsg(botapi.GroupMessage, msg.GroupId, msg.MessageId, "收到，正在为您起大六壬课式...")

	// 使用当前时间起课
	currentTime := time.Now()
	ke := p.generateLiuRenKe(question, currentTime)
	if ke == nil {
		botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, "起课失败，可能是日期超出范围或内部错误。")
		return
	}

	// 格式化课式信息，准备发给 AI
	keInfo := p.formatKeInfo(ke)

	// 使用 goroutine 异步调用 AI 并发送结果
	go func(gid int64, info string, botId int64, botName string, userId int64, userName string) {
		interpretation := interpretLiuRen(info)
		if interpretation == "" {
			botapi.SendGroupAtWithTextMsg(gid, userId, "解读课式失败，可能是 AI 服务器繁忙，请稍后再试。")
			return
		}

		// 构建合并转发消息
		var nodes []botapi.ForwardNode
		nodes = append(nodes, botapi.ForwardNode{
			Type: "node", Data: botapi.ForwardNodeData{
				UserID: botId, Nickname: botName, Content: fmt.Sprintf("根据您的问题，得到的大六壬课式如下：\n%s", info),
			},
		})
		nodes = append(nodes, botapi.ForwardNode{
			Type: "node", Data: botapi.ForwardNodeData{
				UserID: botId, Nickname: botName, Content: fmt.Sprintf("【课式解读】\n%s", interpretation),
			},
		})
		nodes = append(nodes, botapi.ForwardNode{
			Type: "node", Data: botapi.ForwardNodeData{
				UserID: botId, Nickname: botName, Content: "小罗纸提醒您：六壬之术，乃观天察地之镜，映照当下因缘际会，而非宿命之锁。卦象如云，变化万千；人生如舟，舵在君手。\n\n莫让片语只言成为心头重负，当以轻松心境面对生活万千。愿此课式为您点亮一盏心灯，助您看清前路，从容抉择。\n\n结果仅供娱乐，祝您心开意解，吉祥随行！",
			},
		})

		// 发送结果
		botapi.SendGroupAtWithTextMsg(gid, userId, "您的大六壬课式已解，请查收")
		botapi.SendGroupForwardMsg(
			gid, nodes,
			botapi.WithSource(fmt.Sprintf("%s 的大六壬占卜", userName)),
		)
	}(msg.GroupId, keInfo, msg.SelfID, "罗纸酱", msg.UserID, msg.NikeName)
}

func (p *DaLiuRen) generateLiuRenKe(question string, currentTime time.Time) *LiuRenKe {
	lunar, err := SolarToLunar(currentTime)
	if err != nil {
		log.Printf("自建日历转换失败: %v", err)
		return nil
	}

	dayGanZhiStr := lunar.GanZhiDay
	zhanShi := lunar.GanZhiTime
	zhanShiIndex := findDiZhiIndex(zhanShi, diZhi)
	month := lunar.Month
	yueJiangName := yueJiang[month-1]

	// 计算天盘
	tianPan := make([]string, 12)
	yueJiangDiPanZhi := strings.Split(yueJiangName, "-")[0]
	yueJiangDiPanIndex := findDiZhiIndex(yueJiangDiPanZhi, diZhi)
	offset := (zhanShiIndex - yueJiangDiPanIndex + 12) % 12
	for i := 0; i < 12; i++ {
		tianPan[i] = diZhi[(i+offset)%12]
	}

	// 计算四课
	siKe := make([]string, 4)
	dayGan := string([]rune(dayGanZhiStr)[0])
	dayZhi := string([]rune(dayGanZhiStr)[1])
	dayGanTianPanZhi := getTianPanZhiForGan(dayGan, diZhi, tianPan)
	dayZhiDiPanIndex := findDiZhiIndex(dayZhi, diZhi)
	siKe[0] = dayGan + dayGanTianPanZhi
	siKe[1] = dayGanTianPanZhi + findDiPanZhiForTianPanZhi(dayGanTianPanZhi, diZhi, tianPan)
	siKe[2] = dayZhi + tianPan[dayZhiDiPanIndex]
	siKe[3] = tianPan[dayZhiDiPanIndex] + findDiPanZhiForTianPanZhi(tianPan[dayZhiDiPanIndex], diZhi, tianPan)

	// 计算三传
	var sanChuan []string
	var qiFa string
	chuChuan, found := findZeKeChuChuan(siKe)
	if found {
		qiFa = "贼克"
		zhongChuan := findTianPanZhiForDiPanZhi(chuChuan, diZhi, tianPan)
		moChuan := findTianPanZhiForDiPanZhi(zhongChuan, diZhi, tianPan)
		sanChuan = []string{chuChuan, zhongChuan, moChuan}
	} else {
		qiFa = "比用"
		chuChuan = string([]rune(siKe[0])[1])
		zhongChuan := findTianPanZhiForDiPanZhi(chuChuan, diZhi, tianPan)
		moChuan := findTianPanZhiForDiPanZhi(zhongChuan, diZhi, tianPan)
		sanChuan = []string{chuChuan, zhongChuan, moChuan}
	}

	return &LiuRenKe{
		Date:      currentTime.Format("2006-01-02 15:04:05"),
		Question:  question,
		YueJiang:  yueJiangName,
		ZhanShi:   zhanShi,
		TianPan:   tianPan,
		SiKe:      siKe,
		SanChuan:  sanChuan,
		QiFa:      qiFa,
		DayGanZhi: dayGanZhiStr,
	}
}

// findDiZhiIndex 查找地支在列表中的索引
func findDiZhiIndex(zhi string, diZhi []string) int {
	for i, z := range diZhi {
		if z == zhi {
			return i
		}
	}
	return -1
}

// findDiPanZhiForTianPanZhi 根据天盘支找地盘支
func findDiPanZhiForTianPanZhi(tianPanZhi string, diZhi, tianPan []string) string {
	for i, z := range tianPan {
		if z == tianPanZhi {
			return diZhi[i%12]
		}
	}
	return ""
}

// findTianPanZhiForDiPanZhi 根据地盘支找天盘支
func findTianPanZhiForDiPanZhi(diPanZhi string, diZhi, tianPan []string) string {
	idx := findDiZhiIndex(diPanZhi, diZhi)
	if idx != -1 && idx < len(tianPan) {
		return tianPan[idx]
	}
	return ""
}

// getTianPanZhiForGan 根据天干获取对应的天盘支
func getTianPanZhiForGan(gan string, diZhi, tianPan []string) string {
	// 使用预定义的寄宫表
	if jigong, exists := jiGong[gan]; exists {
		return findTianPanZhiForDiPanZhi(jigong, diZhi, tianPan)
	}
	return ""
}

// findZeKeChuChuan 查找贼克初传
func findZeKeChuChuan(siKe []string) (string, bool) {
	// 检查四课中是否有上克下或下贼上的关系
	for _, ke := range siKe {
		if len(ke) < 2 {
			continue
		}

		// 获取上下神的五行属性
		xia := string([]rune(ke)[0])
		shang := string([]rune(ke)[1])

		xiaWuXing, xiaExists := wuXing[xia]
		shangWuXing, shangExists := wuXing[shang]

		if !xiaExists || !shangExists {
			continue
		}

		// 检查下贼上 (下克上)
		if wuXingKe[xiaWuXing] == shangWuXing {
			return shang, true
		}

		// 检查上克下 (上克下)
		if wuXingKe[shangWuXing] == xiaWuXing {
			return shang, true
		}
	}
	return "", false
}

// formatKeInfo 格式化课式信息
func (p *DaLiuRen) formatKeInfo(ke *LiuRenKe) string {
	return fmt.Sprintf(
		"公历时间：%s\n问题：%s\n日干支：%s\n月将：%s\n占时：%s\n起法：%s\n四课：%s\n三传：初传-%s, 中传-%s, 末传-%s\n天盘：%s",
		ke.Date, ke.Question, ke.DayGanZhi, ke.YueJiang, ke.ZhanShi, ke.QiFa,
		strings.Join(ke.SiKe, " "), ke.SanChuan[0], ke.SanChuan[1], ke.SanChuan[2], strings.Join(ke.TianPan, " "),
	)
}

// 调用AI解读大六壬课式
func interpretLiuRen(keInfo string) string {
	apiKey := viper.GetString("bot.function.daliuren.api_key")
	apiURL := "https://api.deepseek.com/chat/completions"

	systemPrompt := `你是一位深谙《大六壬》的解课大师。
你的任务是根据用户提供的课式信息（包含时间、问题、日干支、月将、占时、起法、四课、三传、天盘），进行一次完整、专业且清晰易懂的解读。

**输出格式与解读逻辑，必须严格遵循以下规则：**

1.  **【格式要求】**
    -   **绝对禁止**使用任何 Markdown 格式 (如 ` + "``, **, #, -, >" + ` 等)。
    -   使用换行来分隔段落，保持排版清晰。
    -   **不要复述**用户已经提供给你的课式信息，直接开始解读。

2.  **【核心解读逻辑 (必须按此顺序)】**
    -   **第一步：分析日干支与四课**
        -   分析日干的五行属性和旺相休囚状态。
        -   分析日支与日干的关系，判断日柱的吉凶。
        -   分析四课之间的关系，找出受克、相生、相合的课体。

    -   **第二步：分析三传**
        -   分析初传、中传、末传的五行属性和相互关系。
        -   根据三传的顺逆、进退、空亡等情况，判断事情的发展过程。
        -   特别关注末传，它代表事情的最终结果。

    -   **第三步：分析天盘与月将**
        -   分析天盘中各支的关系，找出贵人、青龙、白虎等神煞的位置。
        -   分析月将与占时的关系，判断课式的整体格局。

    -   **第四步：综合结论与建议**
        -   **总结**：综合日干支、四课、三传、天盘的分析，对用户提出的**具体问题**给出一个明确的结论。
        -   **建议**：基于整个课式的启示，为用户提供1-2条切实可行的行动建议或注意事项。
        -   给出最准确的结论，无论结局是好是坏，不要给出模棱两可的回答，要给出明确的吉凶结论。
        -   结论和建议不要使用太专业的术语，使用通俗易懂的语言。

3.  **【语言风格】**
    -   请使用一位智慧、沉稳且友善的解课师的口吻。
    -   语言要通俗易懂，避免使用过于晦涩的术语，但在关键之处要体现专业性。
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
				Content: keInfo,
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
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

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

func init() {
	registry.RegisterFactory(
		registry.CreatePluginFactory(&DaLiuRen{}, "features.daliuren.enabled", true),
	)
}
