package function

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/XiaoLuozee/go-bot/botapi"
	"github.com/XiaoLuozee/go-bot/handler"
	"github.com/XiaoLuozee/go-bot/registry"
	"io"
	"log"
	"net/http"
	"strings"
)

// JMComicAPIResponse API 的响应结构。
type JMComicAPIResponse struct {
	ComicID int64  `json:"comic_id"`
	PDFURL  string `json:"pdf_url"`
}

type JMCoimcDownloadPlugin struct{}

func (p *JMCoimcDownloadPlugin) Process(event interface{}) {
	msg, ok := event.(handler.OB11GroupMessage)
	if !ok {
		return
	}

	fields := strings.Fields(msg.RawMessage)
	if len(fields) != 2 || strings.ToLower(fields[0]) != "jm" {
		return
	}

	comicID := fields[1]

	botapi.SendReplyMsg(
		botapi.GroupMessage,
		msg.GroupId,
		msg.MessageId,
		fmt.Sprintf("正在获取 %s 的漫画资源链接...", comicID),
	)

	go func(gid int64, cid string) {
		apiResp, err := callJMComicAPI(cid)
		if err != nil {
			log.Printf("调用 JM 漫画 API 失败: %v", err)
			botapi.SendTextMsg(botapi.GroupMessage, gid, fmt.Sprintf("下载漫画失败", cid, err))
			return
		}

		if apiResp.PDFURL == "" {
			log.Printf("JM 漫画 API 未返回有效的 PDF URL for ID: %s", cid)
			botapi.SendTextMsg(botapi.GroupMessage, gid, fmt.Sprintf("漫画下载失败", cid))
			return
		}

		log.Printf("获取到漫画 %s 的下载链接: %s", cid, apiResp.PDFURL)

		fileName := fmt.Sprintf("JM_%s.pdf", cid)

		botapi.SendTextMsg(
			botapi.GroupMessage,
			gid,
			fmt.Sprintf("漫画下载成功，正在发送，请耐心等待..."),
		)

		botapi.SendFileMsg(botapi.GroupMessage, gid, apiResp.PDFURL, fileName)

	}(msg.GroupId, comicID)
}

// callJMComicAPI
func callJMComicAPI(comicID string) (*JMComicAPIResponse, error) {
	// API 的固定路径
	apiURL := "http://127.0.0.1:17994/download/comic/" + comicID

	requestBody := bytes.NewBuffer([]byte("{}"))

	log.Println("正在请求 JM 漫画 API (POST):", apiURL)

	resp, err := http.Post(apiURL, "application/json", requestBody)
	if err != nil {
		return nil, fmt.Errorf("请求 API 失败: %w", err)
	}
	// ------------------------------------

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API 服务器返回非 200 状态码: %s, 详情: %s", resp.Status, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	var apiResp JMComicAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		var errResp struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Detail != "" {
			return nil, fmt.Errorf("API 业务错误: %s", errResp.Detail)
		}
		return nil, fmt.Errorf("解析 JSON 响应失败: %w, 响应原文: %s", err, string(body))
	}

	return &apiResp, nil
}

func init() {
	registry.RegisterFactory(
		registry.CreatePluginFactory(&JMCoimcDownloadPlugin{}, "bot.function.jmcomic_download", true),
	)
}
