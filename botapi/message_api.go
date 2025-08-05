package botapi

import (
	"encoding/json"
	"fmt"
	"github.com/XiaoLuozee/go-bot/handler"
	"log"
	"time"
)

const (
	GroupMessage   = 1
	PrivateMessage = 2
)

func SendTextMsg(msgType int, id int64, msg string) {
	client := GetInstance()
	if client == nil {
		log.Println("API 调用失败: 机器人客户端未连接。")
		return
	}
	jsonData, err := json.Marshal(TextData{
		msg,
	})
	messageArray := []handler.OB11Segment{
		{
			Type: "text",
			Data: json.RawMessage(jsonData),
		},
	}
	var action Action
	switch msgType {
	case GroupMessage:
		action = Action{
			Action: "send_group_msg",
			Params: GroupMsg{
				GroupId: id,
				Message: messageArray,
			},
		}
	case PrivateMessage:
		action = Action{
			Action: "send_private_msg",
			Params: PrivateMsg{
				UserId:  id,
				Message: messageArray,
			},
		}
	default:
		log.Printf("API 调用失败: 未知的消息类型: %d", msgType)
		return
	}
	action.Echo = fmt.Sprintf("%d", time.Now().UnixNano())

	finalData, err := json.Marshal(action)
	log.Println("测试内容" + string(finalData))
	if err != nil {
		log.Printf("API 调用失败: JSON 序列化错误: %v", err)
		return
	}

	if err := client.Send(finalData); err != nil {
		log.Printf("API 调用失败: 发送消息错误: %v", err)
	}

	log.Printf("API 调用成功: 发送消息到 %d", id)
}

func SendImgMsg(msgType int, id int64, imgPath string) {
	client := GetInstance()
	if client == nil {
		log.Println("API 调用失败: 机器人客户端未连接。")
		return
	}
	jsonData, err := json.Marshal(FileData{
		File: imgPath,
	})
	messageArray := []handler.OB11Segment{
		{
			Type: "image",
			Data: json.RawMessage(jsonData),
		},
	}
	var action Action
	switch msgType {
	case GroupMessage:
		action = Action{
			Action: "send_group_msg",
			Params: GroupMsg{
				GroupId: id,
				Message: messageArray,
			},
		}
	case PrivateMessage:
		action = Action{
			Action: "send_private_msg",
			Params: PrivateMsg{
				UserId:  id,
				Message: messageArray,
			},
		}
	default:
		log.Printf("API 调用失败: 未知的消息类型: %d", msgType)
		return
	}
	action.Echo = fmt.Sprintf("%d", time.Now().UnixNano())

	finalData, err := json.Marshal(action)
	log.Println("测试内容" + string(finalData))
	if err != nil {
		log.Printf("API 调用失败: JSON 序列化错误: %v", err)
		return
	}

	if err := client.Send(finalData); err != nil {
		log.Printf("API 调用失败: 发送消息错误: %v", err)
	}

	log.Printf("API 调用成功: 发送消息到 %d", id)
}
