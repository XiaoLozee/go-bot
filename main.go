package main

import (
	"fmt"
	"github.com/XiaoLuozee/go-bot/botapi"
	_ "github.com/XiaoLuozee/go-bot/function"
	"github.com/XiaoLuozee/go-bot/handler"
	"github.com/XiaoLuozee/go-bot/registry"
	"github.com/spf13/viper"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

func main() {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", "8080")

	if err := viper.ReadInConfig(); err != nil {
		panic(fmt.Errorf("无法读取配置文件: %w", err))
	}

	serverConfig := viper.Sub("server")

	host := serverConfig.GetString("host")
	port := serverConfig.GetString("port")

	log.Println("加载配置 -> Host: " + host + ", Port: " + port)

	registry.LoadPlugins()

	// 设置路由，将 "/ws" 路径的请求交给 handleWebSocket 函数处理
	http.HandleFunc("/ws", handleWebSocket)

	log.Println("\n⠀⠀⠀⣠⠤⡀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣀⣀⠀⠀\n⠀⠀⡜⠁⠀⠈⢢⡀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢀⣴⠋⠷⠶⠱⡄\n⠀⢸⣸⣿⠀⠀⠀⠙⢦⡀⠀⠀⠀⠀⠀⠀⠀⢀⡴⠫⢀⣖⡃⢀⣸⢹\n⠀⡇⣿⣿⣶⣤⡀⠀⠀⠙⢆⠀⠀⠀⠀⠀⣠⡪⢀⣤⣾⣿⣿⣿⣿⣸\n⠀⡇⠛⠛⠛⢿⣿⣷⣦⣀⠀⣳⣄⠀⢠⣾⠇⣠⣾⣿⣿⣿⣿⣿⣿⣽\n⠀⠯⣠⣠⣤⣤⣤⣭⣭⡽⠿⠾⠞⠛⠷⠧⣾⣿⣿⣯⣿⡛⣽⣿⡿⡼\n⠀⡇⣿⣿⣿⣿⠟⠋⠁⠀⠀⠀⠀⠀⠀⠀⠀⠈⠙⠻⣿⣿⣮⡛⢿⠃\n⠀⣧⣛⣭⡾⠁⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠈⢿⣿⣷⣎⡇\n⠀⡸⣿⡟⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠘⢿⣷⣟⡇\n⣜⣿⣿⡧⠀⠀⠀⠀⠀⡀⠀⠀⠀⠀⠀⠀⣄⠀⠀⠀⠀⠀⣸⣿⡜⡄\n⠉⠉⢹⡇⠀⠀⠀⢀⣞⠡⠀⠀⠀⠀⠀⠀⡝⣦⠀⠀⠀⠀⢿⣿⣿⣹\n⠀⠀⢸⠁⠀⠀⢠⣏⣨⣉⡃⠀⠀⠀⢀⣜⡉⢉⣇⠀⠀⠀⢹⡄⠀⠀\n⠀⠀⡾⠄⠀⠀⢸⣾⢏⡍⡏⠑⠆⠀⢿⣻⣿⣿⣿⠀⠀⢰⠈⡇⠀⠀\n⠀⢰⢇⢀⣆⠀⢸⠙⠾⠽⠃⠀⠀⠀⠘⠿⡿⠟⢹⠀⢀⡎⠀⡇⠀⠀\n⠀⠘⢺⣻⡺⣦⣫⡀⠀⠀⠀⣄⣀⣀⠀⠀⠀⠀⢜⣠⣾⡙⣆⡇⠀⠀\n⠀⠀⠀⠙⢿⡿⡝⠿⢧⡢⣠⣤⣍⣀⣤⡄⢀⣞⣿⡿⣻⣿⠞⠀⠀⠀\n⠀⠀⠀⢠⠏⠄⠐⠀⣼⣿⣿⣿⣿⣿⣿⣿⣿⡇⠀⠳⢤⣉⢳⠀⠀⠀\n⢀⡠⠖⠉⠀⠀⣠⠇⣿⡿⣿⡿⢹⣿⣿⣿⣿⣧⣠⡀⠀⠈⠉⢢⡀⠀\n⢿⠀⠀⣠⠴⣋⡤⠚⠛⠛⠛⠛⠛⠛⠛⠛⠙⠛⠛⢿⣦⣄⠀⢈⡇⠀\n⠈⢓⣤⣵⣾⠁⣀⣀⠤⣤⣀⠀⠀⠀⠀⢀⡤⠶⠤⢌⡹⠿⠷⠻⢤⡀\n⢰⠋⠈⠉⠘⠋⠁⠀⠀⠈⠙⠳⢄⣀⡴⠉⠀⠀⠀⠀⠙⠂⠀⠀⢀⡇\n⢸⡠⡀⠀⠒⠂⠐⠢⠀⣀⠀⠀⠀⠀⠀⢀⠤⠚⠀⠀⢸⣔⢄⠀⢾⠀\n⠀⠑⠸⢿⠀⠀⠀⠀⢈⡗⠭⣖⡒⠒⢊⣱⠀⠀⠀⠀⢨⠟⠂⠚⠋⠀\n⠀⠀⠀⠘⠦⣄⣀⣠⠞⠀⠀⠀⠈⠉⠉⠀⠳⠤⠤⡤⠞⠀⠀⠀⠀⠀\n  ____        _  __       ____        _   \n |  _ \\      | |/ /      |  _ \\      | |  \n | |_) | __ _| ' / __ _  | |_) | ___ | |_ \n |  _ < / _` |  < / _` | |  _ < / _ \\| __|\n | |_) | (_| | . \\ (_| | | |_) | (_) | |_ \n |____/ \\__,_|_|\\_\\__,_| |____/ \\___/ \\__|\n                                          \n                                          ")

	log.Println("服务器已启动 " + host + ":" + port)
	// 启动 HTTP 服务器，监听 8080 端口
	err := http.ListenAndServe(host+":"+port, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

// 定义upgrader将http连接升级为WebSocket
var upgrader = websocket.Upgrader{
	// 缓冲区大小
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,

	// CheckOrigin 会检查请求的来源。
	// 返回 true，代表允许所有来源的请求
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// handleWebSocket 是处理 WebSocket 请求的函数
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 升级 HTTP 连接为 WebSocket 连接
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("升级错误:", err)
		return
	}
	// 函数退出时确保关闭连接
	defer func(conn *websocket.Conn) {
		_ = conn.Close()
	}(conn)

	client := botapi.NewClient(conn)
	botapi.SetInstance(client)
	log.Println("客户端已连接:", conn.RemoteAddr())

	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			// 如果是连接关闭的错误，就打印日志并退出循环
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("错误：意外关闭: %v", err)
			} else {
				log.Println("读取错误:", err)
			}
			break
		}

		if messageType == websocket.TextMessage {
			//log.Printf("收到消息 %s: %s", conn.RemoteAddr(), string(p))
			handler.MessageHandler(p)
		}
	}

	log.Println("客户端断开连接:", conn.RemoteAddr())
}
