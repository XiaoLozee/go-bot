package main

import (
	"fmt"
	_ "github.com/XiaoLuozee/go-bot/function"
	"github.com/XiaoLuozee/go-bot/handler"
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
	// 默认情况下，WebSocket 服务器只接受来自同源的请求。
	// 为了方便开发，我们这里返回 true，允许所有来源的请求。
	// 在生产环境中，你需要实现一个安全的检查逻辑！
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

	log.Println("客户端已连接:", conn.RemoteAddr())

	// 进入无限循环，处理来自客户端的消息
	for {
		// ReadMessage 从连接中读取一个消息
		// messageType 是消息类型，可以是 TextMessage 或 BinaryMessage 等
		// p 是消息的内容 (payload)
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

		// 3. 将收到的消息原样写回客户端 (Echo)
		//err = conn.WriteMessage(messageType, p)
		//if err != nil {
		//	log.Println("写入错误:", err)
		//	break
		//}
	}

	log.Println("客户端断开连接:", conn.RemoteAddr())
}
