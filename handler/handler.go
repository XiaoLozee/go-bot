package handler

import (
	"github.com/XiaoLuozee/go-bot/registry"
	"log"
)

func MessageHandler(message []byte) {
	log.Println(string(message))

	event, err := ParseEvent(message)
	if err != nil {
		// 如果解析失败（例如，JSON 格式错误、未知的事件类型等），
		// 我们记录下错误日志，然后终止对这条消息的处理。
		log.Printf("事件解析失败: %v", err)
		return
	}
	registry.Dispatch(event)
}
