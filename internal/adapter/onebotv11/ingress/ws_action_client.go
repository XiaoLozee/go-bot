package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	onebotv11 "github.com/XiaoLozee/go-bot/internal/adapter/onebotv11"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type wsActionSession struct {
	conn      *websocket.Conn
	logger    *slog.Logger
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan wsActionResult
}

type wsActionResult struct {
	response wsActionResponse
	err      error
}

type wsActionRequest struct {
	Action string `json:"action"`
	Params any    `json:"params,omitempty"`
	Echo   string `json:"echo,omitempty"`
}

type wsActionResponse struct {
	Status  string          `json:"status"`
	RetCode int64           `json:"retcode"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
	Wording string          `json:"wording"`
	Echo    any             `json:"echo"`
}

type wsActionProbe struct {
	PostType string `json:"post_type"`
	Status   string `json:"status"`
	RetCode  *int64 `json:"retcode"`
	Echo     any    `json:"echo"`
}

type wsActionClient struct {
	id              string
	timeout         time.Duration
	logger          *slog.Logger
	sessionProvider func() *wsActionSession
}

func newWSActionSession(conn *websocket.Conn, logger *slog.Logger) *wsActionSession {
	return &wsActionSession{
		conn:    conn,
		logger:  logger,
		pending: make(map[string]chan wsActionResult),
	}
}

func (s *wsActionSession) handleActionResponse(payload []byte) bool {
	var probe wsActionProbe
	if err := json.Unmarshal(payload, &probe); err != nil {
		return false
	}
	if strings.TrimSpace(probe.PostType) != "" {
		return false
	}
	if probe.Echo == nil && probe.Status == "" && probe.RetCode == nil {
		return false
	}

	var resp wsActionResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		s.logger.Warn("解析 WebSocket 动作响应失败", "error", err)
		return true
	}

	echo := normalizeEcho(resp.Echo)
	if echo == "" {
		s.logger.Warn("收到缺少 echo 的 WebSocket 动作响应")
		return true
	}

	s.pendingMu.Lock()
	ch, ok := s.pending[echo]
	if ok {
		delete(s.pending, echo)
	}
	s.pendingMu.Unlock()
	if !ok {
		return true
	}

	select {
	case ch <- wsActionResult{response: resp}:
	default:
	}
	return true
}

func (s *wsActionSession) registerPending(echo string) chan wsActionResult {
	ch := make(chan wsActionResult, 1)
	s.pendingMu.Lock()
	s.pending[echo] = ch
	s.pendingMu.Unlock()
	return ch
}

func (s *wsActionSession) unregisterPending(echo string) {
	s.pendingMu.Lock()
	delete(s.pending, echo)
	s.pendingMu.Unlock()
}

func (s *wsActionSession) closeWithError(err error) {
	if err == nil {
		err = fmt.Errorf("WebSocket 动作连接已关闭")
	}

	s.pendingMu.Lock()
	pending := s.pending
	s.pending = make(map[string]chan wsActionResult)
	s.pendingMu.Unlock()

	for _, ch := range pending {
		select {
		case ch <- wsActionResult{err: err}:
		default:
		}
	}
}

func (s *wsActionSession) writeJSON(v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.WriteJSON(v)
}

func newWSActionClient(id string, timeout time.Duration, logger *slog.Logger, sessionProvider func() *wsActionSession) *wsActionClient {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &wsActionClient{
		id:              id,
		timeout:         timeout,
		logger:          logger.With("connection", id, "component", "onebot_ws"),
		sessionProvider: sessionProvider,
	}
}

func (c *wsActionClient) ID() string {
	return c.id
}

func (c *wsActionClient) Ready() bool {
	return c.sessionProvider != nil && c.sessionProvider() != nil
}

func (c *wsActionClient) ReadinessReason() string {
	return "等待 WebSocket 动作通道就绪"
}

func (c *wsActionClient) SendMessage(ctx context.Context, req adapter.SendMessageRequest) (*adapter.SendMessageResult, error) {
	action := "send_msg"
	payload := map[string]any{
		"message":     mapSegments(segsOrEmpty(req.Segments)),
		"auto_escape": req.AutoEscape,
	}

	switch req.ChatType {
	case "group":
		action = "send_group_msg"
		payload["group_id"] = req.GroupID
	case "private":
		action = "send_private_msg"
		payload["user_id"] = req.UserID
	default:
		payload["message_type"] = req.ChatType
		payload["group_id"] = req.GroupID
		payload["user_id"] = req.UserID
	}

	var resp struct {
		MessageID any    `json:"message_id"`
		ForwardID string `json:"forward_id"`
	}
	if err := c.doAction(ctx, action, payload, &resp); err != nil {
		return nil, err
	}

	return &adapter.SendMessageResult{
		MessageID: normalizeID(resp.MessageID),
		ForwardID: resp.ForwardID,
	}, nil
}

func (c *wsActionClient) SendGroupForwardMessage(ctx context.Context, req adapter.SendGroupForwardRequest) (*adapter.SendMessageResult, error) {
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

	var resp struct {
		MessageID any    `json:"message_id"`
		ForwardID string `json:"forward_id"`
	}
	if err := c.doAction(ctx, "send_group_forward_msg", payload, &resp); err != nil {
		return nil, err
	}

	return &adapter.SendMessageResult{
		MessageID: normalizeID(resp.MessageID),
		ForwardID: resp.ForwardID,
	}, nil
}

func (c *wsActionClient) DeleteMessage(ctx context.Context, messageID string) error {
	return c.doAction(ctx, "delete_msg", map[string]any{"message_id": messageID}, nil)
}

func (c *wsActionClient) GetMessage(ctx context.Context, messageID string) (*adapter.MessageDetail, error) {
	var resp struct {
		Time        int64           `json:"time"`
		MessageType string          `json:"message_type"`
		MessageID   any             `json:"message_id"`
		Message     json.RawMessage `json:"message"`
		RawMessage  string          `json:"raw_message"`
		GroupID     any             `json:"group_id"`
		UserID      any             `json:"user_id"`
		Sender      map[string]any  `json:"sender"`
	}
	if err := c.doAction(ctx, "get_msg", map[string]any{"message_id": messageID}, &resp); err != nil {
		return nil, err
	}

	return &adapter.MessageDetail{
		Time:        time.Unix(resp.Time, 0),
		MessageType: resp.MessageType,
		MessageID:   normalizeID(resp.MessageID),
		UserID:      normalizeID(resp.UserID),
		GroupID:     normalizeID(resp.GroupID),
		RawMessage:  resp.RawMessage,
		Message:     resp.Message,
		Sender:      resp.Sender,
	}, nil
}

func (c *wsActionClient) GetForwardMessage(ctx context.Context, forwardID string) (*adapter.ForwardMessage, error) {
	var resp json.RawMessage
	if err := c.doAction(ctx, "get_forward_msg", map[string]any{"id": forwardID}, &resp); err != nil {
		return nil, err
	}
	return onebotv11.ParseForwardMessage(forwardID, resp)
}

func (c *wsActionClient) GetRecentMessages(ctx context.Context, req adapter.RecentMessagesRequest) ([]adapter.MessageDetail, error) {
	action, payload, err := onebotv11.RecentMessageAction(req)
	if err != nil {
		return nil, err
	}
	var resp json.RawMessage
	if err := c.doAction(ctx, action, payload, &resp); err != nil {
		return nil, err
	}
	return onebotv11.ParseMessageHistory(resp, req)
}

func (c *wsActionClient) ResolveMedia(ctx context.Context, segmentType, file string) (*adapter.ResolvedMedia, error) {
	segmentType = strings.TrimSpace(strings.ToLower(segmentType))
	file = strings.TrimSpace(file)
	if file == "" {
		return nil, fmt.Errorf("媒体引用不能为空")
	}

	action := "get_file"
	switch segmentType {
	case "image":
		action = "get_image"
	case "record", "audio", "voice":
		action = "get_record"
	case "video", "file":
		action = "get_file"
	}

	var resp struct {
		File     string `json:"file"`
		URL      string `json:"url"`
		FileName string `json:"file_name"`
		FileSize int64  `json:"file_size"`
	}
	if err := c.doAction(ctx, action, map[string]any{"file": file}, &resp); err != nil {
		return nil, err
	}

	return &adapter.ResolvedMedia{
		File:     resp.File,
		URL:      resp.URL,
		FileName: resp.FileName,
		FileSize: resp.FileSize,
	}, nil
}

func (c *wsActionClient) GetLoginInfo(ctx context.Context) (*adapter.LoginInfo, error) {
	var resp struct {
		UserID   any    `json:"user_id"`
		Nickname string `json:"nickname"`
	}
	if err := c.doAction(ctx, "get_login_info", map[string]any{}, &resp); err != nil {
		return nil, err
	}

	return &adapter.LoginInfo{
		UserID:   normalizeID(resp.UserID),
		Nickname: resp.Nickname,
	}, nil
}

func (c *wsActionClient) GetStatus(ctx context.Context) (*adapter.BotStatus, error) {
	var resp struct {
		Online bool           `json:"online"`
		Good   bool           `json:"good"`
		Stat   map[string]any `json:"stat"`
	}
	if err := c.doAction(ctx, "get_status", map[string]any{}, &resp); err != nil {
		return nil, err
	}

	return &adapter.BotStatus{
		Online: resp.Online,
		Good:   resp.Good,
		Stat:   resp.Stat,
	}, nil
}

func (c *wsActionClient) GetStrangerInfo(ctx context.Context, userID string) (*adapter.UserInfo, error) {
	var resp struct {
		UserID   any    `json:"user_id"`
		Nickname string `json:"nickname"`
		Sex      string `json:"sex"`
		Age      int    `json:"age"`
	}
	if err := c.doAction(ctx, "get_stranger_info", map[string]any{"user_id": userID}, &resp); err != nil {
		return nil, err
	}

	return &adapter.UserInfo{
		UserID:   normalizeID(resp.UserID),
		Nickname: resp.Nickname,
		Sex:      resp.Sex,
		Age:      resp.Age,
	}, nil
}

func (c *wsActionClient) GetGroupInfo(ctx context.Context, groupID string) (*adapter.GroupInfo, error) {
	var resp struct {
		GroupID        any    `json:"group_id"`
		GroupName      string `json:"group_name"`
		MemberCount    int    `json:"member_count"`
		MaxMemberCount int    `json:"max_member_count"`
	}
	if err := c.doAction(ctx, "get_group_info", map[string]any{"group_id": groupID}, &resp); err != nil {
		return nil, err
	}

	return &adapter.GroupInfo{
		GroupID:        normalizeID(resp.GroupID),
		GroupName:      resp.GroupName,
		MemberCount:    resp.MemberCount,
		MaxMemberCount: resp.MaxMemberCount,
	}, nil
}

func (c *wsActionClient) GetGroupList(ctx context.Context) ([]adapter.GroupInfo, error) {
	var resp []struct {
		GroupID        any    `json:"group_id"`
		GroupName      string `json:"group_name"`
		MemberCount    int    `json:"member_count"`
		MaxMemberCount int    `json:"max_member_count"`
	}
	if err := c.doAction(ctx, "get_group_list", map[string]any{}, &resp); err != nil {
		return nil, err
	}

	out := make([]adapter.GroupInfo, 0, len(resp))
	for _, item := range resp {
		out = append(out, adapter.GroupInfo{
			GroupID:        normalizeID(item.GroupID),
			GroupName:      item.GroupName,
			MemberCount:    item.MemberCount,
			MaxMemberCount: item.MaxMemberCount,
		})
	}
	return out, nil
}

func (c *wsActionClient) GetFriendList(ctx context.Context) ([]adapter.UserInfo, error) {
	var resp []struct {
		UserID   any    `json:"user_id"`
		Nickname string `json:"nickname"`
		Remark   string `json:"remark"`
		Sex      string `json:"sex"`
		Age      int    `json:"age"`
	}
	if err := c.doAction(ctx, "get_friend_list", map[string]any{}, &resp); err != nil {
		return nil, err
	}

	out := make([]adapter.UserInfo, 0, len(resp))
	for _, item := range resp {
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

func (c *wsActionClient) GetGroupMemberList(ctx context.Context, groupID string) ([]adapter.GroupMemberInfo, error) {
	var resp []struct {
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
	if err := c.doAction(ctx, "get_group_member_list", map[string]any{"group_id": groupID}, &resp); err != nil {
		return nil, err
	}

	out := make([]adapter.GroupMemberInfo, 0, len(resp))
	for _, item := range resp {
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

func (c *wsActionClient) GetGroupMemberInfo(ctx context.Context, groupID, userID string) (*adapter.GroupMemberInfo, error) {
	var resp struct {
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
	if err := c.doAction(ctx, "get_group_member_info", map[string]any{"group_id": groupID, "user_id": userID}, &resp); err != nil {
		return nil, err
	}

	return &adapter.GroupMemberInfo{
		GroupID:  normalizeID(resp.GroupID),
		UserID:   normalizeID(resp.UserID),
		Nickname: resp.Nickname,
		Card:     resp.Card,
		Role:     resp.Role,
		Sex:      resp.Sex,
		Age:      resp.Age,
		Level:    resp.Level,
		Title:    resp.Title,
		Area:     resp.Area,
		JoinTime: resp.JoinTime,
		LastSent: resp.LastSentTime,
	}, nil
}

func (c *wsActionClient) doAction(ctx context.Context, action string, payload any, out any) error {
	if _, ok := ctx.Deadline(); !ok && c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	session, err := c.waitSession(ctx)
	if err != nil {
		return err
	}

	action = strings.TrimSpace(strings.TrimPrefix(action, "/"))
	echo := "go-bot-" + uuid.NewString()
	respCh := session.registerPending(echo)
	req := wsActionRequest{
		Action: action,
		Params: payload,
		Echo:   echo,
	}

	start := time.Now()
	if err := session.writeJSON(req); err != nil {
		session.unregisterPending(echo)
		return fmt.Errorf("发送 WebSocket 动作失败: %w", err)
	}

	select {
	case result := <-respCh:
		if result.err != nil {
			return result.err
		}
		c.logger.Debug("OneBot WebSocket 动作完成", "action", action, "latency_ms", time.Since(start).Milliseconds())
		if result.response.Status != "ok" || result.response.RetCode != 0 {
			return &adapter.ActionError{
				Endpoint: "/" + action,
				RetCode:  result.response.RetCode,
				Message:  result.response.Message,
				Wording:  result.response.Wording,
			}
		}
		if out == nil || len(result.response.Data) == 0 || string(result.response.Data) == "null" {
			return nil
		}
		if err := json.Unmarshal(result.response.Data, out); err != nil {
			return fmt.Errorf("解析 WebSocket 动作响应失败: %w", err)
		}
		return nil
	case <-ctx.Done():
		session.unregisterPending(echo)
		return fmt.Errorf("等待 WebSocket 动作响应失败: %w", ctx.Err())
	}
}

func (c *wsActionClient) waitSession(ctx context.Context) (*wsActionSession, error) {
	if session := c.sessionProvider(); session != nil {
		return session, nil
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("WebSocket 动作通道未就绪: %w", ctx.Err())
		case <-ticker.C:
			if session := c.sessionProvider(); session != nil {
				return session, nil
			}
		}
	}
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

func normalizeEcho(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
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
		return strings.TrimSpace(fmt.Sprintf("%v", x))
	}
}

func segsOrEmpty(items []message.Segment) []message.Segment {
	if items == nil {
		return []message.Segment{}
	}
	return items
}
