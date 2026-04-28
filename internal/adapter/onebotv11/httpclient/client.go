package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	onebotv11 "github.com/XiaoLozee/go-bot/internal/adapter/onebotv11"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
)

type Client struct {
	id          string
	baseURL     string
	accessToken string
	httpClient  *http.Client
	logger      *slog.Logger
}

func New(id, baseURL, accessToken string, timeout time.Duration, logger *slog.Logger) *Client {
	return &Client{
		id:          id,
		baseURL:     strings.TrimRight(baseURL, "/"),
		accessToken: accessToken,
		httpClient:  &http.Client{Timeout: timeout},
		logger:      logger.With("connection", id, "component", "napcat_http"),
	}
}

func (c *Client) ID() string {
	return c.id
}

func (c *Client) SendMessage(ctx context.Context, req adapter.SendMessageRequest) (*adapter.SendMessageResult, error) {
	endpoint := "/send_msg"
	payload := map[string]any{
		"message":     mapSegments(req.Segments),
		"auto_escape": req.AutoEscape,
	}

	switch req.ChatType {
	case "group":
		endpoint = "/send_group_msg"
		payload["group_id"] = req.GroupID
	case "private":
		endpoint = "/send_private_msg"
		payload["user_id"] = req.UserID
	default:
		payload["message_type"] = req.ChatType
		payload["group_id"] = req.GroupID
		payload["user_id"] = req.UserID
	}

	var resp sendMessageResponse
	if err := c.doJSON(ctx, endpoint, payload, &resp); err != nil {
		return nil, err
	}

	return &adapter.SendMessageResult{
		MessageID: normalizeID(resp.Data.MessageID),
		ForwardID: resp.Data.ForwardID,
	}, nil
}

func (c *Client) SendGroupForwardMessage(ctx context.Context, req adapter.SendGroupForwardRequest) (*adapter.SendMessageResult, error) {
	payload := map[string]any{
		"group_id": req.GroupID,
		"messages": mapForwardNodes(req.Nodes),
	}
	if req.Options.Prompt != "" {
		payload["prompt"] = req.Options.Prompt
	}
	if req.Options.Summary != "" {
		payload["summary"] = req.Options.Summary
	}
	if req.Options.Source != "" {
		payload["source"] = req.Options.Source
	}

	var resp sendMessageResponse
	if err := c.doJSON(ctx, "/send_group_forward_msg", payload, &resp); err != nil {
		return nil, err
	}

	return &adapter.SendMessageResult{
		MessageID: normalizeID(resp.Data.MessageID),
		ForwardID: resp.Data.ForwardID,
	}, nil
}

func (c *Client) DeleteMessage(ctx context.Context, messageID string) error {
	var resp baseResponse[struct{}]
	return c.doJSON(ctx, "/delete_msg", map[string]any{"message_id": messageID}, &resp)
}

func (c *Client) GetMessage(ctx context.Context, messageID string) (*adapter.MessageDetail, error) {
	var resp baseResponse[getMessageData]
	if err := c.doJSON(ctx, "/get_msg", map[string]any{"message_id": messageID}, &resp); err != nil {
		return nil, err
	}

	return &adapter.MessageDetail{
		Time:        time.Unix(resp.Data.Time, 0),
		MessageType: resp.Data.MessageType,
		MessageID:   normalizeID(resp.Data.MessageID),
		UserID:      normalizeID(resp.Data.UserID),
		GroupID:     normalizeID(resp.Data.GroupID),
		RawMessage:  resp.Data.RawMessage,
		Message:     resp.Data.Message,
		Sender:      resp.Data.Sender,
	}, nil
}

func (c *Client) GetForwardMessage(ctx context.Context, forwardID string) (*adapter.ForwardMessage, error) {
	var resp baseResponse[json.RawMessage]
	if err := c.doJSON(ctx, "/get_forward_msg", map[string]any{"id": forwardID}, &resp); err != nil {
		return nil, err
	}
	return onebotv11.ParseForwardMessage(forwardID, resp.Data)
}

func (c *Client) GetRecentMessages(ctx context.Context, req adapter.RecentMessagesRequest) ([]adapter.MessageDetail, error) {
	action, payload, err := onebotv11.RecentMessageAction(req)
	if err != nil {
		return nil, err
	}
	var resp baseResponse[json.RawMessage]
	if err := c.doJSON(ctx, "/"+action, payload, &resp); err != nil {
		return nil, err
	}
	return onebotv11.ParseMessageHistory(resp.Data, req)
}

func (c *Client) ResolveMedia(ctx context.Context, segmentType, file string) (*adapter.ResolvedMedia, error) {
	segmentType = strings.TrimSpace(strings.ToLower(segmentType))
	file = strings.TrimSpace(file)
	if file == "" {
		return nil, fmt.Errorf("媒体引用不能为空")
	}

	endpoint := "/get_file"
	switch segmentType {
	case "image":
		endpoint = "/get_image"
	case "record", "audio", "voice":
		endpoint = "/get_record"
	case "video", "file":
		endpoint = "/get_file"
	}

	var resp baseResponse[resolvedMediaData]
	if err := c.doJSON(ctx, endpoint, map[string]any{"file": file}, &resp); err != nil {
		return nil, err
	}

	return &adapter.ResolvedMedia{
		File:     resp.Data.File,
		URL:      resp.Data.URL,
		FileName: resp.Data.FileName,
		FileSize: resp.Data.FileSize,
	}, nil
}

func (c *Client) GetLoginInfo(ctx context.Context) (*adapter.LoginInfo, error) {
	var resp baseResponse[loginInfoData]
	if err := c.doJSON(ctx, "/get_login_info", map[string]any{}, &resp); err != nil {
		return nil, err
	}

	return &adapter.LoginInfo{
		UserID:   normalizeID(resp.Data.UserID),
		Nickname: resp.Data.Nickname,
	}, nil
}

func (c *Client) GetStatus(ctx context.Context) (*adapter.BotStatus, error) {
	var resp baseResponse[statusData]
	if err := c.doJSON(ctx, "/get_status", map[string]any{}, &resp); err != nil {
		return nil, err
	}

	return &adapter.BotStatus{
		Online: resp.Data.Online,
		Good:   resp.Data.Good,
		Stat:   resp.Data.Stat,
	}, nil
}

func (c *Client) GetStrangerInfo(ctx context.Context, userID string) (*adapter.UserInfo, error) {
	var resp baseResponse[userInfoData]
	if err := c.doJSON(ctx, "/get_stranger_info", map[string]any{"user_id": userID}, &resp); err != nil {
		return nil, err
	}

	return &adapter.UserInfo{
		UserID:   normalizeID(resp.Data.UserID),
		Nickname: resp.Data.Nickname,
		Sex:      resp.Data.Sex,
		Age:      resp.Data.Age,
	}, nil
}

func (c *Client) GetGroupInfo(ctx context.Context, groupID string) (*adapter.GroupInfo, error) {
	var resp baseResponse[groupInfoData]
	if err := c.doJSON(ctx, "/get_group_info", map[string]any{"group_id": groupID}, &resp); err != nil {
		return nil, err
	}

	return &adapter.GroupInfo{
		GroupID:        normalizeID(resp.Data.GroupID),
		GroupName:      resp.Data.GroupName,
		MemberCount:    resp.Data.MemberCount,
		MaxMemberCount: resp.Data.MaxMemberCount,
	}, nil
}

func (c *Client) GetGroupList(ctx context.Context) ([]adapter.GroupInfo, error) {
	var resp baseResponse[[]groupInfoData]
	if err := c.doJSON(ctx, "/get_group_list", map[string]any{}, &resp); err != nil {
		return nil, err
	}

	out := make([]adapter.GroupInfo, 0, len(resp.Data))
	for _, item := range resp.Data {
		out = append(out, adapter.GroupInfo{
			GroupID:        normalizeID(item.GroupID),
			GroupName:      item.GroupName,
			MemberCount:    item.MemberCount,
			MaxMemberCount: item.MaxMemberCount,
		})
	}
	return out, nil
}

func (c *Client) GetFriendList(ctx context.Context) ([]adapter.UserInfo, error) {
	var resp baseResponse[[]userInfoData]
	if err := c.doJSON(ctx, "/get_friend_list", map[string]any{}, &resp); err != nil {
		return nil, err
	}

	out := make([]adapter.UserInfo, 0, len(resp.Data))
	for _, item := range resp.Data {
		nickname := strings.TrimSpace(item.Nickname)
		if nickname == "" {
			nickname = strings.TrimSpace(item.Remark)
		}
		out = append(out, adapter.UserInfo{
			UserID:   normalizeID(item.UserID),
			Nickname: nickname,
			Sex:      item.Sex,
			Age:      item.Age,
		})
	}
	return out, nil
}

func (c *Client) GetGroupMemberList(ctx context.Context, groupID string) ([]adapter.GroupMemberInfo, error) {
	var resp baseResponse[[]groupMemberInfoData]
	if err := c.doJSON(ctx, "/get_group_member_list", map[string]any{"group_id": groupID}, &resp); err != nil {
		return nil, err
	}

	out := make([]adapter.GroupMemberInfo, 0, len(resp.Data))
	for _, item := range resp.Data {
		out = append(out, adapter.GroupMemberInfo{
			GroupID:  normalizeID(item.GroupID),
			UserID:   normalizeID(item.UserID),
			Nickname: item.Nickname,
			Card:     item.Card,
			Role:     item.Role,
			Sex:      item.Sex,
			Age:      item.Age,
			Level:    item.Level,
			Title:    item.Title,
			Area:     item.Area,
			JoinTime: item.JoinTime,
			LastSent: item.LastSentTime,
		})
	}
	return out, nil
}

func (c *Client) GetGroupMemberInfo(ctx context.Context, groupID, userID string) (*adapter.GroupMemberInfo, error) {
	var resp baseResponse[groupMemberInfoData]
	if err := c.doJSON(ctx, "/get_group_member_info", map[string]any{"group_id": groupID, "user_id": userID}, &resp); err != nil {
		return nil, err
	}

	return &adapter.GroupMemberInfo{
		GroupID:  normalizeID(resp.Data.GroupID),
		UserID:   normalizeID(resp.Data.UserID),
		Nickname: resp.Data.Nickname,
		Card:     resp.Data.Card,
		Role:     resp.Data.Role,
		Sex:      resp.Data.Sex,
		Age:      resp.Data.Age,
		Level:    resp.Data.Level,
		Title:    resp.Data.Title,
		Area:     resp.Data.Area,
		JoinTime: resp.Data.JoinTime,
		LastSent: resp.Data.LastSentTime,
	}, nil
}

func (c *Client) doJSON(ctx context.Context, endpoint string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求接口失败: %w", err)
	}
	defer resp.Body.Close()

	c.logger.Debug("NapCat HTTP 请求完成", "endpoint", endpoint, "status_code", resp.StatusCode, "latency_ms", time.Since(start).Milliseconds())

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &adapter.ActionError{
			Endpoint: endpoint,
			Message:  fmt.Sprintf("HTTP 状态码异常: %d", resp.StatusCode),
		}
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	if base, ok := out.(responseStatusCarrier); ok {
		if base.GetStatus() != "ok" || base.GetRetCode() != 0 {
			return &adapter.ActionError{
				Endpoint: endpoint,
				RetCode:  base.GetRetCode(),
				Message:  base.GetMessage(),
				Wording:  base.GetWording(),
			}
		}
	}

	return nil
}

func mapSegments(segs []message.Segment) []map[string]any {
	out := make([]map[string]any, 0, len(segs))
	for _, seg := range segs {
		out = append(out, map[string]any{
			"type": seg.Type,
			"data": seg.Data,
		})
	}
	return out
}

func mapForwardNodes(nodes []message.ForwardNode) []map[string]any {
	out := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, map[string]any{
			"type": "node",
			"data": map[string]any{
				"user_id":  node.UserID,
				"nickname": node.Nickname,
				"content":  mapSegments(node.Content),
			},
		})
	}
	return out
}

func normalizeID(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case float32:
		return strconv.FormatInt(int64(x), 10)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	default:
		return fmt.Sprintf("%v", x)
	}
}

type responseStatusCarrier interface {
	GetStatus() string
	GetRetCode() int64
	GetMessage() string
	GetWording() string
}

type baseResponse[T any] struct {
	Status  string `json:"status"`
	RetCode int64  `json:"retcode"`
	Data    T      `json:"data"`
	Message string `json:"message"`
	Wording string `json:"wording"`
	Stream  string `json:"stream"`
}

func (r baseResponse[T]) GetStatus() string  { return r.Status }
func (r baseResponse[T]) GetRetCode() int64  { return r.RetCode }
func (r baseResponse[T]) GetMessage() string { return r.Message }
func (r baseResponse[T]) GetWording() string { return r.Wording }

type sendMessageResponse struct {
	baseResponse[struct {
		MessageID any    `json:"message_id"`
		ForwardID string `json:"forward_id"`
	}]
}

type getMessageData struct {
	Time        int64           `json:"time"`
	MessageType string          `json:"message_type"`
	MessageID   any             `json:"message_id"`
	Message     json.RawMessage `json:"message"`
	RawMessage  string          `json:"raw_message"`
	GroupID     any             `json:"group_id"`
	UserID      any             `json:"user_id"`
	Sender      map[string]any  `json:"sender"`
}

type loginInfoData struct {
	UserID   any    `json:"user_id"`
	Nickname string `json:"nickname"`
}

type statusData struct {
	Online bool           `json:"online"`
	Good   bool           `json:"good"`
	Stat   map[string]any `json:"stat"`
}

type userInfoData struct {
	UserID   any    `json:"user_id"`
	Nickname string `json:"nickname"`
	Remark   string `json:"remark"`
	Sex      string `json:"sex"`
	Age      int    `json:"age"`
}

type groupInfoData struct {
	GroupID        any    `json:"group_id"`
	GroupName      string `json:"group_name"`
	MemberCount    int    `json:"member_count"`
	MaxMemberCount int    `json:"max_member_count"`
}

type groupMemberInfoData struct {
	GroupID      any    `json:"group_id"`
	UserID       any    `json:"user_id"`
	Nickname     string `json:"nickname"`
	Card         string `json:"card"`
	Role         string `json:"role"`
	Sex          string `json:"sex"`
	Age          int    `json:"age"`
	Level        string `json:"level"`
	Title        string `json:"title"`
	Area         string `json:"area"`
	JoinTime     int64  `json:"join_time"`
	LastSentTime int64  `json:"last_sent_time"`
}

type resolvedMediaData struct {
	File     string `json:"file"`
	URL      string `json:"url"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
}
