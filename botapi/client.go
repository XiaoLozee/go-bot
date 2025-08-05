package botapi

import (
	"github.com/gorilla/websocket"
	"sync"
)

// BotClient WebSocket连接的结构体
type BotClient struct {
	conn       *websocket.Conn
	writeMutex sync.Mutex
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
		conn: conn,
	}
}

// Send 用于向 WebSocket 客户端安全地发送数据
func (c *BotClient) Send(data []byte) error {
	c.writeMutex.Lock()
	defer c.writeMutex.Unlock()

	return c.conn.WriteMessage(websocket.TextMessage, data)
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
