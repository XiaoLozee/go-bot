package botapi

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"sync"
	"time"
)

// BotClient WebSocket连接的结构体
type BotClient struct {
	conn             *websocket.Conn
	writeMutex       sync.Mutex
	responseChannels map[string]chan *APIResponse
	responseMutex    sync.Mutex
}

type APIResponse struct {
	Status  string          `json:"status"`
	RetCode int             `json:"retcode"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
	Wording string          `json:"wording"`
	Echo    string          `json:"echo"`
}

// Action OneBotAPI请求的通用结构
type Action struct {
	Action string      `json:"action"`         // 接口名
	Params interface{} `json:"params"`         // 发送的内容
	Echo   string      `json:"echo,omitempty"` // 唯一标识请求（可空）
}

// NewClient 是 BotClient 的构造函数
func NewClient(conn *websocket.Conn) *BotClient {
	return &BotClient{
		conn:             conn,
		responseChannels: make(map[string]chan *APIResponse),
	}
}

// Send 用于向 WebSocket 客户端安全地发送数据
func (c *BotClient) Send(data []byte) error {
	c.writeMutex.Lock()
	defer c.writeMutex.Unlock()

	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// 同步发送等待响应
func (c *BotClient) sendAndWait(action *Action) (*APIResponse, error) {
	if action.Echo == "" {
		action.Echo = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	ch := make(chan *APIResponse, 1)

	c.responseMutex.Lock()
	c.responseChannels[action.Echo] = ch
	c.responseMutex.Unlock()

	defer func() {
		c.responseMutex.Lock()
		delete(c.responseChannels, action.Echo)
		c.responseMutex.Unlock()
		close(ch)
	}()

	data, err := json.Marshal(action)
	if err != nil {
		return nil, fmt.Errorf("序列化 Action 失败: %w", err)
	}
	if err := c.Send(data); err != nil {
		return nil, err
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("API 调用超时 (action: %s, echo: %s)", action.Action, action.Echo)
	}
}

func (c *BotClient) DispatchResponse(message []byte) bool {
	// 尝试将消息解析为通用响应结构
	var resp APIResponse
	if err := json.Unmarshal(message, &resp); err != nil || resp.Echo == "" {
		return false
	}

	c.responseMutex.Lock()
	if ch, ok := c.responseChannels[resp.Echo]; ok {
		select {
		case ch <- &resp:
		default:
		}
	}
	c.responseMutex.Unlock()

	return true
}

// 实现全局单例的核心代码
var (
	instance      *BotClient
	instanceMutex sync.Mutex // 用于保护 instance 变量的读写
)

// SetInstance 设置全局的 BotClient 实例
func SetInstance(client *BotClient) {
	instanceMutex.Lock()
	defer instanceMutex.Unlock()
	instance = client
}

// GetInstance 获取当前的 BotClient 实例
func GetInstance() *BotClient {
	instanceMutex.Lock()
	defer instanceMutex.Unlock()
	return instance
}
