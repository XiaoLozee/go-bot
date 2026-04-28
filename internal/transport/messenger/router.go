package messenger

import (
	"context"
	"fmt"
	"sync"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
	"github.com/XiaoLozee/go-bot/internal/plugin/sdk"
)

type Router struct {
	mu        sync.RWMutex
	clients   map[string]adapter.ActionClient
	defaultID string
}

func New() *Router {
	return &Router{
		clients: make(map[string]adapter.ActionClient),
	}
}

func (r *Router) Register(client adapter.ActionClient, isDefault bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[client.ID()] = client
	if isDefault || r.defaultID == "" {
		r.defaultID = client.ID()
	}
}

func (r *Router) Replace(clients []adapter.ActionClient, defaultID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.clients = make(map[string]adapter.ActionClient, len(clients))
	r.defaultID = ""
	for _, client := range clients {
		if client == nil {
			continue
		}
		r.clients[client.ID()] = client
		if r.defaultID == "" {
			r.defaultID = client.ID()
		}
	}
	if defaultID != "" {
		if _, ok := r.clients[defaultID]; ok {
			r.defaultID = defaultID
		}
	}
}

func (r *Router) SendText(ctx context.Context, target message.Target, text string) error {
	return r.SendSegments(ctx, target, []message.Segment{message.Text(text)})
}

func (r *Router) SendSegments(ctx context.Context, target message.Target, segs []message.Segment) error {
	client, err := r.resolveClient(target.ConnectionID)
	if err != nil {
		return err
	}

	_, err = client.SendMessage(ctx, adapter.SendMessageRequest{
		ConnectionID: target.ConnectionID,
		ChatType:     target.ChatType,
		UserID:       target.UserID,
		GroupID:      target.GroupID,
		Segments:     segs,
	})
	return err
}

func (r *Router) ReplyText(ctx context.Context, target message.Target, replyTo string, text string) error {
	segs := []message.Segment{message.Reply(replyTo), message.Text(" " + text)}
	return r.SendSegments(ctx, target, segs)
}

func (r *Router) ResolveMedia(ctx context.Context, connectionID, segmentType, file string) (*adapter.ResolvedMedia, error) {
	client, err := r.resolveClient(connectionID)
	if err != nil {
		return nil, err
	}
	return client.ResolveMedia(ctx, segmentType, file)
}

func (r *Router) ResolveMediaInfo(ctx context.Context, connectionID, segmentType, file string) (*sdk.ResolvedMedia, error) {
	resolved, err := r.ResolveMedia(ctx, connectionID, segmentType, file)
	if err != nil {
		return nil, err
	}
	return &sdk.ResolvedMedia{
		File:     resolved.File,
		URL:      resolved.URL,
		FileName: resolved.FileName,
		FileSize: resolved.FileSize,
	}, nil
}

func (r *Router) SendGroupForward(ctx context.Context, connectionID, groupID string, nodes []message.ForwardNode, opts message.ForwardOptions) error {
	client, err := r.resolveClient(connectionID)
	if err != nil {
		return err
	}

	_, err = client.SendGroupForwardMessage(ctx, adapter.SendGroupForwardRequest{
		ConnectionID: connectionID,
		GroupID:      groupID,
		Nodes:        nodes,
		Options:      opts,
	})
	return err
}

func (r *Router) GetStrangerInfo(ctx context.Context, connectionID, userID string) (*sdk.UserInfo, error) {
	client, err := r.resolveClient(connectionID)
	if err != nil {
		return nil, err
	}

	info, err := client.GetStrangerInfo(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &sdk.UserInfo{
		UserID:   info.UserID,
		Nickname: info.Nickname,
		Sex:      info.Sex,
		Age:      info.Age,
	}, nil
}

func (r *Router) GetGroupInfo(ctx context.Context, connectionID, groupID string) (*sdk.GroupInfo, error) {
	client, err := r.resolveClient(connectionID)
	if err != nil {
		return nil, err
	}

	getter, ok := client.(interface {
		GetGroupInfo(context.Context, string) (*adapter.GroupInfo, error)
	})
	if !ok {
		return nil, fmt.Errorf("当前连接不支持读取群信息")
	}

	info, err := getter.GetGroupInfo(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return &sdk.GroupInfo{
		GroupID:        info.GroupID,
		GroupName:      info.GroupName,
		MemberCount:    info.MemberCount,
		MaxMemberCount: info.MaxMemberCount,
	}, nil
}

func (r *Router) GetGroupList(ctx context.Context, connectionID string) ([]adapter.GroupInfo, error) {
	client, err := r.resolveClient(connectionID)
	if err != nil {
		return nil, err
	}

	getter, ok := client.(interface {
		GetGroupList(context.Context) ([]adapter.GroupInfo, error)
	})
	if !ok {
		return nil, fmt.Errorf("当前连接不支持读取群列表")
	}
	return getter.GetGroupList(ctx)
}

func (r *Router) GetFriendList(ctx context.Context, connectionID string) ([]adapter.UserInfo, error) {
	client, err := r.resolveClient(connectionID)
	if err != nil {
		return nil, err
	}

	getter, ok := client.(interface {
		GetFriendList(context.Context) ([]adapter.UserInfo, error)
	})
	if !ok {
		return nil, fmt.Errorf("当前连接不支持读取好友列表")
	}
	return getter.GetFriendList(ctx)
}

func (r *Router) GetGroupMemberList(ctx context.Context, connectionID, groupID string) ([]sdk.GroupMemberInfo, error) {
	client, err := r.resolveClient(connectionID)
	if err != nil {
		return nil, err
	}

	items, err := client.GetGroupMemberList(ctx, groupID)
	if err != nil {
		return nil, err
	}

	out := make([]sdk.GroupMemberInfo, 0, len(items))
	for _, item := range items {
		out = append(out, sdk.GroupMemberInfo{
			GroupID:  item.GroupID,
			UserID:   item.UserID,
			Nickname: item.Nickname,
			Card:     item.Card,
			Role:     item.Role,
			Sex:      item.Sex,
			Age:      item.Age,
			Level:    item.Level,
			Title:    item.Title,
			Area:     item.Area,
			JoinTime: item.JoinTime,
			LastSent: item.LastSent,
		})
	}
	return out, nil
}

func (r *Router) GetGroupMemberInfo(ctx context.Context, connectionID, groupID, userID string) (*sdk.GroupMemberInfo, error) {
	client, err := r.resolveClient(connectionID)
	if err != nil {
		return nil, err
	}

	item, err := client.GetGroupMemberInfo(ctx, groupID, userID)
	if err != nil {
		return nil, err
	}
	return convertGroupMemberInfo(item), nil
}

func (r *Router) GetMessage(ctx context.Context, connectionID, messageID string) (*sdk.MessageDetail, error) {
	client, err := r.resolveClient(connectionID)
	if err != nil {
		return nil, err
	}

	detail, err := client.GetMessage(ctx, messageID)
	if err != nil {
		return nil, err
	}
	return &sdk.MessageDetail{
		Time:        detail.Time,
		MessageType: detail.MessageType,
		MessageID:   detail.MessageID,
		UserID:      detail.UserID,
		GroupID:     detail.GroupID,
		RawMessage:  detail.RawMessage,
		Message:     detail.Message,
		Sender:      detail.Sender,
	}, nil
}

func (r *Router) GetForwardMessage(ctx context.Context, connectionID, forwardID string) (*adapter.ForwardMessage, error) {
	client, err := r.resolveClient(connectionID)
	if err != nil {
		return nil, err
	}

	getter, ok := client.(interface {
		GetForwardMessage(context.Context, string) (*adapter.ForwardMessage, error)
	})
	if !ok {
		return nil, fmt.Errorf("当前连接不支持读取合并转发消息")
	}
	return getter.GetForwardMessage(ctx, forwardID)
}

func (r *Router) GetForwardMessageInfo(ctx context.Context, connectionID, forwardID string) (*sdk.ForwardMessage, error) {
	item, err := r.GetForwardMessage(ctx, connectionID, forwardID)
	if err != nil {
		return nil, err
	}

	nodes := make([]sdk.ForwardMessageNode, 0, len(item.Nodes))
	for _, node := range item.Nodes {
		nodes = append(nodes, sdk.ForwardMessageNode{
			Time:      node.Time,
			MessageID: node.MessageID,
			UserID:    node.UserID,
			Nickname:  node.Nickname,
			Content:   node.Content,
		})
	}
	return &sdk.ForwardMessage{
		ID:    item.ID,
		Nodes: nodes,
	}, nil
}

func (r *Router) GetRecentMessages(ctx context.Context, req adapter.RecentMessagesRequest) ([]adapter.MessageDetail, error) {
	client, err := r.resolveClient(req.ConnectionID)
	if err != nil {
		return nil, err
	}

	getter, ok := client.(interface {
		GetRecentMessages(context.Context, adapter.RecentMessagesRequest) ([]adapter.MessageDetail, error)
	})
	if !ok {
		return nil, fmt.Errorf("当前连接不支持同步最近消息")
	}

	req.ConnectionID = client.ID()
	return getter.GetRecentMessages(ctx, req)
}

func (r *Router) DeleteMessage(ctx context.Context, connectionID, messageID string) error {
	client, err := r.resolveClient(connectionID)
	if err != nil {
		return err
	}
	return client.DeleteMessage(ctx, messageID)
}

func (r *Router) GetLoginInfo(ctx context.Context, connectionID string) (*sdk.LoginInfo, error) {
	client, err := r.resolveClient(connectionID)
	if err != nil {
		return nil, err
	}

	info, err := client.GetLoginInfo(ctx)
	if err != nil {
		return nil, err
	}
	return &sdk.LoginInfo{
		UserID:   info.UserID,
		Nickname: info.Nickname,
	}, nil
}

func (r *Router) GetStatus(ctx context.Context, connectionID string) (*sdk.BotStatus, error) {
	client, err := r.resolveClient(connectionID)
	if err != nil {
		return nil, err
	}

	status, err := client.GetStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &sdk.BotStatus{
		Online: status.Online,
		Good:   status.Good,
		Stat:   status.Stat,
	}, nil
}

func convertGroupMemberInfo(item *adapter.GroupMemberInfo) *sdk.GroupMemberInfo {
	if item == nil {
		return nil
	}
	return &sdk.GroupMemberInfo{
		GroupID:  item.GroupID,
		UserID:   item.UserID,
		Nickname: item.Nickname,
		Card:     item.Card,
		Role:     item.Role,
		Sex:      item.Sex,
		Age:      item.Age,
		Level:    item.Level,
		Title:    item.Title,
		Area:     item.Area,
		JoinTime: item.JoinTime,
		LastSent: item.LastSent,
	}
}

func (r *Router) resolveClient(connectionID string) (adapter.ActionClient, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if connectionID != "" {
		if client, ok := r.clients[connectionID]; ok {
			return client, nil
		}
		return nil, fmt.Errorf("未找到连接: %s", connectionID)
	}

	if r.defaultID == "" {
		return nil, fmt.Errorf("当前没有可用的默认连接")
	}
	client, ok := r.clients[r.defaultID]
	if !ok {
		return nil, fmt.Errorf("默认连接不存在: %s", r.defaultID)
	}
	return client, nil
}
