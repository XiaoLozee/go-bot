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

// SendTextMsg 发送文本消息
func SendTextMsg(msgType int, id int64, msg string) {
	dataPayload := TextData{
		Text: msg,
	}
	segment, err := buildSegment("text", dataPayload)
	if err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
	SendMessage(msgType, id, []handler.OB11Segment{*segment})
}

// SendImgMsg 发送图片消息
func SendImgMsg(msgType int, id int64, imgPath string, opts ...ImageOption) {
	dataPayload := &FileData{File: imgPath}
	for _, opt := range opts {
		opt(dataPayload)
	}
	segment, err := buildSegment("image", dataPayload)
	if err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
	SendMessage(msgType, id, []handler.OB11Segment{*segment})
}

// SendVideoMsg 发送视频消息
func SendVideoMsg(msgType int, id int64, videoPath string) {
	dataPayload := FileData{File: videoPath}
	segment, err := buildSegment("video", dataPayload)
	if err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
	SendMessage(msgType, id, []handler.OB11Segment{*segment})
}

// SendFileMsg 发送文件消息
func SendFileMsg(msgType int, id int64, filePath string, fileName string) {
	dataPayload := FileData{File: filePath, Name: fileName}
	segment, err := buildSegment("file", dataPayload)
	if err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
	SendMessage(msgType, id, []handler.OB11Segment{*segment})
}

// SendRecordMsg 发送语音消息
func SendRecordMsg(msgType int, id int64, recordPath string) {
	dataPayload := FileData{File: recordPath}
	segment, err := buildSegment("record", dataPayload)
	if err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
	SendMessage(msgType, id, []handler.OB11Segment{*segment})
}

// SendGroupAtMsg 发送艾特
func SendGroupAtMsg(groupId int64, userId any) {
	dataPayload := AtData{
		QQ: userId,
	}

	segment, err := buildSegment("at", dataPayload)
	if err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}

	SendMessage(GroupMessage, groupId, []handler.OB11Segment{*segment})
}

// SendGroupAtAllMsg 发送艾特全体
func SendGroupAtAllMsg(groupId int64) {
	SendGroupAtMsg(groupId, "all")
}

// SendGroupAtWithTextMsg 发送艾特以及消息
func SendGroupAtWithTextMsg(groupId int64, userId any, text string) {
	atDataPayload := AtData{QQ: userId}
	atSegment, err1 := buildSegment("at", atDataPayload)
	if err1 != nil {
		log.Printf("API 调用失败: %v", err1)
		return
	}

	textDataPayload := TextData{Text: " " + text}
	textSegment, err2 := buildSegment("text", textDataPayload)
	if err2 != nil {
		log.Printf("API 调用失败: %v", err2)
		return
	}

	messageArray := []handler.OB11Segment{*atSegment, *textSegment}

	SendMessage(GroupMessage, groupId, messageArray)
}

// SendMessage 发送任意消息段组合
func SendMessage(msgType int, id int64, messageArray []handler.OB11Segment) {
	// 1. 构造 Action，并检查返回值是否为 nil (防止 panic)
	action := buildAction(msgType, id, messageArray)
	if action == nil {
		log.Printf("API 调用失败: 无效的消息类型或参数, msgType: %d", msgType)
		return
	}

	// 2. 调用底层的发送工具，并处理错误
	if err := sendUtil(action); err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}

	// 3. 只有在 sendUtil 成功返回后，才打印成功日志
	log.Printf("API 调用成功: 已发送消息到 ID %d", id)
}

// 创建消息段
func buildSegment(segType string, dataPayload interface{}) (*handler.OB11Segment, error) {
	dataBytes, err := json.Marshal(dataPayload)
	if err != nil {
		return nil, fmt.Errorf("构造 %s 消息段 data 失败: %w", segType, err)
	}
	return &handler.OB11Segment{
		Type: segType,
		Data: dataBytes,
	}, nil
}

// 发送最终请求
func sendUtil(action *Action) error {
	client := GetInstance()
	if client == nil {
		// 返回一个明确的错误
		return fmt.Errorf("机器人客户端未连接")
	}

	finalData, err := json.Marshal(action)
	if err != nil {
		return fmt.Errorf("最终 JSON 序列化错误: %w", err)
	}

	log.Println("发送内容: " + string(finalData)) // 将 "测试内容" 改为更通用的 "发送内容"

	if err := client.Send(finalData); err != nil {
		return fmt.Errorf("发送消息到 WebSocket 失败: %w", err)
	}
	// 成功时返回 nil
	return nil
}

// 构造发送消息Action
func buildAction(msgType int, id int64, messageArray []handler.OB11Segment) *Action {
	switch msgType {
	case GroupMessage:
		return &Action{
			Action: "send_group_msg",
			Params: GroupMsgParams{
				GroupId: id,
				Message: messageArray,
			},
			Echo: fmt.Sprintf("%d", time.Now().UnixNano()),
		}
	case PrivateMessage:
		return &Action{
			Action: "send_private_msg",
			Params: PrivateMsgParams{
				UserId:  id,
				Message: messageArray,
			},
			Echo: fmt.Sprintf("%d", time.Now().UnixNano()),
		}
	default:
		return nil
	}
}
