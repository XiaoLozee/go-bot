package handler

import (
	"github.com/XiaoLuozee/go-bot/registry"
	"log"
)

func MessageHandler(message []byte) {
	log.Println(string(message))

	event, err := ParseEvent(message)
	if err != nil {
		log.Printf("事件解析失败: %v", err)
		return
	}
	registry.Dispatch(event)
}
