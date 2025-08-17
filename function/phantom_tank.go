package function

import (
	"encoding/json"
	"fmt"
	"github.com/XiaoLuozee/go-bot/botapi"
	"github.com/XiaoLuozee/go-bot/handler"
	"github.com/XiaoLuozee/go-bot/registry"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// TankAPIResponse 幻影坦克生成API的响应结构
type TankAPIResponse struct {
	Code     int    `json:"code"`
	Success  bool   `json:"success"`
	Message  string `json:"message"`
	ImageURL string `json:"imageUrl"`
}

// PhantomTank 插件结构体
type PhantomTank struct{}

// userState 存储用户制作幻影坦克的状态
type userState struct {
	firstImageURL string
	mode          string
	expiresAt     time.Time
}

// tankState 全局的状态存储
var (
	tankState = make(map[int64]*userState)
	stateLock sync.Mutex
)

// callTankAPI 调用API来生成幻影坦克图片
func callTankAPI(url1, url2, mode string) (string, error) {
	apiBaseURL := "http://luozhi.de:17993/generate"

	params := url.Values{}
	params.Add("url1", url1)
	params.Add("url2", url2)
	fullURL := fmt.Sprintf("%s?%s", apiBaseURL, params.Encode())
	if mode != "" {
		params.Add("mode", mode)
	}
	log.Println("正在请求幻影坦克 API:", fullURL)

	resp, err := http.Get(fullURL)
	if err != nil {
		return "", fmt.Errorf("请求 API 失败: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API 服务器返回非 200 状态码: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应体失败: %w", err)
	}

	var apiResp TankAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("解析 JSON 响应失败: %w", err)
	}

	if !apiResp.Success || apiResp.Code != 200 {
		return "", fmt.Errorf("API 业务错误: %s (code: %d)", apiResp.Message, apiResp.Code)
	}

	return apiResp.ImageURL, nil
}

func (p *PhantomTank) Process(event interface{}) {
	msg, ok := event.(handler.OB11GroupMessage)
	if !ok {
		return
	}
	var textMessage string
	var imageFileID string

	for _, segment := range msg.Message.Message {
		if segment.Type == "image" {
			var imgData struct {
				File string `json:"file"`
			}
			if json.Unmarshal(segment.Data, &imgData) == nil {
				imageFileID = imgData.File
			}
		}
		if segment.Type == "text" {
			var textData struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(segment.Data, &textData) == nil {
				textMessage += textData.Text
			}
		}
	}
	textMessage = strings.TrimSpace(textMessage)

	stateLock.Lock()
	defer stateLock.Unlock()

	if state, waiting := tankState[msg.UserID]; waiting {
		if time.Now().After(state.expiresAt) {
			delete(tankState, msg.UserID)
			botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, "制作超时了哦，请重新开始吧~")
			return
		}

		if imageFileID == "" {
			botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, "请发送第二张图片哦~")
			return
		}

		//botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, "收到第二张图片，正在处理...")
		info2, err := botapi.GetImageInfo(imageFileID)
		if err != nil { /* ... 错误处理 ... */
			return
		}

		// 获取两张图的 URL 和模式
		firstImgURL := state.firstImageURL
		secondImgURL := info2.URL
		mode := state.mode // <--- 从状态中获取模式

		delete(tankState, msg.UserID) // 清理状态

		// 调用 API 生成
		botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, "图片信息获取成功，正在合成...")
		go func(gid int64, url1, url2, mode string) {
			// 将 mode 传递给 callTankAPI
			generatedImageURL, err := callTankAPI(url1, url2, mode)
			if err != nil {
				log.Printf("调用幻影坦克 API 失败: %v", err)
				botapi.SendTextMsg(botapi.GroupMessage, gid, fmt.Sprintf("合成失败了QAQ：%v", err))
				return
			}
			botapi.SendImgMsg(botapi.GroupMessage, gid, generatedImageURL)
		}(msg.GroupId, firstImgURL, secondImgURL, mode)

		return
	}

	var triggerMode string
	if textMessage == "/幻影坦克" || textMessage == "幻影坦克" {
		triggerMode = ""
	} else if textMessage == "/彩色幻影坦克" || textMessage == "彩色幻影坦克" {
		triggerMode = "color"
	} else {
		return
	}

	if imageFileID != "" {
		info1, err := botapi.GetImageInfo(imageFileID)
		if err != nil {
			return
		}

		tankState[msg.UserID] = &userState{
			firstImageURL: info1.URL,
			mode:          triggerMode,
			expiresAt:     time.Now().Add(60 * time.Second),
		}
		// ----------------------------------------

		modeText := "普通"
		if triggerMode == "color" {
			modeText = "彩色"
		}
		botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, fmt.Sprintf("已选择【%s】幻影坦克模式！请在60秒内发送第二张图片（亮色背景图）", modeText))

	} else {
		// 发送帮助信息
		helpText := "请发送“幻影坦克”或“彩色幻影坦克”并带上一张图片来开始制作哦！"
		botapi.SendTextMsg(botapi.GroupMessage, msg.GroupId, helpText)
	}
}

func init() {
	registry.RegisterFactory(
		registry.CreatePluginFactory(&PhantomTank{}, "bot.function.phantom_tank", true),
	)
}
