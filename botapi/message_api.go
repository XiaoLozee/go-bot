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

type StrangerInfo struct {
	UserID    int64  `json:"user_id"`
	Nickname  string `json:"nickname"`
	Sex       string `json:"sex"`
	Age       int32  `json:"age"`
	Qid       string `json:"qid"`
	Level     int    `json:"qq_level"`
	LoginDays int    `json:"login_days"`
}

type GroupMemberInfo struct {
	GroupID         int64  `json:"group_id"`
	UserID          int64  `json:"user_id"`
	Nickname        string `json:"nickname"`
	Card            string `json:"card"`
	Sex             string `json:"sex"`
	Age             int32  `json:"age"`
	Area            string `json:"area"`
	JoinTime        int64  `json:"join_time"`
	LastSentTime    int64  `json:"last_sent_time"`
	Level           string `json:"level"`
	Role            string `json:"role"` // "owner", "admin", "member"
	Unfriendly      bool   `json:"unfriendly"`
	Title           string `json:"title"`
	TitleExpireTime int64  `json:"title_expire_time"`
	CardChangeable  bool   `json:"card_changeable"`
}

// GetGroupMemberList 获取群成员列表
func GetGroupMemberList(groupId int64) ([]GroupMemberInfo, error) {
	action := &Action{
		Action: "get_group_member_list",
		Params: map[string]interface{}{
			"group_id": groupId,
		},
	}

	resp, err := sendSync(action)
	if err != nil {
		return nil, err
	}

	if resp.RetCode != 0 {
		return nil, fmt.Errorf("API 返回错误: %s (retcode: %d)", resp.Message, resp.RetCode)
	}

	var memberList []GroupMemberInfo
	if err := json.Unmarshal(resp.Data, &memberList); err != nil {
		return nil, fmt.Errorf("解析群成员列表 data 失败: %w", err)
	}

	return memberList, nil
}

// GetStrangerInfo 获取账号信息
func GetStrangerInfo(userId int64) (*StrangerInfo, error) {
	action := &Action{
		Action: "get_stranger_info",
		Params: map[string]interface{}{
			"user_id": userId,
		},
		Echo: fmt.Sprintf("%d", time.Now().UnixNano()),
	}
	resp, err := sendSync(action)
	if err != nil {
		return nil, fmt.Errorf("API 调用失败 (get_stranger_info): %w", err)
	}

	if resp.RetCode != 0 {
		return nil, fmt.Errorf("API 返回错误: %s (retcode: %d)", resp.Message, resp.RetCode)
	}

	var info StrangerInfo
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return nil, fmt.Errorf("解析 data 字段失败: %w", err)
	}

	return &info, nil
}

// GetImageInfo 获取图片信息
func GetImageInfo(file string) (*ImageInfo, error) {
	action := &Action{
		Action: "get_image",
		Params: map[string]interface{}{
			"file": file,
		},
	}

	resp, err := sendSync(action) // 使用我们之前创建的同步发送函数
	if err != nil {
		return nil, err
	}

	if resp.RetCode != 0 {
		return nil, fmt.Errorf("API 返回错误: %s (retcode: %d)", resp.Message, resp.RetCode)
	}

	var info ImageInfo
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return nil, fmt.Errorf("解析 data 字段失败: %w", err)
	}

	if info.URL == "" {
		return nil, fmt.Errorf("API 返回的 URL 为空")
	}

	return &info, nil
}

// sendSync 同步发送
func sendSync(action *Action) (*APIResponse, error) {
	client := GetInstance()
	if client == nil {
		return nil, fmt.Errorf("机器人客户端未连接")
	}
	return client.sendAndWait(action)
}

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

// SendMultiImageWithTextMsg 发送图文消息
func SendMultiImageWithTextMsg(msgType int, id int64, imgPaths []string, text string) {
	var messageArray []handler.OB11Segment

	for _, path := range imgPaths {
		imgDataPayload := &FileData{File: path}
		imgSegment, err := buildSegment("image", imgDataPayload)
		if err != nil {
			log.Printf("构建图片段失败 for path %s: %v", path, err)
			continue
		}
		messageArray = append(messageArray, *imgSegment)
	}

	// 最后添加文本段
	if text != "" {
		textDataPayload := TextData{Text: text}
		textSegment, err := buildSegment("text", textDataPayload)
		if err == nil {
			messageArray = append(messageArray, *textSegment)
		}
	}

	// 发送组合消息
	if len(messageArray) > 0 {
		SendMessage(msgType, id, messageArray)
	}
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

// SendMusicCardMsg 发送音乐卡片
func SendMusicCardMsg(msgType int, id int64, musicPlatform MusicPlatform, musicId string) {
	dataPayload := MusicData{
		Type: musicPlatform,
		Id:   musicId,
	}
	segment, err := buildSegment("music", dataPayload)
	if err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
	SendMessage(msgType, id, []handler.OB11Segment{*segment})
}

// SendCustomMusicCardMsg 发送自定义音乐卡片
func SendCustomMusicCardMsg(msgType int, id int64, url string, audio string, title string, image string) {
	dataPayload := MusicData{
		Type:  Custom,
		Url:   url,
		Audio: audio,
		Title: title,
		Image: image,
	}
	segment, err := buildSegment("music", dataPayload)
	if err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
	SendMessage(msgType, id, []handler.OB11Segment{*segment})
}

// ForwardGroupSingleMsg 转发消息到群
func ForwardGroupSingleMsg(groupId int64, messageID int64) {
	params := &ForwardData{
		GroupId:   groupId,
		MessageID: messageID,
	}
	action := &Action{
		Action: "forward_group_single_msg",
		Params: params,
		Echo:   fmt.Sprintf("%d", time.Now().UnixNano()),
	}
	if err := sendUtil(action); err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
}

// ForwardFriendSingleMsg 转发消息到私聊
func ForwardFriendSingleMsg(userId int64, messageID int64) {
	params := &ForwardData{
		UserId:    userId,
		MessageID: messageID,
	}
	action := &Action{
		Action: "forward_friend_single_msg",
		Params: params,
		Echo:   fmt.Sprintf("%d", time.Now().UnixNano()),
	}
	if err := sendUtil(action); err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
}

// SendReplyMsg 发送回复消息
func SendReplyMsg(msgType int, id int64, replyId int64, text string) {
	replyPayload := IdData{Id: replyId}
	replySegment, err1 := buildSegment("reply", replyPayload)
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

	messageArray := []handler.OB11Segment{*replySegment, *textSegment}

	SendMessage(msgType, id, messageArray)
}

// SendGroupPoke 发送群聊戳一戳
func SendGroupPoke(groupId int64, userId int64) {
	params := &PokeData{
		GroupId: groupId,
		UserId:  userId,
	}
	action := &Action{
		Action: "group_poke",
		Params: params,
		Echo:   fmt.Sprintf("%d", time.Now().UnixNano()),
	}
	if err := sendUtil(action); err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
}

// SendPrivatePoke 发送私聊戳一戳
func SendPrivatePoke(userId int64) {
	params := &PokeData{
		UserId: userId,
	}
	action := &Action{
		Action: "friend_poke",
		Params: params,
		Echo:   fmt.Sprintf("%d", time.Now().UnixNano()),
	}
	if err := sendUtil(action); err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
}

// SendJsonMsg 发送Json消息
func SendJsonMsg(msgType int, id int64, jsonMsg string) {
	dataPayload := JsonData{Data: jsonMsg}
	segment, err := buildSegment("json", dataPayload)
	if err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
	SendMessage(msgType, id, []handler.OB11Segment{*segment})
}

// SendFaceMsg 发送QQ表情
func SendFaceMsg(msgType int, id int64, faceId int64) {
	dataPayload := IdData{Id: faceId}
	segment, err := buildSegment("face", dataPayload)
	if err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
	SendMessage(msgType, id, []handler.OB11Segment{*segment})
}

// SendDiceMsg 发送骰子
func SendDiceMsg(msgType int, id int64) {
	segment := &handler.OB11Segment{
		Type: "dice",
	}
	SendMessage(msgType, id, []handler.OB11Segment{*segment})
}

// SendRpsMsg 发送猜拳
func SendRpsMsg(msgType int, id int64) {
	segment := &handler.OB11Segment{
		Type: "rps",
	}
	SendMessage(msgType, id, []handler.OB11Segment{*segment})
}

// SendGroupForwardMsg 发送群合并转发消息
func SendGroupForwardMsg(groupId int64, nodes []ForwardNode, opts ...ForwardOption) {
	params := &GroupForwardMsgParams{
		GroupID:  groupId,
		Messages: nodes,
	}

	for _, opt := range opts {
		opt(params)
	}

	action := &Action{
		Action: "send_group_forward_msg",
		Params: params, // 将构造好的参数赋值
		Echo:   fmt.Sprintf("%d", time.Now().UnixNano()),
	}

	if err := sendUtil(action); err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}
}

// SendPrivateForwardMsg 发送私聊合并转发消息
func SendPrivateForwardMsg(userId int64, nodes []ForwardNode) {
	action := &Action{
		Action: "send_private_forward_msg",
		Params: &PrivateForwardMsgParams{
			UserID:   userId,
			Messages: nodes,
		},
		Echo: fmt.Sprintf("%d", time.Now().UnixNano()),
	}

	if err := sendUtil(action); err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}

	log.Printf("API 调用成功: 已发送合并转发消息到用户 %d", userId)
}

// SendMessage 发送任意消息段组合
func SendMessage(msgType int, id int64, messageArray []handler.OB11Segment) {
	// 构造 Action，并检查返回值是否为 nil (防止 panic)
	action := buildAction(msgType, id, messageArray)
	if action == nil {
		log.Printf("API 调用失败: 无效的消息类型或参数, msgType: %d", msgType)
		return
	}

	// 调用底层的发送工具，并处理错误
	if err := sendUtil(action); err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}

	// 只有在 sendUtil 成功返回后，才打印成功日志
	log.Printf("API 调用成功: 已发送消息到 ID %d", id)
}

func DeleteMsg(messageId int64) {
	params := map[string]interface{}{
		"message_id": messageId,
	}

	action := &Action{
		Action: "delete_msg",
		Params: params,
		Echo:   fmt.Sprintf("%d", time.Now().UnixNano()),
	}

	if err := sendUtil(action); err != nil {
		log.Printf("API 调用失败: %v", err)
		return
	}

	log.Printf("API 调用成功: 已请求撤回消息 ID %d", messageId)
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
		return fmt.Errorf("机器人客户端未连接")
	}

	finalData, err := json.Marshal(action)
	if err != nil {
		return fmt.Errorf("最终 JSON 序列化错误: %w", err)
	}

	log.Println("发送内容: " + string(finalData))

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
