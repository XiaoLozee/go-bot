package freqtrade_bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Client 是与 Freqtrade API 交互的客户端
type Client struct {
	BaseURL    string
	Username   string
	Password   string
	HTTPClient *http.Client
}

// NewClient 是 Client 的构造函数
func NewClient(baseURL, username, password string) *Client {
	return &Client{
		BaseURL:  baseURL,
		Username: username,
		Password: password,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second, // 设置10秒超时
		},
	}
}

// ==============================================================================
//  内部请求处理 (升级版)
// ==============================================================================

func (c *Client) doRequest(method, endpoint string, requestBody interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if requestBody != nil {
		// 将 Go 结构体或 map 编码为 JSON
		jsonBody, err := json.Marshal(requestBody)
		if err != nil {
			return nil, fmt.Errorf("编码请求体为 JSON 失败: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonBody)
	}

	// 创建请求
	req, err := http.NewRequest(method, c.BaseURL+"/api/v1"+endpoint, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置 HTTP Basic Authentication
	req.SetBasicAuth(c.Username, c.Password)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// 发送请求
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 API 失败: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// 读取响应体
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 返回错误状态: %s, 响应: %s", resp.Status, string(responseBody))
	}

	return responseBody, nil
}

// ==============================================================================
//  只读 (GET) API 封装
// ==============================================================================

// QueryBalance 查询账户余额 (最终修正版)
func (c *Client) QueryBalance() (*BalanceResponse, error) { // 返回值是指针
	body, err := c.doRequest("GET", "/balance", nil)
	if err != nil {
		return nil, err
	}

	var balanceResp BalanceResponse // 声明一个结构体变量

	if err := json.Unmarshal(body, &balanceResp); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}

	return &balanceResp, nil // 返回这个结构体的指针
}

// QueryStatus 查询当前所有持仓
func (c *Client) QueryStatus() (StatusResponse, error) {
	body, err := c.doRequest("GET", "/status", nil)
	if err != nil {
		return nil, err
	}
	var resp StatusResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}
	return resp, nil
}

// QueryProfit 查询盈利总结
func (c *Client) QueryProfit() (*ProfitSummary, error) {
	body, err := c.doRequest("GET", "/profit", nil)
	if err != nil {
		return nil, err
	}
	var resp ProfitSummary
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}
	return &resp, nil
}

// QueryTrades 查询历史交易
// limit: 返回最近的 N 笔交易, 0 表示默认 (最多500)
func (c *Client) QueryTrades(limit int) (*TradesResponse, error) {
	endpoint := "/trades"
	if limit > 0 {
		endpoint = fmt.Sprintf("/trades?limit=%d", limit)
	}

	body, err := c.doRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	var resp TradesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}
	return &resp, nil
}

// QueryDailyStats 查询每日盈利统计
// days: 返回过去 N 天的数据
func (c *Client) QueryDailyStats(days int) (*DailyResponse, error) {
	endpoint := fmt.Sprintf("/daily?days=%d", days)

	body, err := c.doRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	var resp DailyResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}
	return &resp, nil
}

// ==============================================================================
//  写操作 (POST) API 封装
// ==============================================================================

// ForceExitResponse 是 /forceexit 接口的响应结构
type ForceExitResponse struct {
	Result string `json:"result"`
}

// ForceExit 强制平仓一个指定的交易
// tradeID: 交易的ID
// orderType: "market" 或 "limit" (如果为"", 则使用配置中的默认值)
func (c *Client) ForceExit(tradeID int, orderType string) (*ForceExitResponse, error) {
	// 准备请求体
	requestBody := map[string]interface{}{
		"tradeid": strconv.Itoa(tradeID), // API 要求 tradeid 是字符串
	}
	if orderType != "" {
		requestBody["ordertype"] = orderType
	}

	body, err := c.doRequest("POST", "/forceexit", requestBody)
	if err != nil {
		return nil, err
	}

	var resp ForceExitResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}
	return &resp, nil
}

// StartBot 启动机器人 (如果处于停止状态)
func (c *Client) StartBot() (map[string]string, error) {
	body, err := c.doRequest("POST", "/start", nil)
	if err != nil {
		return nil, err
	}
	var resp map[string]string
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}
	return resp, nil
}

// StopBot 停止机器人
func (c *Client) StopBot() (map[string]string, error) {
	body, err := c.doRequest("POST", "/stop", nil)
	if err != nil {
		return nil, err
	}
	var resp map[string]string
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}
	return resp, nil
}

// ReloadConfig 重新加载配置文件
func (c *Client) ReloadConfig() (map[string]interface{}, error) {
	body, err := c.doRequest("POST", "/reload_config", nil)
	if err != nil {
		return nil, err
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}
	return resp, nil
}

// QueryPerformance 查询各交易对表现
func (c *Client) QueryPerformance() (PerformanceResponse, error) {
	body, err := c.doRequest("GET", "/performance", nil)
	if err != nil {
		return nil, err
	}
	var resp PerformanceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// QueryVersion 查询机器人版本
func (c *Client) QueryVersion() (*VersionResponse, error) {
	body, err := c.doRequest("GET", "/version", nil)
	if err != nil {
		return nil, err
	}
	var resp VersionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ==============================================================================
//  新增：机器人状态查询
// ==============================================================================

// QueryConfig 查询机器人当前运行配置
func (c *Client) QueryConfig() (*ShowConfigResponse, error) {
	body, err := c.doRequest("GET", "/show_config", nil)
	if err != nil {
		return nil, err
	}
	var resp ShowConfigResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}
	return &resp, nil
}

// QueryHealth 查询机器人健康状况 (心跳)
func (c *Client) QueryHealth() (*HealthResponse, error) {
	body, err := c.doRequest("GET", "/health", nil)
	if err != nil {
		return nil, err
	}
	var resp HealthResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}
	return &resp, nil
}
