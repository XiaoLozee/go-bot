package function

import (
	"encoding/json"
	"fmt"
	"github.com/XiaoLuozee/go-bot/botapi"
	"github.com/XiaoLuozee/go-bot/handler"
	"github.com/XiaoLuozee/go-bot/registry"
	"github.com/spf13/viper"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type APIResponse struct {
	Code int       `json:"code"` // 状态码
	Msg  string    `json:"msg"`  // 状态消息
	Data VideoData `json:"data"` // 核心数据
}

// VideoData 对应了 "data" 字段内的结构。
type VideoData struct {
	Author   AuthorInfo    `json:"author"`
	Title    string        `json:"title"`     // 视频标题
	VideoURL string        `json:"video_url"` // 视频链接
	MusicURL string        `json:"music_url"` // 音频链接
	CoverURL string        `json:"cover_url"` // 封面链接
	Images   []interface{} `json:"images"`
}

// AuthorInfo 对应了 "author" 字段内的结构。
type AuthorInfo struct {
	UID    string `json:"uid"`
	Name   string `json:"name"` // 作者名
	Avatar string `json:"avatar"`
}

type VideoParserPlugin struct{}

func (p *VideoParserPlugin) Process(event interface{}) {
	switch e := event.(type) {

	case handler.OB11PrivateMessage:
		videoParser(e.RawMessage, botapi.PrivateMessage, e.UserID)
	case handler.OB11GroupMessage:
		videoParser(e.RawMessage, botapi.GroupMessage, e.GroupId)
	}
}

func videoParser(message string, msgType int, id int64) {
	videoShareUrl, err := regexpMatchUrlFromString(message)
	if err != nil {
		return
	}

	videoData, err := parseVideoShareURL(videoShareUrl)
	if err != nil {
		fmt.Printf("视频链接解析失败: %v\n", err)
		return
	}

	viper.SetDefault("video_parser.video_max_size", 50)
	videoMaxSize := viper.GetInt("video_parser.video_max_size")

	botapi.SendMultiImageWithTextMsg(msgType, id, []string{videoData.CoverURL}, "视频解析成功\n标题:"+videoData.Title+"\n作者:"+videoData.Author.Name+"\n正在发送视频")
	videoSizeThreshold := int64(videoMaxSize) * 1024 * 1024

	// 获取远程视频的大小
	fileSize, err := getRemoteFileSize(videoData.VideoURL)
	if err != nil {
		fmt.Printf("获取视频大小失败 (%v)，将作为文件发送\n", err)
		botapi.SendFileMsg(msgType, id, videoData.VideoURL, videoData.Title+".mp4")
		return
	}

	// 将字节大小转换为 MB
	fileSizeMB := float64(fileSize) / (1024 * 1024)

	if fileSize > videoSizeThreshold {
		fmt.Println("视频超过阈值，将作为文件发送")
		botapi.SendTextMsg(msgType, id, fmt.Sprintf("视频大小为 %.2fMB，超过%dMB，将以文件形式发送", fileSizeMB, videoMaxSize))
		botapi.SendFileMsg(msgType, id, videoData.VideoURL, videoData.Title+".mp4") // 文件名可以自定义
	} else {
		botapi.SendVideoMsg(msgType, id, videoData.VideoURL)
	}
}

var videoShareURLRegex = regexp.MustCompile(`(https?://)?(v\.douyin\.com|www\.iesdouyin\.com|www\.douyin\.com|v\.kuaishou\.com|share\.xiaochuankeji\.cn|v\.ixigua\.com|h5\.pipix\.com|isee\.weishi\.qq\.com|share\.huoshan\.com|www\.pearvideo\.com|h5\.pipigx\.com|xspshare\.baidu\.com|v\.huya\.com|www\.acfun\.cn|weibo\.com|weibo\.cn|meipai\.com|doupai\.cc|kg\.qq\.com|6\.cn|xinpianchang\.com|haokan\.baidu\.com|haokan\.hao123\.com|www\.xiaohongshu\.com|xhslink\.com|bilibili\.com|b23\.tv)(\S*)`)

func regexpMatchUrlFromString(text string) (string, error) {
	match := videoShareURLRegex.FindString(text)

	if match == "" {
		return "", fmt.Errorf("在文本中未找到支持的视频分享链接")
	}

	if strings.Contains(match, "b23.tv") {
		match = strings.ReplaceAll(match, `\/`, `/`)
		match = "https://" + strings.Split(match, "?")[0]
	}
	return match, nil
}

// getRemoteFileSize 获取远程文件的大小
func getRemoteFileSize(fileURL string) (int64, error) {
	req, err := http.NewRequest("HEAD", fileURL, nil)
	if err != nil {
		return -1, fmt.Errorf("创建 HEAD 请求失败: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return -1, fmt.Errorf("发送 HEAD 请求失败: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return -1, fmt.Errorf("服务器返回非 200 状态码: %s", resp.Status)
	}

	contentLengthStr := resp.Header.Get("Content-Length")
	if contentLengthStr == "" {
		return -1, fmt.Errorf("服务器未在响应头中提供 Content-Length")
	}

	size, err := strconv.ParseInt(contentLengthStr, 10, 64)
	if err != nil {
		return -1, fmt.Errorf("解析 Content-Length 失败: %w", err)
	}

	return size, nil
}

// parseVideoShareURL 调用外部 API 来解析视频分享链接。
// 它接收原始的分享链接，返回解析后的 VideoData 和一个错误。
func parseVideoShareURL(shareURL string) (*VideoData, error) {
	// 1. 构建完整的 API 请求 URL
	// 使用 url.QueryEscape 来确保分享链接中的特殊字符被正确编码
	apiBaseURL := "http://luozhi.de:17992/video/share/url/parse"
	fullURL := fmt.Sprintf("%s?url=%s", apiBaseURL, url.QueryEscape(shareURL))

	fmt.Println("正在请求 API:", fullURL) // 打印日志，方便调试

	resp, err := http.Get(fullURL)
	if err != nil {
		return nil, fmt.Errorf("请求 API 失败: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 返回了非 200 的状态码: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("解析 JSON 响应失败: %w", err)
	}

	if apiResp.Code != 200 {
		return nil, fmt.Errorf("API 业务错误: %s (code: %d)", apiResp.Msg, apiResp.Code)
	}

	return &apiResp.Data, nil
}

func init() {
	registry.RegisterFactory(
		registry.CreatePluginFactory(&VideoParserPlugin{}, "bot.function.video_parser", true),
	)
}
