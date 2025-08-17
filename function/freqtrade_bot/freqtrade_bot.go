package freqtrade_bot

import (
	"bytes"
	"fmt"
	"github.com/XiaoLuozee/go-bot/botapi"
	"github.com/XiaoLuozee/go-bot/handler"
	"github.com/XiaoLuozee/go-bot/registry"
	"github.com/spf13/viper"
	"sort"
	"strconv"
	"strings"
	"time"
)

type FreqtradeBotPlugin struct{}

func (f FreqtradeBotPlugin) Process(event interface{}) {
	// 检查事件类型是否为私聊消息
	msg, ok := event.(handler.OB11PrivateMessage)
	if !ok {
		return
	}
	fields := strings.Fields(msg.RawMessage)
	if len(fields) < 1 || strings.ToLower(fields[0]) != "量化" {
		return
	}

	master := viper.GetInt64("bot.master")

	if msg.UserID != master {
		botapi.SendTextMsg(botapi.PrivateMessage, msg.UserID, "❌ 抱歉，您没有权限执行此操作")
		return
	}

	var command string
	var args []string
	if len(fields) > 1 {
		command = fields[1]
	}
	if len(fields) > 2 {
		args = fields[2:]
	}

	apiUri := viper.GetString("bot.function.freqtrade.api_url")
	username := viper.GetString("bot.function.freqtrade.username")
	password := viper.GetString("bot.function.freqtrade.password")
	if apiUri == "" || username == "" || password == "" {
		botapi.SendTextMsg(botapi.PrivateMessage, msg.UserID, "❌ Freqtrade API 配置不完整，请检查配置文件")
		return
	}
	client := NewClient(apiUri, username, password)

	switch command {
	case "状态", "status":
		handleGetStatus(client, msg.UserID)
	case "余额", "balance":
		handleGetBalance(client, msg.UserID)
	case "盈利", "profit":
		handleGetProfit(client, msg.UserID)
	case "历史", "trades":
		handleGetTrades(client, msg.UserID, args)
	case "日报", "daily":
		handleGetDailyStats(client, msg.UserID, args)
	case "平仓", "forceexit":
		handleForceExit(client, msg.UserID, args)
	case "启动", "start":
		handleStartBot(client, msg.UserID)
	case "停止", "stop":
		handleStopBot(client, msg.UserID)
	case "重载", "reload":
		handleReloadConfig(client, msg.UserID)
	case "表现", "performance":
		handleGetPerformance(client, msg.UserID)
	case "配置", "config":
		handleShowConfig(client, msg.UserID)
	case "健康", "health":
		handleHealth(client, msg.UserID)
	case "版本", "version":
		handleGetVersion(client, msg.UserID)
	case "帮助", "help", "":
		handleHelp(msg.UserID)
	default:
		botapi.SendTextMsg(botapi.PrivateMessage, msg.UserID, fmt.Sprintf("❓ 未知指令: '%s'。发送“量化 帮助”查看可用指令。", command))
	}
}

// 打印帮助信息
func handleHelp(userId int64) {
	helpText := `--- 小罗纸量化策略 Bot ---
量化 状态        - 查询当前持仓
量化 余额        - 查询账户余额
量化 盈利        - 查询盈利总结
量化 历史 [N]    - 查询最近N笔交易(默认10)
量化 日报 [N]    - 查询过去N天日报(默认7)
量化 平仓 [ID]   - 强制平仓指定ID的交易
量化 启动        - 启动机器人
量化 停止        - 停止机器人
量化 重载        - 重新加载配置
量化 表现        - 查询各交易对表现
量化 配置        - 查询机器人的运行配置
量化 健康        - 查询机器人的健康状态
量化 版本        - 显示Freqtrade版本
量化 帮助        - 显示本帮助信息`
	botapi.SendTextMsg(botapi.PrivateMessage, userId, helpText)
}

// 查询余额信息
func handleGetBalance(client *Client, userId int64) {
	balanceData, err := client.QueryBalance() // balanceData 现在是 *BalanceResponse 类型
	if err != nil {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, fmt.Sprintf("❌ 查询余额失败: %v", err))
		return
	}

	currenciesSlice := balanceData.Currencies // **关键：我们操作的是内部的 Currencies 切片**

	if len(currenciesSlice) == 0 {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, "ℹ️ 账户中没有任何资产")
		return
	}

	var sb strings.Builder
	sb.WriteString("💰 账户余额详情\n")
	sb.WriteString("--------------------\n")

	sort.Slice(currenciesSlice, func(i, j int) bool {
		priority := map[string]int{"USDT": 1, "BTC": 2, "ETH": 3}
		p1, ok1 := priority[currenciesSlice[i].Currency]
		p2, ok2 := priority[currenciesSlice[j].Currency]
		if ok1 && ok2 {
			return p1 < p2
		}
		if ok1 {
			return true
		}
		if ok2 {
			return false
		}
		return currenciesSlice[i].Currency < currenciesSlice[j].Currency
	})

	for _, details := range currenciesSlice {
		if details.Balance > 0.0001 {
			sb.WriteString(fmt.Sprintf("%s: %.4f (可用: %.4f)\n",
				details.Currency,
				details.Balance,
				details.Available,
			))
		}
	}

	sb.WriteString("--------------------\n")
	sb.WriteString(fmt.Sprintf("💰 总价值: %.2f %s", balanceData.Total, balanceData.Stake))

	botapi.SendTextMsg(botapi.PrivateMessage, userId, sb.String())
}

// 查询持仓状态
func handleGetStatus(client *Client, userId int64) {
	status, err := client.QueryStatus()

	if err != nil {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, fmt.Sprintf("❌ 查询状态时发生错误：%s", err))
		return
	}
	if len(status) == 0 {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, "✅ 当前没有任何持仓")
		return
	}
	var nodes []botapi.ForwardNode

	headerContent := fmt.Sprintf(
		"📊 当前持仓总览 (共 %d 笔)\n数据更新于: %s",
		len(status),
		time.Now().Format("2006-01-02 15:04:05"),
	)
	headerNode := botapi.ForwardNode{
		Type: "node",
		Data: botapi.ForwardNodeData{
			UserID:   userId,
			Nickname: "当前持仓状态",
			Content:  headerContent,
		},
	}
	nodes = append(nodes, headerNode)

	for _, trade := range status {
		node := buildStatusNode(trade, userId)
		nodes = append(nodes, node)
	}

	botapi.SendPrivateForwardMsg(userId, nodes)
}

// buildStatusNode 构建状态节点
func buildStatusNode(trade Trade, userId int64) botapi.ForwardNode {
	var contentBuilder bytes.Buffer
	var directionEmoji, directionText string
	if trade.IsShort {
		directionEmoji = "📉"
		directionText = "空头"
	} else {
		directionEmoji = "📈"
		directionText = "多头"
	}

	// 交易对 (方向 | 杠杆)
	contentBuilder.WriteString(fmt.Sprintf(
		"%s %s (%s | %.0fx)\n",
		directionEmoji,
		trade.Pair,
		directionText,
		trade.Leverage,
	))

	// 盈亏状态
	var profitEmoji, sign string
	if trade.ProfitAbsolute >= 0 {
		profitEmoji = "🟢"
		sign = "+"
	} else {
		profitEmoji = "🔴"
		sign = ""
	}
	// 浮动盈亏行
	contentBuilder.WriteString(fmt.Sprintf(
		"%s 浮动盈亏: %s%.2f USDT (%s%.2f%%)\n",
		profitEmoji,
		sign, trade.ProfitAbsolute,
		sign, trade.ProfitPercentage,
	))

	// 价格信息
	contentBuilder.WriteString("--------------------\n")
	contentBuilder.WriteString(fmt.Sprintf("开仓价格: %.4f\n", trade.OpenRate))
	contentBuilder.WriteString(fmt.Sprintf("当前价格: %.4f\n", trade.CurrentRate))
	// 爆仓价只有在大于0时才有意义
	if trade.LiquidationPrice > 0 {
		contentBuilder.WriteString(fmt.Sprintf("预估爆仓: %.4f 💥\n", trade.LiquidationPrice))
	}

	// 风险控制
	// 止损价大于0时才显示
	if trade.StopLossAbsolute > 0 {
		contentBuilder.WriteString(fmt.Sprintf(
			"当前止损: %.4f (距: %.2f%%)\n",
			trade.StopLossAbsolute,
			trade.StoplossCurrentDistPercentage,
		))
	}

	// 时间与ID
	contentBuilder.WriteString("--------------------\n")
	contentBuilder.WriteString(fmt.Sprintf("开仓时间: %s\n", trade.OpenDate.Format("01-02 15:04")))
	contentBuilder.WriteString(fmt.Sprintf("交易ID: #%d", trade.TradeID))
	// 如果有开仓标签，也显示出来
	if trade.EnterTag != "" {
		contentBuilder.WriteString(fmt.Sprintf(" | 标签: %s", trade.EnterTag))
	}

	// 返回构造好的 ForwardNode
	return botapi.ForwardNode{
		Type: "node",
		Data: botapi.ForwardNodeData{
			UserID:   userId,       // 你的机器人QQ号
			Nickname: "小罗纸量化策略Bot", // 机器人的专属昵称
			Content:  contentBuilder.String(),
		},
	}
}

// 查询盈利状态
func handleGetProfit(client *Client, userId int64) {
	profit, err := client.QueryProfit()
	if err != nil {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, fmt.Sprintf("❌ 查询盈利失败: %v", err))
		return
	}

	var sb strings.Builder
	sb.WriteString("🏆 盈利总结报告\n")
	sb.WriteString("------------------------------\n")
	sb.WriteString(fmt.Sprintf("📊 总览 (含浮动盈亏):\n"))
	sb.WriteString(fmt.Sprintf("  - 交易数: %d\n", profit.ProfitAllCount))
	sb.WriteString(fmt.Sprintf("  - 总盈利: %.2f USDT (%.2f%%)\n", profit.ProfitAllSum, profit.ProfitAllSumPercentage))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("✅ 已平仓:\n"))
	sb.WriteString(fmt.Sprintf("  - 交易数: %d\n", profit.ClosedTradesCount))
	sb.WriteString(fmt.Sprintf("  - 总盈利: %.2f USDT (%.2f%%)\n", profit.ProfitClosedSum, profit.ProfitClosedSumPercentage))
	sb.WriteString(fmt.Sprintf("  - 胜率: %.2f%%\n", profit.WinRate*100))
	sb.WriteString(fmt.Sprintf("  - 胜 / 负: %d / %d\n", profit.WinningTrades, profit.LosingTrades))
	sb.WriteString(fmt.Sprintf("  - 盈亏因子: %.2f\n", profit.ProfitFactor))
	sb.WriteString(fmt.Sprintf("  - 平均持仓: %s\n", profit.HoldingAvg))
	sb.WriteString("\n")
	sb.WriteString("🌟 最佳 & 最差:\n")
	sb.WriteString(fmt.Sprintf("  - 最佳交易对: %s (+%.2f%%)\n", profit.BestPair, profit.BestPairProfitPercentage))
	sb.WriteString(fmt.Sprintf("  - 最差交易对: %s (%.2f%%)\n", profit.WorstPair, profit.WorstPairProfitPercentage))
	sb.WriteString(fmt.Sprintf("  - 单笔最佳盈利: +%.2f USDT (%.2f%%)\n", profit.BestTradeProfit, profit.BestTradeProfitPercentage))
	sb.WriteString(fmt.Sprintf("  - 单笔最差亏损: %.2f USDT (%.2f%%)\n", profit.WorstTradeProfit, profit.WorstTradeProfitPercentage))
	sb.WriteString("------------------------------\n")
	if !profit.FirstTradeOpen.IsZero() {
		sb.WriteString(fmt.Sprintf("🕒 首次开仓: %s", profit.FirstTradeOpen.Format("2006-01-02 15:04")))
	} else {
		sb.WriteString("🕒 首次开仓: 等待第一笔交易...")
	}

	botapi.SendTextMsg(botapi.PrivateMessage, userId, sb.String())
}

// 查询历史交易记录
func handleGetTrades(client *Client, userId int64, args []string) {
	limit := 5 // 默认查询5条
	if len(args) > 0 {
		if l, err := strconv.Atoi(args[0]); err == nil && l > 0 {
			limit = l
		}
	}

	tradesData, err := client.QueryTrades(limit)
	if err != nil {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, fmt.Sprintf("❌ 查询历史交易失败: %v", err))
		return
	}

	if tradesData.TradesCount == 0 {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, "📜 没有历史交易记录。")
		return
	}

	var nodes []botapi.ForwardNode
	// 创建头节点
	nodes = append(nodes, botapi.ForwardNode{
		Type: "node", Data: botapi.ForwardNodeData{
			UserID: userId, Nickname: "历史交易报告",
			Content: fmt.Sprintf("📜 最近 %d 笔已平仓交易 (共%d笔)", len(tradesData.Trades), tradesData.TradesCount),
		},
	})

	// 为每条历史交易创建 node
	for _, trade := range tradesData.Trades {
		nodes = append(nodes, buildHistoricalTradeNode(trade, userId))
	}
	botapi.SendPrivateForwardMsg(userId, nodes)
}

// buildHistoricalTradeNode 为 历史 指令创建节点的辅助函数
func buildHistoricalTradeNode(trade HistoricalTrade, userId int64) botapi.ForwardNode {
	var sb bytes.Buffer
	var directionEmoji, directionText, profitEmoji, sign string
	if trade.IsShort {
		directionEmoji = "📉"
		directionText = "空"
	} else {
		directionEmoji = "📈"
		directionText = "多"
	}
	if trade.CloseProfit >= 0 {
		profitEmoji = "🟢"
		sign = "+"
	} else {
		profitEmoji = "🔴"
		sign = ""
	}

	sb.WriteString(fmt.Sprintf("%s %s (%.0fx) (%s)\n", directionEmoji, trade.Pair, trade.Leverage, directionText))
	sb.WriteString(fmt.Sprintf("%s 最终盈亏: %s%.2f USDT (%.2f%%)\n", profitEmoji, sign, trade.CloseProfitAbs, trade.CloseProfit*100))
	sb.WriteString("--------------------\n")
	sb.WriteString(fmt.Sprintf("开仓: %.4f\n", trade.OpenRate))
	sb.WriteString(fmt.Sprintf("平仓: %.4f (%s)\n", trade.CloseRate, trade.ExitReason))
	sb.WriteString(fmt.Sprintf("时间: %s ~ %s", trade.OpenDate.Format("01-02 15:04"), trade.CloseDate.Format("01-02 15:04")))

	return botapi.ForwardNode{
		Type: "node", Data: botapi.ForwardNodeData{
			UserID: userId, Nickname: "小罗纸量化策略Bot", Content: sb.String(),
		},
	}
}

// 查询日报
func handleGetDailyStats(client *Client, userId int64, args []string) {
	days := 7 // 默认查询7天
	if len(args) > 0 {
		if d, err := strconv.Atoi(args[0]); err == nil && d > 0 {
			days = d
		}
	}
	stats, err := client.QueryDailyStats(days)
	if err != nil {
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📅 过去 %d 天每日统计\n", len(stats.Data)))
	sb.WriteString("--------------------\n")
	totalProfit := 0.0
	for _, day := range stats.Data {
		var profitEmoji string
		if day.AbsProfit >= 0 {
			profitEmoji = "🟢"
		} else {
			profitEmoji = "🔴"
		}
		sb.WriteString(fmt.Sprintf("%s %s: %.2f USDT (%d笔)\n", profitEmoji, day.Date, day.AbsProfit, day.TradeCount))
		totalProfit += day.AbsProfit
	}
	sb.WriteString("--------------------\n")
	sb.WriteString(fmt.Sprintf("总计: %.2f USDT", totalProfit))
	botapi.SendTextMsg(botapi.PrivateMessage, userId, sb.String())
}

// 查询表现
func handleGetPerformance(client *Client, userId int64) {
	performance, err := client.QueryPerformance()
	if err != nil { /* 错误处理 */
		return
	}

	var sb strings.Builder
	sb.WriteString("🚀 各交易对表现\n--------------------\n")
	for _, p := range performance {
		var profitEmoji string
		if p.Profit >= 0 {
			profitEmoji = "🟢"
		} else {
			profitEmoji = "🔴"
		}
		sb.WriteString(fmt.Sprintf("%s %s: %.2f USDT (%d笔)\n", profitEmoji, p.Pair, p.Profit, p.Count))
	}
	botapi.SendTextMsg(botapi.PrivateMessage, userId, sb.String())
}

// 查看 Freqtrade 版本
func handleGetVersion(client *Client, userId int64) {
	version, err := client.QueryVersion()
	if err != nil { /* 错误处理 */
		return
	}

	text := fmt.Sprintf("🤖 Freqtrade 版本: %s", version.Version)
	botapi.SendTextMsg(botapi.PrivateMessage, userId, text)
}

// 强制平仓
func handleForceExit(client *Client, userId int64, args []string) {
	if len(args) == 0 {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, "请提供要平仓的交易ID，例如: 量化 平仓 123")
		return
	}
	tradeID, err := strconv.Atoi(args[0])
	if err != nil {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, "交易ID必须是数字")
		return
	}

	resp, err := client.ForceExit(tradeID, "market") // 默认使用市价平仓
	if err != nil {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, fmt.Sprintf("❌ 平仓失败: %v", err))
		return
	}
	botapi.SendTextMsg(botapi.PrivateMessage, userId, fmt.Sprintf("✅ 已发送平仓指令 (ID: %d)，结果: %s", tradeID, resp.Result))
}

// 启动交易机器人
func handleStartBot(client *Client, userId int64) {
	resp, err := client.StartBot()
	if err != nil {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, fmt.Sprintf("交易机器人启动失败: %s", err))
		return
	}
	botapi.SendTextMsg(botapi.PrivateMessage, userId, fmt.Sprintf("✅ 启动指令已发送，状态: %s", resp["status"]))
}

// 停止交易机器人
func handleStopBot(client *Client, userId int64) {
	resp, err := client.StopBot()
	if err != nil {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, fmt.Sprintf("交易机器人停止失败: %s", err))
		return
	}
	botapi.SendTextMsg(botapi.PrivateMessage, userId, fmt.Sprintf("✅ 停止指令已发送，状态: %s", resp["status"]))
}

// 重载配置
func handleReloadConfig(client *Client, userId int64) {
	resp, err := client.ReloadConfig()
	if err != nil { /* 错误处理 */
		return
	}
	botapi.SendTextMsg(botapi.PrivateMessage, userId, fmt.Sprintf("✅ 重载配置指令已发送，状态: %v", resp["status"]))
}

// 查询机器人运行配置
func handleShowConfig(client *Client, userId int64) {
	config, err := client.QueryConfig()
	if err != nil {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, fmt.Sprintf("❌ 查询配置失败: %v", err))
		return
	}

	var sb strings.Builder
	sb.WriteString("⚙️ 机器人当前运行配置\n")
	sb.WriteString("--------------------------\n")
	sb.WriteString(fmt.Sprintf("- 机器人名称: %s\n", config.BotName))
	sb.WriteString(fmt.Sprintf("- 策略: %s\n", config.Strategy))
	sb.WriteString(fmt.Sprintf("- 模式: %s (逐仓)\n", config.TradingMode))
	sb.WriteString(fmt.Sprintf("- 交易所: %s\n", config.Exchange))
	sb.WriteString(fmt.Sprintf("- 模拟盘: %t\n", config.IsDryRun))
	sb.WriteString(fmt.Sprintf("- 计价货币: %s\n", config.StakeCurrency))
	sb.WriteString(fmt.Sprintf("- 每单金额: %s\n", config.StakeAmount))
	sb.WriteString(fmt.Sprintf("- 最大持仓数: %.0f\n", config.MaxOpenTrades))
	sb.WriteString(fmt.Sprintf("- 交易对白名单: [%s]", strings.Join(config.PairWhitelist, ", ")))

	botapi.SendTextMsg(botapi.PrivateMessage, userId, sb.String())
}

// 查询健康状态
func handleHealth(client *Client, userId int64) {
	health, err := client.QueryHealth()
	if err != nil {
		botapi.SendTextMsg(botapi.PrivateMessage, userId, fmt.Sprintf("❌ 查询健康状况失败: %v", err))
		return
	}

	var emoji string
	// 检查上次心跳时间是否在最近2倍的 timeframe 内
	if time.Since(health.LastProcess.Time) < 10*time.Minute {
		emoji = "✅" // 绿色
	} else {
		emoji = "⚠️" // 黄色警告
	}

	text := fmt.Sprintf("%s 机器人健康状况: %s\n上次心跳: %s",
		emoji,
		health.Status,
		health.LastProcess.Time.Format("2006-01-02 15:04:05"),
	)
	botapi.SendTextMsg(botapi.PrivateMessage, userId, text)
}

func init() {
	registry.RegisterFactory(
		registry.CreatePluginFactory(&FreqtradeBotPlugin{}, "bot.function.freqtrade.enabled", false),
	)
}
