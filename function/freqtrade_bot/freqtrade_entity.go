package freqtrade_bot

import (
	"fmt"
	"strings"
	"time"
)

// FreqtradeTime 自定义时间类型
type FreqtradeTime struct {
	time.Time
}

// UnmarshalJSON 实现自定义时间类型的接口
func (ft *FreqtradeTime) UnmarshalJSON(b []byte) error {
	// Freqtrade API 返回的时间格式为 "YYYY-MM-DD HH:MM:SS"
	// Go语言的标准时间格式是 "2006-01-02 15:04:05"
	s := strings.Trim(string(b), "\"")
	if s == "null" {
		return nil
	}
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return fmt.Errorf("could not parse time: %w", err)
		}
	}
	ft.Time = t
	return nil
}

// ==============================================================================
//  /show_config - Show Bot Configuration (显示机器人配置)
// ==============================================================================

type ShowConfigResponse struct {
	BotName       string   `json:"bot_name"`
	Strategy      string   `json:"strategy"`
	StakeCurrency string   `json:"stake_currency"`
	StakeAmount   string   `json:"stake_amount"` // 注意：这里可能是 "unlimited" 或数字，所以用 string
	MaxOpenTrades float64  `json:"max_open_trades"`
	TradingMode   string   `json:"trading_mode"`
	MarginMode    string   `json:"margin_mode"`
	IsDryRun      bool     `json:"dry_run"`
	PairWhitelist []string `json:"pair_whitelist"`
	Exchange      string   `json:"exchange"`
}

// ==============================================================================
//  /health - Bot Health (机器人健康状况)
// ==============================================================================

type HealthResponse struct {
	LastProcess FreqtradeTime `json:"last_process"` // 上次主循环时间
	Status      string        `json:"status"`       // 状态，例如 "running"
}

// ==============================================================================
//  /status - Open Trades (当前持仓)
// ==============================================================================

type Trade struct {
	TradeID                       int           `json:"trade_id"`
	Pair                          string        `json:"pair"`
	Amount                        float64       `json:"amount"`
	StakeAmount                   float64       `json:"stake_amount"`
	OpenRate                      float64       `json:"open_rate"`
	OpenDate                      FreqtradeTime `json:"open_date"`
	CurrentRate                   float64       `json:"current_rate"`
	CurrentProfit                 float64       `json:"current_profit"`
	CurrentProfitAbs              float64       `json:"current_profit_abs"`
	ProfitPercentage              float64       `json:"profit_pct"`            // 与 current_profit 相同
	ProfitAbsolute                float64       `json:"profit_abs"`            // 与 current_profit_abs 相同
	StopLossAbsolute              float64       `json:"stop_loss_abs"`         // 止损价格
	StopLossPercentage            float64       `json:"stop_loss_pct"`         // 止损百分比 (相对于开仓价)
	LiquidationPrice              float64       `json:"liquidation_price"`     // 预估爆仓价 (仅期货)
	InitialStopLoss               float64       `json:"initial_stop_loss_abs"` // 初始止损价
	InitialStopLossPercentage     float64       `json:"initial_stop_loss_pct"`
	StoplossCurrentDist           float64       `json:"stoploss_current_dist"` // 当前价格与止损价的距离
	StoplossCurrentDistPercentage float64       `json:"stoploss_current_dist_pct"`
	StoplossEntryPoint            float64       `json:"stoploss_entry_dist"` // 开仓价与止损价的距离
	StoplossEntryPointPercentage  float64       `json:"stoploss_entry_dist_pct"`
	OpenOrder                     string        `json:"open_order"` // 如果有挂单，显示其订单ID
	IsShort                       bool          `json:"is_short"`   // 是否是空头仓位
	Leverage                      float64       `json:"leverage"`   // 杠杆倍数
	InterestRate                  float64       `json:"interest_rate"`
	EnterTag                      string        `json:"enter_tag"` // 开仓标签
}

// StatusResponse is the response from the /status endpoint.
type StatusResponse []Trade

// ==============================================================================
//  /trades - Trade History (历史交易)
// ==============================================================================

// HistoricalTrade represents a single trade object from the /trades endpoint.
type HistoricalTrade struct {
	TradeID                   int           `json:"trade_id"`
	Pair                      string        `json:"pair"`
	StakeAmount               float64       `json:"stake_amount"`
	Amount                    float64       `json:"amount"`
	IsShort                   bool          `json:"is_short"`
	Leverage                  float64       `json:"leverage"`
	OpenRate                  float64       `json:"open_rate"`
	OpenDate                  FreqtradeTime `json:"open_date"`
	CloseRate                 float64       `json:"close_rate"`
	CloseDate                 FreqtradeTime `json:"close_date"`
	CloseProfit               float64       `json:"close_profit"`
	CloseProfitAbs            float64       `json:"close_profit_abs"`
	ExitReason                string        `json:"exit_reason"`
	EnterTag                  string        `json:"enter_tag"`
	StopLossAbsolute          float64       `json:"stop_loss_abs"`
	StopLossPercentage        float64       `json:"stop_loss_pct"`
	InitialStopLossAbsolute   float64       `json:"initial_stop_loss_abs"`
	InitialStopLossPercentage float64       `json:"initial_stop_loss_pct"`
	OpenFee                   float64       `json:"open_fee"`
	OpenFeeCost               float64       `json:"open_fee_cost"`
	OpenFeeCurrency           string        `json:"open_fee_currency"`
	CloseFee                  float64       `json:"close_fee"`
	CloseFeeCost              float64       `json:"close_fee_cost"`
	CloseFeeCurrency          string        `json:"close_fee_currency"`
}

// TradesResponse is the response from the /trades endpoint.
type TradesResponse struct {
	Trades      []HistoricalTrade `json:"trades"`
	TradesCount int               `json:"trades_count"`
}

// ==============================================================================
//  /profit - Profit Summary (盈利总结)
// ==============================================================================

// ProfitSummary represents the data from the /profit endpoint.
type ProfitSummary struct {
	ProfitClosedCount          int           `json:"profit_closed_count"`
	ProfitClosedSum            float64       `json:"profit_closed_sum"`
	ProfitClosedSumPercentage  float64       `json:"profit_closed_sum_pct"`
	ProfitAllCount             int           `json:"profit_all_count"`
	ProfitAllSum               float64       `json:"profit_all_sum"`
	ProfitAllSumPercentage     float64       `json:"profit_all_sum_pct"`
	BestPair                   string        `json:"best_pair"`
	BestPairProfit             float64       `json:"best_pair_profit"`
	BestPairProfitPercentage   float64       `json:"best_pair_profit_pct"`
	WorstPair                  string        `json:"worst_pair"`
	WorstPairProfit            float64       `json:"worst_pair_profit"`
	WorstPairProfitPercentage  float64       `json:"worst_pair_profit_pct"`
	FirstTradeOpen             FreqtradeTime `json:"first_trade_open"`
	LatestTradeOpen            FreqtradeTime `json:"latest_trade_open"`
	TradesCount                int           `json:"trades_count"`
	ClosedTradesCount          int           `json:"closed_trades_count"`
	WinningTrades              int           `json:"winning_trades"`
	LosingTrades               int           `json:"losing_trades"`
	HoldingAvg                 string        `json:"holding_avg"` // 平均持仓时间，格式如 "1:23:45"
	WinRate                    float64       `json:"win_rate"`
	BestTradeProfit            float64       `json:"best_trade_profit"`
	BestTradeProfitPercentage  float64       `json:"best_trade_profit_pct"`
	WorstTradeProfit           float64       `json:"worst_trade_profit"`
	WorstTradeProfitPercentage float64       `json:"worst_trade_profit_pct"`
	ProfitFactor               float64       `json:"profit_factor"`
	TotalStake                 float64       `json:"total_stake"`
}

// ==============================================================================
//  /daily - Daily Profit Stats (每日盈利统计)
// ==============================================================================

// DailyProfit represents a single day's profit statistics.
type DailyProfit struct {
	Date       string  `json:"date"` // 日期字符串，格式 "YYYY-MM-DD"
	AbsProfit  float64 `json:"abs_profit"`
	FiatValue  float64 `json:"fiat_value"`
	TradeCount int     `json:"trade_count"`
}

// DailyResponse is the response from the /daily endpoint.
type DailyResponse struct {
	Data []DailyProfit `json:"data"`
}

// ==============================================================================
//  /balance - Balance (账户余额)
// ==============================================================================

// CurrencyBalance 代表 balance 响应中 "currencies" 数组里的单个货币对象
type CurrencyBalance struct {
	Currency  string  `json:"currency"`
	Balance   float64 `json:"balance"`
	Available float64 `json:"available"`
	Locked    float64 `json:"locked"`
}

// BalanceResponse 是 /balance 接口返回的完整顶层对象结构
type BalanceResponse struct {
	Currencies []CurrencyBalance `json:"currencies"` // 这是一个包含货币详情的数组
	Total      float64           `json:"total"`      // 以 stake 货币计价的总价值
	Stake      string            `json:"stake"`      // 计价货币, e.g., "USDT"
	Note       string            `json:"note"`
}

// ==============================================================================
//  /count - Open Trades Count (持仓数量)
// ==============================================================================

// CountResponse is the response from the /count endpoint.
type CountResponse struct {
	Current int `json:"current"`
	Max     int `json:"max"`
	Total   int `json:"total_stake"`
}

// ==============================================================================
//  /performance - Performance Stats (表现统计)
// ==============================================================================

// PairPerformance represents a single pair's performance statistics.
type PairPerformance struct {
	Pair   string  `json:"pair"`
	Profit float64 `json:"profit_sum"`
	Count  int     `json:"count"`
}

// PerformanceResponse is the response from the /performance endpoint.
type PerformanceResponse []PairPerformance

// ==============================================================================
//  /version - Bot Version (机器人版本)
// ==============================================================================

// VersionResponse is the response from the /version endpoint.
type VersionResponse struct {
	Version string `json:"version"`
}
