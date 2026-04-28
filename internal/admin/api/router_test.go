package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/XiaoLozee/go-bot/internal/adapter"
	"github.com/XiaoLozee/go-bot/internal/ai"
	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/domain/message"
	"github.com/XiaoLozee/go-bot/internal/plugin/host"
	"github.com/XiaoLozee/go-bot/internal/runtime"
)

type mockProvider struct{}

func (m mockProvider) Snapshot() runtime.Snapshot {
	return runtime.Snapshot{State: runtime.StateRunning, AppName: "go-bot", Environment: "test", Connections: 1, Plugins: 2}
}

func (m mockProvider) Metadata() runtime.Metadata {
	return runtime.Metadata{
		AppName:      "go-bot",
		Environment:  "test",
		OwnerQQ:      "123456789",
		AdminEnabled: true,
		WebUIEnabled: true,
		WebUIBaseURL: "/",
		WebUITheme:   config.WebUIThemePinkLight,
		Capabilities: map[string]bool{
			"webui_bootstrap":      true,
			"webui_theme_save":     true,
			"ai_core":              true,
			"ai_config":            true,
			"ai_inspect":           true,
			"ai_message_log":       true,
			"ai_memory_manage":     true,
			"ai_reflection_run":    true,
			"ai_relation_analysis": true,
			"connection_probe":     true,
			"connection_save":      true,
			"connection_delete":    true,
			"config_validate":      true,
			"config_save":          true,
			"config_hot_restart":   true,
			"plugin_install":       true,
			"plugin_upload":        true,
			"plugin_reload":        true,
			"plugin_recover":       true,
			"plugin_uninstall":     true,
			"plugin_api_debug":     true,
		},
	}
}

func (m mockProvider) ConfigView() map[string]any {
	return map[string]any{"security": map[string]any{"admin_auth": map[string]any{"password": "******"}}}
}

func (m mockProvider) AIView() runtime.AIView {
	return runtime.AIView{
		Snapshot: ai.Snapshot{
			Enabled:        true,
			Ready:          true,
			State:          "ready",
			ProviderKind:   "openai_compatible",
			ProviderVendor: "openai",
			Model:          "gpt-4.1-mini",
			SessionCount:   3,
			CandidateCount: 2,
			LongTermCount:  1,
		},
		Config: map[string]any{
			"enabled": true,
			"provider": map[string]any{
				"kind":        "openai_compatible",
				"vendor":      "openai",
				"base_url":    "https://api.openai.com/v1",
				"api_key":     "******",
				"model":       "gpt-4.1-mini",
				"timeout_ms":  30000,
				"temperature": 0.8,
			},
			"private_personas": []map[string]any{
				{
					"id":            "private_gentle",
					"name":          "温柔陪伴",
					"bot_name":      "罗纸酱",
					"system_prompt": "你说话温柔、克制、会照顾对方情绪。",
					"style_tags":    []string{"温柔", "陪伴"},
					"enabled":       true,
				},
			},
			"private_active_persona_id": "private_gentle",
		},
		Debug: ai.DebugView{
			Sessions: []ai.SessionDebugView{{
				Scope:        "group:10001",
				GroupID:      "10001",
				TopicSummary: "最近在聊东方Project",
				RecentCount:  3,
				UpdatedAt:    time.Unix(1710000000, 0),
			}},
			CandidateMemories: []ai.CandidateMemory{{
				ID:            "candidate-1",
				Scope:         "user_in_group",
				MemoryType:    "preference",
				Subtype:       "interest",
				SubjectID:     "20002",
				GroupID:       "10001",
				Content:       "用户喜欢 东方Project",
				Confidence:    0.82,
				EvidenceCount: 3,
				Status:        "pending",
				TTLDays:       30,
				LastSeenAt:    time.Unix(1710000000, 0),
			}},
			LongTermMemories: []ai.LongTermMemory{{
				ID:            "memory-1",
				Scope:         "user_in_group",
				MemoryType:    "semantic",
				Subtype:       "preference",
				SubjectID:     "20002",
				GroupID:       "10001",
				Content:       "用户喜欢 东方Project",
				Confidence:    0.9,
				EvidenceCount: 4,
				TTLDays:       180,
				UpdatedAt:     time.Unix(1710000000, 0),
			}},
		},
		Skills: []ai.SkillView{{
			ProviderID:  "builtin.core",
			Source:      "builtin",
			Namespace:   "builtin.core",
			Name:        "核心技能",
			Description: "AI 内置工具能力",
			ToolCount:   2,
			Tools: []ai.SkillToolView{{
				Name:        "send_message_current",
				Description: "发送当前会话消息",
			}},
		}},
		InstalledSkills: []runtime.AIInstalledSkillView{{
			ID:                 "demo-skill",
			Name:               "Demo Skill",
			Description:        "A prompt skill imported from GitHub.",
			SourceType:         "github",
			SourceLabel:        "GitHub",
			SourceURL:          "https://github.com/demo/demo-skill",
			Provider:           "GitHub",
			Enabled:            true,
			InstalledAt:        time.Unix(1710000000, 0),
			UpdatedAt:          time.Unix(1710003600, 0),
			EntryPath:          "SKILL.md",
			Format:             "zip",
			InstructionPreview: "# Demo Skill",
			ContentLength:      128,
		}},
	}
}

func (m mockProvider) WebUIBootstrap() runtime.WebUIBootstrap {
	return runtime.WebUIBootstrap{
		GeneratedAt: time.Unix(1710000000, 0),
		Meta:        m.Metadata(),
		Runtime:     m.Snapshot(),
		AI:          m.AIView(),
		Connections: m.ConnectionSnapshots(),
		Plugins:     m.PluginSnapshots(),
		Config:      m.ConfigView(),
	}
}

func (m mockProvider) ConnectionSnapshots() []adapter.ConnectionSnapshot {
	return []adapter.ConnectionSnapshot{{ID: "napcat-main", Platform: "onebot_v11"}}
}

func (m mockProvider) ConnectionDetail(id string) (runtime.ConnectionDetail, bool) {
	if id != "napcat-main" {
		return runtime.ConnectionDetail{}, false
	}
	return runtime.ConnectionDetail{
		Snapshot: adapter.ConnectionSnapshot{ID: "napcat-main", Platform: "onebot_v11"},
		Config:   map[string]any{"id": "napcat-main", "action": map[string]any{"access_token": "******"}},
	}, true
}

func (m mockProvider) RefreshConnection(context.Context, string) (runtime.ConnectionDetail, error) {
	return runtime.ConnectionDetail{
		Snapshot: adapter.ConnectionSnapshot{
			ID:           "napcat-main",
			Platform:     "onebot_v11",
			State:        adapter.ConnectionRunning,
			IngressState: adapter.ConnectionRunning,
			Online:       true,
			Good:         true,
		},
		Config: map[string]any{"id": "napcat-main", "action": map[string]any{"access_token": "******"}},
	}, nil
}

func (m mockProvider) SaveConnectionConfig(_ context.Context, conn config.ConnectionConfig) (runtime.ConnectionSaveResult, error) {
	if strings.TrimSpace(conn.Platform) == "" {
		conn.Platform = "onebot_v11"
	}
	if strings.TrimSpace(conn.Ingress.Type) == "" {
		conn.Ingress.Type = "ws_server"
	}
	if strings.TrimSpace(conn.Action.Type) == "" {
		conn.Action.Type = "napcat_http"
	}
	if conn.Action.TimeoutMS <= 0 {
		conn.Action.TimeoutMS = 10000
	}
	detail := runtime.ConnectionDetail{
		Snapshot: adapter.ConnectionSnapshot{
			ID:          conn.ID,
			Platform:    conn.Platform,
			Enabled:     conn.Enabled,
			IngressType: conn.Ingress.Type,
			ActionType:  conn.Action.Type,
		},
		Config: map[string]any{
			"id":       conn.ID,
			"enabled":  conn.Enabled,
			"platform": conn.Platform,
			"ingress": map[string]any{
				"type":              conn.Ingress.Type,
				"listen":            conn.Ingress.Listen,
				"path":              conn.Ingress.Path,
				"url":               conn.Ingress.URL,
				"retry_interval_ms": conn.Ingress.RetryIntervalMS,
			},
			"action": map[string]any{
				"type":         conn.Action.Type,
				"base_url":     conn.Action.BaseURL,
				"timeout_ms":   conn.Action.TimeoutMS,
				"access_token": conn.Action.AccessToken,
			},
		},
	}
	return runtime.ConnectionSaveResult{
		Accepted:     true,
		Persisted:    true,
		ConnectionID: conn.ID,
		Detail:       detail,
		Message:      "网络配置已保存并已热应用",
	}, nil
}

func (m mockProvider) SetConnectionEnabled(_ context.Context, id string, enabled bool) (runtime.ConnectionSaveResult, error) {
	return runtime.ConnectionSaveResult{
		Accepted:     true,
		Persisted:    true,
		HotApplied:   true,
		ConnectionID: id,
		Detail: runtime.ConnectionDetail{
			Snapshot: adapter.ConnectionSnapshot{
				ID:           id,
				Platform:     "onebot_v11",
				Enabled:      enabled,
				State:        adapter.ConnectionRunning,
				IngressState: adapter.ConnectionRunning,
			},
			Config: map[string]any{
				"id":       id,
				"enabled":  enabled,
				"platform": "onebot_v11",
			},
		},
		Message: "网络连接已启停并已热应用",
	}, nil
}

func (m mockProvider) DeleteConnection(_ context.Context, id string) (runtime.ConnectionSaveResult, error) {
	return runtime.ConnectionSaveResult{
		Accepted:     true,
		Persisted:    true,
		ConnectionID: id,
		Message:      "网络配置已删除并已热应用",
	}, nil
}

func (m mockProvider) PluginSnapshots() []host.Snapshot {
	return []host.Snapshot{
		{ID: "menu_hint", Name: "菜单提示", Kind: "builtin", Enabled: true, Configured: true, Builtin: true},
		{ID: "ext_demo", Name: "External Demo", Kind: "external_exec", Enabled: true, Configured: true},
	}
}

func (m mockProvider) PluginDetail(id string) (runtime.PluginDetail, bool) {
	switch id {
	case "menu_hint":
		return runtime.PluginDetail{
			Snapshot: host.Snapshot{ID: "menu_hint", Name: "菜单提示", Kind: "builtin", Enabled: true, Configured: true, Builtin: true},
			Config:   map[string]any{"header_text": "✨ Go-bot 菜单"},
		}, true
	case "ext_demo":
		return runtime.PluginDetail{
			Snapshot: host.Snapshot{ID: "ext_demo", Name: "External Demo", Kind: "external_exec", Enabled: true, Configured: true},
			Config:   map[string]any{"mode": "debug"},
		}, true
	default:
		return runtime.PluginDetail{}, false
	}
}

func (m mockProvider) InstallPluginPackage(context.Context, string, []byte, bool) (runtime.PluginInstallResult, error) {
	return runtime.PluginInstallResult{
		PluginID:     "uploaded_demo",
		Kind:         "external_exec",
		Format:       "zip",
		InstalledTo:  "plugins/uploaded_demo",
		ManifestPath: "plugins/uploaded_demo/plugin.yaml",
		Reloaded:     true,
		Message:      "插件包已安装",
	}, nil
}

func (m mockProvider) InstallPlugin(context.Context, string) error   { return nil }
func (m mockProvider) StartPlugin(context.Context, string) error     { return nil }
func (m mockProvider) StopPlugin(context.Context, string) error      { return nil }
func (m mockProvider) ReloadPlugin(context.Context, string) error    { return nil }
func (m mockProvider) RecoverPlugin(context.Context, string) error   { return nil }
func (m mockProvider) UninstallPlugin(context.Context, string) error { return nil }
func (m mockProvider) DebugPluginAPI(_ context.Context, id string, req runtime.PluginAPIDebugRequest) (runtime.PluginAPIDebugResult, error) {
	return runtime.PluginAPIDebugResult{
		Accepted: true,
		PluginID: id,
		Method:   req.Method,
		Result:   map[string]any{"echo": req.Payload},
		Message:  "接口调用成功",
	}, nil
}
func (m mockProvider) DebugFrameworkPluginAPI(_ context.Context, req runtime.PluginAPIDebugRequest) (runtime.PluginAPIDebugResult, error) {
	return runtime.PluginAPIDebugResult{
		Accepted: true,
		Method:   req.Method,
		Result:   map[string]any{"echo": req.Payload},
		Message:  "接口调用成功",
	}, nil
}
func (m mockProvider) SavePluginConfig(context.Context, string, bool, map[string]any) (runtime.PluginConfigSaveResult, error) {
	return runtime.PluginConfigSaveResult{
		Accepted:  true,
		Persisted: true,
		PluginID:  "menu_hint",
		Detail: runtime.PluginDetail{
			Snapshot: host.Snapshot{ID: "menu_hint", Name: "菜单提示", Kind: "builtin", Enabled: true, Configured: true},
			Config:   map[string]any{"header_text": "✨ Go-bot 菜单"},
		},
		Message: "插件配置已保存并已热应用",
	}, nil
}
func (m mockProvider) SaveAIConfig(context.Context, config.AIConfig) (runtime.AISaveResult, error) {
	return runtime.AISaveResult{
		Accepted:   true,
		Persisted:  true,
		HotApplied: true,
		View:       m.AIView(),
		Message:    "AI 配置已保存并已在线生效",
	}, nil
}
func (m mockProvider) ListAIInstalledSkills(context.Context) ([]runtime.AIInstalledSkillView, error) {
	return m.AIView().InstalledSkills, nil
}
func (m mockProvider) GetAIInstalledSkill(_ context.Context, id string) (runtime.AIInstalledSkillDetailView, error) {
	if id != "demo-skill" {
		return runtime.AIInstalledSkillDetailView{}, os.ErrNotExist
	}
	return runtime.AIInstalledSkillDetailView{
		AIInstalledSkillView: runtime.AIInstalledSkillView{
			ID:                 "demo-skill",
			Name:               "Demo Skill",
			Description:        "A prompt skill imported from GitHub.",
			SourceType:         "github",
			SourceLabel:        "GitHub",
			SourceURL:          "https://github.com/demo/demo-skill",
			Provider:           "GitHub",
			Enabled:            true,
			InstalledAt:        time.Unix(1710000000, 0),
			UpdatedAt:          time.Unix(1710003600, 0),
			EntryPath:          "SKILL.md",
			Format:             "zip",
			InstructionPreview: "# Demo Skill",
			ContentLength:      128,
		},
		Content: "# Demo Skill\n\nUse this skill when the user asks for code review.",
	}, nil
}
func (m mockProvider) InstallAIInstalledSkillPackage(_ context.Context, fileName string, _ []byte, overwrite bool) (runtime.AISkillInstallResult, error) {
	return runtime.AISkillInstallResult{
		Accepted:    true,
		Replaced:    overwrite,
		InstalledTo: "data/skills/items/demo-skill",
		BackupPath:  "data/skills/.bak/demo-skill-20260419-100000.000",
		Skill: runtime.AIInstalledSkillDetailView{
			AIInstalledSkillView: runtime.AIInstalledSkillView{
				ID:                 "demo-skill",
				Name:               "Demo Skill",
				Description:        "A prompt skill imported from GitHub.",
				SourceType:         "github",
				SourceLabel:        "GitHub",
				SourceURL:          "https://github.com/demo/demo-skill",
				Provider:           "GitHub",
				Enabled:            true,
				InstalledAt:        time.Unix(1710000000, 0),
				UpdatedAt:          time.Unix(1710003600, 0),
				EntryPath:          "SKILL.md",
				Format:             "zip",
				InstructionPreview: "# Demo Skill",
				ContentLength:      128,
			},
			Content: "# Demo Skill\n\nInstalled from package.",
		},
		View:    m.AIView(),
		Message: "技能包已安装",
	}, nil
}
func (m mockProvider) InstallAIInstalledSkillFromURL(_ context.Context, sourceURL string, overwrite bool) (runtime.AISkillInstallResult, error) {
	result, _ := m.InstallAIInstalledSkillPackage(context.Background(), sourceURL, nil, overwrite)
	result.Skill.SourceURL = sourceURL
	result.Message = "技能已从链接导入"
	return result, nil
}
func (m mockProvider) SetAIInstalledSkillEnabled(_ context.Context, id string, enabled bool) (runtime.AISkillActionResult, error) {
	item := runtime.AIInstalledSkillView{
		ID:                 id,
		Name:               "Demo Skill",
		Description:        "A prompt skill imported from GitHub.",
		SourceType:         "github",
		SourceLabel:        "GitHub",
		SourceURL:          "https://github.com/demo/demo-skill",
		Provider:           "GitHub",
		Enabled:            enabled,
		InstalledAt:        time.Unix(1710000000, 0),
		UpdatedAt:          time.Unix(1710003600, 0),
		EntryPath:          "SKILL.md",
		Format:             "zip",
		InstructionPreview: "# Demo Skill",
		ContentLength:      128,
	}
	return runtime.AISkillActionResult{
		Accepted: true,
		Action:   map[bool]string{true: "enable", false: "disable"}[enabled],
		ID:       id,
		Enabled:  enabled,
		Skill:    &item,
		View:     m.AIView(),
		Message:  "技能状态已更新",
	}, nil
}
func (m mockProvider) UninstallAIInstalledSkill(_ context.Context, id string) (runtime.AISkillActionResult, error) {
	item := runtime.AIInstalledSkillView{ID: id, Name: "Demo Skill", SourceType: "github", SourceLabel: "GitHub"}
	return runtime.AISkillActionResult{
		Accepted: true,
		Action:   "uninstall",
		ID:       id,
		Skill:    &item,
		View:     m.AIView(),
		Message:  "技能已卸载",
	}, nil
}
func (m mockProvider) ListAIMessageLogs(_ context.Context, query ai.MessageLogQuery) (runtime.AIMessageListView, error) {
	chatType := strings.TrimSpace(query.ChatType)
	if chatType == "" {
		chatType = "group"
	}
	return runtime.AIMessageListView{
		Query: query,
		Items: []ai.MessageLog{
			{
				MessageID:     "msg-1",
				ConnectionID:  "napcat-main",
				ChatType:      chatType,
				GroupID:       "10001",
				UserID:        "20002",
				SenderRole:    "user",
				SenderName:    "Alice",
				TextContent:   "这是一条测试消息",
				HasText:       true,
				HasImage:      true,
				MessageStatus: "normal",
				OccurredAt:    time.Unix(1710000000, 0),
				CreatedAt:     time.Unix(1710000000, 0),
				ImageCount:    1,
			},
		},
	}, nil
}
func (m mockProvider) ListAIMessageSuggestions(_ context.Context, query ai.MessageSuggestionQuery) (ai.MessageSearchSuggestions, error) {
	result := ai.MessageSearchSuggestions{
		Groups: []string{"10001", "10088"},
		Users:  []string{"20002", "20099"},
	}
	if strings.TrimSpace(strings.ToLower(query.ChatType)) == "private" {
		result.Groups = nil
	}
	return result, nil
}
func (m mockProvider) GetAIMessageDetail(context.Context, string) (runtime.AIMessageDetailView, error) {
	return runtime.AIMessageDetailView{
		Item: ai.MessageDetail{
			Message: ai.MessageLog{
				MessageID:     "msg-1",
				ConnectionID:  "napcat-main",
				ChatType:      "group",
				GroupID:       "10001",
				UserID:        "20002",
				SenderRole:    "user",
				SenderName:    "Alice",
				TextContent:   "这是一条测试消息",
				HasText:       true,
				HasImage:      true,
				MessageStatus: "normal",
				OccurredAt:    time.Unix(1710000000, 0),
				CreatedAt:     time.Unix(1710000000, 0),
				ImageCount:    1,
			},
			Images: []ai.MessageImage{
				{
					ID:            "msg-1#00",
					MessageID:     "msg-1",
					SegmentIndex:  0,
					OriginRef:     "https://example.com/cat.png",
					VisionSummary: "一张猫咪表情包",
					VisionStatus:  "ready",
					PublicURL:     "https://cdn.example.com/cat.png",
					PreviewURL:    "/api/admin/ai/messages/msg-1/images/0/content",
					CreatedAt:     time.Unix(1710000000, 0),
				},
			},
		},
	}, nil
}
func (m mockProvider) GetAIForwardMessage(_ context.Context, connectionID, forwardID string) (runtime.AIForwardMessageView, error) {
	return runtime.AIForwardMessageView{
		ConnectionID: connectionID,
		ForwardID:    forwardID,
		Nodes: []adapter.ForwardMessageNode{
			{
				Time:     time.Unix(1710000000, 0),
				UserID:   "20002",
				Nickname: "Alice",
				Content:  []message.Segment{message.Text("合并转发节点")},
			},
		},
		FetchedAt: time.Unix(1710000000, 0),
	}, nil
}
func (m mockProvider) SendAIMessage(_ context.Context, req runtime.AIMessageSendRequest) (runtime.AIMessageSendResult, error) {
	return runtime.AIMessageSendResult{
		Accepted:     true,
		ConnectionID: req.ConnectionID,
		ChatType:     req.ChatType,
		GroupID:      req.GroupID,
		UserID:       req.UserID,
		SentAt:       time.Unix(1710000000, 0),
		Message:      "消息已发送",
	}, nil
}
func (m mockProvider) DiscoverAIProviderModels(context.Context, config.AIProviderConfig) (runtime.AIProviderModelsResult, error) {
	return runtime.AIProviderModelsResult{
		Accepted: true,
		Models: []runtime.AIProviderModel{
			{ID: "gpt-test", OwnedBy: "openai"},
			{ID: "deepseek-chat", OwnedBy: "deepseek"},
		},
		FetchedAt: time.Unix(1710000000, 0),
		Message:   "已获取 2 个可用模型",
	}, nil
}
func (m mockProvider) SyncAIRecentMessages(_ context.Context, req runtime.AIRecentMessagesSyncRequest) (runtime.AIRecentMessagesSyncResult, error) {
	return runtime.AIRecentMessagesSyncResult{
		Accepted:     true,
		ConnectionID: firstNonEmpty(req.ConnectionID, "napcat-main"),
		ChatType:     firstNonEmpty(req.ChatType, "group"),
		GroupID:      req.GroupID,
		UserID:       req.UserID,
		Requested:    req.Count,
		Fetched:      1,
		Synced:       1,
		SyncedAt:     time.Unix(1710000000, 0),
		Message:      "已同步 1 条最近消息",
	}, nil
}
func (m mockProvider) SyncAllAIRecentMessages(_ context.Context, req runtime.AIRecentMessagesBulkSyncRequest) (runtime.AIRecentMessagesBulkSyncResult, error) {
	return runtime.AIRecentMessagesBulkSyncResult{
		Accepted:     true,
		ConnectionID: firstNonEmpty(req.ConnectionID, "napcat-main"),
		ChatType:     firstNonEmpty(req.ChatType, "group"),
		Targets:      2,
		Requested:    req.Count,
		Fetched:      4,
		Synced:       3,
		Failed:       1,
		SyncedAt:     time.Unix(1710000000, 0),
		Message:      "同步 2 个群聊，读取 4 条，写入 3 条，失败 1 个",
	}, nil
}
func (m mockProvider) ResolveAIMessageImagePreview(context.Context, string, int) (ai.MessageImagePreview, error) {
	return ai.MessageImagePreview{
		MessageID:    "msg-1",
		SegmentIndex: 0,
		MimeType:     "image/png",
		RedirectURL:  "https://cdn.example.com/cat.png",
	}, nil
}
func (m mockProvider) RunAIReflection(context.Context) (runtime.AIMemoryActionResult, error) {
	return runtime.AIMemoryActionResult{
		Accepted: true,
		Action:   "run",
		Target:   "reflection",
		ID:       "core",
		View:     m.AIView(),
		Message:  "晋升 1 条候选记忆 · 清理候选 0 条 · 清理长期记忆 0 条",
	}, nil
}
func (m mockProvider) AnalyzeAIRelations(context.Context, runtime.AIRelationAnalysisRequest) (runtime.AIRelationAnalysisResult, error) {
	return runtime.AIRelationAnalysisResult{
		Accepted:    true,
		GroupID:     "10001",
		Markdown:    "# 群友关系与性格分析\n",
		GeneratedAt: time.Unix(1710000000, 0),
		UserCount:   2,
		EdgeCount:   1,
		MemoryCount: 1,
		Message:     "AI 关系分析已生成",
	}, nil
}
func (m mockProvider) StartAIRelationAnalysis(context.Context, runtime.AIRelationAnalysisRequest) (runtime.AIRelationAnalysisTaskView, error) {
	startedAt := time.Unix(1710000000, 0)
	return runtime.AIRelationAnalysisTaskView{
		Accepted:  true,
		TaskID:    "rel-1710000000000-1",
		Status:    "running",
		GroupID:   "10001",
		CreatedAt: startedAt,
		StartedAt: &startedAt,
		Message:   "AI 关系分析任务已提交",
	}, nil
}
func (m mockProvider) GetAIRelationAnalysisTask(context.Context, string) (runtime.AIRelationAnalysisTaskView, error) {
	finishedAt := time.Unix(1710000060, 0)
	return runtime.AIRelationAnalysisTaskView{
		Accepted:   true,
		TaskID:     "rel-1710000000000-1",
		Status:     "succeeded",
		GroupID:    "10001",
		CreatedAt:  time.Unix(1710000000, 0),
		FinishedAt: &finishedAt,
		Result: &runtime.AIRelationAnalysisResult{
			Accepted:    true,
			GroupID:     "10001",
			Markdown:    "# 群友关系与性格分析\n",
			GeneratedAt: time.Unix(1710000000, 0),
			UserCount:   2,
			EdgeCount:   1,
			MemoryCount: 1,
			Message:     "AI 关系分析已生成",
		},
		Message: "AI 关系分析已生成",
	}, nil
}
func (m mockProvider) PromoteAICandidateMemory(context.Context, string) (runtime.AIMemoryActionResult, error) {
	return runtime.AIMemoryActionResult{
		Accepted: true,
		Action:   "promote",
		Target:   "candidate_memory",
		ID:       "candidate-1",
		View:     m.AIView(),
		Message:  "候选记忆已晋升为长期记忆",
	}, nil
}
func (m mockProvider) DeleteAICandidateMemory(context.Context, string) (runtime.AIMemoryActionResult, error) {
	return runtime.AIMemoryActionResult{
		Accepted: true,
		Action:   "delete",
		Target:   "candidate_memory",
		ID:       "candidate-1",
		View:     m.AIView(),
		Message:  "候选记忆已删除",
	}, nil
}
func (m mockProvider) DeleteAILongTermMemory(context.Context, string) (runtime.AIMemoryActionResult, error) {
	return runtime.AIMemoryActionResult{
		Accepted: true,
		Action:   "delete",
		Target:   "long_term_memory",
		ID:       "memory-1",
		View:     m.AIView(),
		Message:  "长期记忆已删除",
	}, nil
}
func (m mockProvider) SaveConfig(context.Context, *config.Config) (runtime.ConfigSaveResult, error) {
	return runtime.ConfigSaveResult{
		Accepted:          true,
		Persisted:         true,
		RestartRequired:   true,
		PluginChanged:     true,
		HotApplyAttempted: true,
		HotApplied:        false,
		HotApplyError:     "启动插件 menu_hint 失败: boom",
		SourcePath:        "configs/config.example.yml",
		Path:              "configs/config.yml",
		BackupPath:        "configs/.bak/config-20260418-120000.000.yml",
		SavedAt:           time.Unix(1710000100, 0),
		NormalizedConfig:  map[string]any{"security": map[string]any{"admin_auth": map[string]any{"password": "******"}}},
		Message:           "插件配置已保存，但热应用失败，请重启实例后生效",
	}, nil
}
func (m mockProvider) HotRestart(context.Context) (runtime.RuntimeRestartResult, error) {
	return runtime.RuntimeRestartResult{
		Accepted:    true,
		Restarted:   true,
		State:       runtime.StateRunning,
		RestartedAt: time.Unix(1710000200, 0),
		Message:     "运行时已按已保存配置完成热重启",
	}, nil
}
func (m mockProvider) SaveWebUITheme(context.Context, string) (runtime.ConfigSaveResult, error) {
	return runtime.ConfigSaveResult{
		Accepted:         true,
		Persisted:        true,
		RestartRequired:  false,
		Path:             "configs/config.yml",
		BackupPath:       "configs/.bak/config-20260418-120000.000.yml",
		SavedAt:          time.Unix(1710000120, 0),
		NormalizedConfig: map[string]any{"server": map[string]any{"webui": map[string]any{"theme": config.WebUIThemeBlueLight}}},
		Message:          "WebUI 主题已保存并立即生效",
	}, nil
}
func (m mockProvider) AdminAuthStatus() runtime.AuthStatus {
	return runtime.AuthStatus{
		Enabled:       false,
		Configured:    true,
		RequiresSetup: false,
	}
}
func (m mockProvider) ConfigureAdminAuth(context.Context, string) (runtime.ConfigSaveResult, error) {
	return runtime.ConfigSaveResult{}, nil
}
func (m mockProvider) ChangeAdminPassword(context.Context, string, string) (runtime.ConfigSaveResult, error) {
	return runtime.ConfigSaveResult{
		Accepted:        true,
		Persisted:       true,
		RestartRequired: false,
		Path:            "configs/config.yml",
		Message:         "后台密码已更新",
	}, nil
}
func (m mockProvider) VerifyAdminPassword(password string) bool { return password == "secret" }
func (m mockProvider) AuditLogs(limit int) []runtime.AuditLogEntry {
	items := []runtime.AuditLogEntry{
		{
			At:       time.Unix(1710000200, 0),
			Category: "plugin",
			Action:   "recover",
			Target:   "menu_hint",
			Result:   "success",
			Summary:  "恢复插件成功",
			Username: "admin",
		},
		{
			At:       time.Unix(1710000100, 0),
			Category: "auth",
			Action:   "login",
			Target:   "admin",
			Result:   "failed",
			Summary:  "后台登录失败",
			Detail:   "密码错误",
			Username: "admin",
		},
	}
	if limit > 0 && limit < len(items) {
		return items[:limit]
	}
	return items
}
func (m mockProvider) RecordAuditLog(runtime.AuditLogEntry) {}

type setupMockProvider struct {
	mockProvider
	configured bool
}

func (m *setupMockProvider) AdminAuthStatus() runtime.AuthStatus {
	return runtime.AuthStatus{
		Enabled:       m.configured,
		Configured:    m.configured,
		RequiresSetup: !m.configured,
	}
}

func (m *setupMockProvider) ConfigureAdminAuth(context.Context, string) (runtime.ConfigSaveResult, error) {
	m.configured = true
	return runtime.ConfigSaveResult{
		Accepted:        true,
		Persisted:       true,
		RestartRequired: false,
		Path:            "configs/config.yml",
		Message:         "后台密码已设置",
	}, nil
}

type protectedMockProvider struct{ mockProvider }

type logViewMockProvider struct {
	mockProvider
	logDir string
}

func (m logViewMockProvider) ConfigView() map[string]any {
	return map[string]any{
		"storage": map[string]any{
			"logs": map[string]any{
				"dir": m.logDir,
			},
		},
	}
}

type requestContextKey string

type configContextProvider struct {
	mockProvider
	trace string
}

func (p *configContextProvider) SaveConfig(ctx context.Context, cfg *config.Config) (runtime.ConfigSaveResult, error) {
	if value, ok := ctx.Value(requestContextKey("trace")).(string); ok {
		p.trace = value
	}
	return p.mockProvider.SaveConfig(ctx, cfg)
}

type authSetupContextProvider struct {
	mockProvider
	trace string
}

func (p *authSetupContextProvider) AdminAuthStatus() runtime.AuthStatus {
	return runtime.AuthStatus{
		Enabled:       true,
		Configured:    false,
		RequiresSetup: true,
	}
}

func (p *authSetupContextProvider) ConfigureAdminAuth(ctx context.Context, password string) (runtime.ConfigSaveResult, error) {
	if value, ok := ctx.Value(requestContextKey("trace")).(string); ok {
		p.trace = value
	}
	return runtime.ConfigSaveResult{
		Accepted:        true,
		Persisted:       true,
		RestartRequired: false,
		Message:         "后台密码已设置",
	}, nil
}

type actionTrackingMockProvider struct {
	protectedMockProvider
	lastAction   string
	lastDebug    runtime.PluginAPIDebugRequest
	auditEntries []runtime.AuditLogEntry
}

type uploadTrackingMockProvider struct {
	protectedMockProvider
	fileName  string
	payload   []byte
	overwrite bool
}

func (m *actionTrackingMockProvider) InstallPlugin(context.Context, string) error {
	m.lastAction = "install"
	return nil
}

func (m *actionTrackingMockProvider) StartPlugin(context.Context, string) error {
	m.lastAction = "start"
	return nil
}

func (m *actionTrackingMockProvider) StopPlugin(context.Context, string) error {
	m.lastAction = "stop"
	return nil
}

func (m *actionTrackingMockProvider) ReloadPlugin(context.Context, string) error {
	m.lastAction = "reload"
	return nil
}

func (m *actionTrackingMockProvider) RecoverPlugin(context.Context, string) error {
	m.lastAction = "recover"
	return nil
}

func (m *actionTrackingMockProvider) UninstallPlugin(context.Context, string) error {
	m.lastAction = "uninstall"
	return nil
}

func (m *actionTrackingMockProvider) SavePluginConfig(_ context.Context, _ string, _ bool, _ map[string]any) (runtime.PluginConfigSaveResult, error) {
	m.lastAction = "config"
	return runtime.PluginConfigSaveResult{
		Accepted:  true,
		Persisted: true,
		PluginID:  "menu_hint",
		Message:   "插件配置已保存并已热应用",
	}, nil
}

func (m *actionTrackingMockProvider) DebugPluginAPI(_ context.Context, id string, req runtime.PluginAPIDebugRequest) (runtime.PluginAPIDebugResult, error) {
	m.lastAction = "debug-api"
	m.lastDebug = req
	return runtime.PluginAPIDebugResult{
		Accepted: true,
		PluginID: id,
		Method:   req.Method,
		Result:   map[string]any{"ok": true},
		Message:  "接口调用成功",
	}, nil
}

func (m *actionTrackingMockProvider) DebugFrameworkPluginAPI(_ context.Context, req runtime.PluginAPIDebugRequest) (runtime.PluginAPIDebugResult, error) {
	m.lastAction = "framework-debug-api"
	m.lastDebug = req
	return runtime.PluginAPIDebugResult{
		Accepted: true,
		Method:   req.Method,
		Result:   map[string]any{"ok": true},
		Message:  "接口调用成功",
	}, nil
}

func (m *actionTrackingMockProvider) AuditLogs(limit int) []runtime.AuditLogEntry {
	if limit <= 0 || limit >= len(m.auditEntries) {
		return append([]runtime.AuditLogEntry(nil), m.auditEntries...)
	}
	return append([]runtime.AuditLogEntry(nil), m.auditEntries[:limit]...)
}

func (m *actionTrackingMockProvider) RecordAuditLog(entry runtime.AuditLogEntry) {
	m.auditEntries = append([]runtime.AuditLogEntry{entry}, m.auditEntries...)
}

func (m *uploadTrackingMockProvider) InstallPluginPackage(_ context.Context, fileName string, payload []byte, overwrite bool) (runtime.PluginInstallResult, error) {
	m.fileName = fileName
	m.payload = append([]byte(nil), payload...)
	m.overwrite = overwrite
	return runtime.PluginInstallResult{
		PluginID:     "uploaded_demo",
		Kind:         "external_exec",
		Format:       "zip",
		InstalledTo:  "plugins/uploaded_demo",
		ManifestPath: "plugins/uploaded_demo/plugin.yaml",
		BackupPath:   "plugins/.bak/uploaded_demo-20260419-100000.000",
		Replaced:     overwrite,
		Reloaded:     true,
		Message:      "插件包已安装",
	}, nil
}

func (m protectedMockProvider) AdminAuthStatus() runtime.AuthStatus {
	return runtime.AuthStatus{
		Enabled:       true,
		Configured:    true,
		RequiresSetup: false,
	}
}

func loginProtectedRoute(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login cookies = 0, want session cookie")
	}
	return cookies[0]
}

func TestNewRouter_WebUIBootstrap(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/webui/bootstrap", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, ok := body["meta"]; !ok {
		t.Fatalf("response = %s, want meta field", rec.Body.String())
	}
	if _, ok := body["runtime"]; !ok {
		t.Fatalf("response = %s, want runtime field", rec.Body.String())
	}
	meta, _ := body["meta"].(map[string]any)
	if got := meta["webui_theme"]; got != config.WebUIThemePinkLight {
		t.Fatalf("webui_theme = %v, want %q", got, config.WebUIThemePinkLight)
	}
}

func TestNewRouter_WebUIIndex(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Go-bot Admin Console") {
		t.Fatalf("body = %s, want admin console page", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "legacyEntryPath") {
		t.Fatalf("body = %s, want legacy runtime config removed", rec.Body.String())
	}
}

func TestNewRouter_SaveAIConfig(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})

	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}

	body := bytes.NewBufferString(`{
		"enabled": true,
		"provider": {
			"kind": "openai_compatible",
			"vendor": "openai",
			"base_url": "https://api.openai.com/v1",
			"api_key": "sk-test",
			"model": "gpt-4.1-mini",
			"timeout_ms": 30000,
			"temperature": 0.8
		},
		"reply": {
			"enabled_in_group": true,
			"enabled_in_private": true,
			"reply_on_at": true,
			"reply_on_bot_name": false,
			"reply_on_quote": true,
			"cooldown_seconds": 20,
			"max_context_messages": 16,
			"max_output_tokens": 160
		},
		"memory": {
			"enabled": true,
			"candidate_enabled": true,
			"session_window": 24,
			"promote_threshold": 2,
			"max_prompt_long_term": 4,
			"max_prompt_candidates": 3,
			"reflection_raw_limit": 768,
			"reflection_per_group_limit": 36
		},
		"prompt": {
			"bot_name": "罗纸酱",
			"system_prompt": "你是测试机器人"
		}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/ai/save", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(loginRec.Result().Cookies()[0])
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "AI 配置已保存") {
		t.Fatalf("body = %s, want save message", rec.Body.String())
	}
}

func TestNewRouter_ListAIInstalledSkills(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})
	cookie := loginProtectedRoute(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/ai/skills", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":"demo-skill"`) {
		t.Fatalf("body = %s, want installed skill list", rec.Body.String())
	}
}

func TestNewRouter_GetAIInstalledSkillDetail(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})
	cookie := loginProtectedRoute(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/ai/skills/demo-skill", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"content":"# Demo Skill`) {
		t.Fatalf("body = %s, want skill detail content", rec.Body.String())
	}
}

func TestNewRouter_UploadAIInstalledSkillPackage(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})
	cookie := loginProtectedRoute(t, handler)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, err := writer.CreateFormFile("file", "demo-skill.zip")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := fileWriter.Write([]byte("fake zip")); err != nil {
		t.Fatalf("Write(file) error = %v", err)
	}
	if err := writer.WriteField("overwrite", "false"); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/ai/skills/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"installed_to":"data/skills/items/demo-skill"`) {
		t.Fatalf("body = %s, want skill install result", rec.Body.String())
	}
}

func TestNewRouter_EnableAIInstalledSkill(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})
	cookie := loginProtectedRoute(t, handler)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/ai/skills/demo-skill/enable", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("body = %s, want enabled true", rec.Body.String())
	}
}

func TestNewRouter_GetAIView(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})

	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/ai", nil)
	req.AddCookie(loginRec.Result().Cookies()[0])
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"candidate_memories"`) {
		t.Fatalf("body = %s, want AI debug view", rec.Body.String())
	}
}

func TestNewRouter_ListAIMessageLogs(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})
	cookie := loginProtectedRoute(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/ai/messages?chat_type=group&group_id=10001&keyword=%E6%B5%8B%E8%AF%95", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"items"`) || !strings.Contains(rec.Body.String(), `"message_id":"msg-1"`) {
		t.Fatalf("body = %s, want ai message list payload", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"chat_type":"group"`) {
		t.Fatalf("body = %s, want query chat_type echoed", rec.Body.String())
	}
}

func TestNewRouter_ListAIMessageSuggestions(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})
	cookie := loginProtectedRoute(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/ai/messages/suggestions?chat_type=group&group_id=100&user_id=200&limit=5", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "\"groups\":[\"10001\",\"10088\"]") || !strings.Contains(rec.Body.String(), "\"users\":[\"20002\",\"20099\"]") {
		t.Fatalf("body = %s, want ai message suggestions payload", rec.Body.String())
	}
}

func TestNewRouter_SyncAllAIRecentMessages(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})
	cookie := loginProtectedRoute(t, handler)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/ai/messages/sync-all", bytes.NewBufferString(`{"connection_id":"napcat-main","chat_type":"group","count":50}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"targets":2`) || !strings.Contains(rec.Body.String(), `"failed":1`) {
		t.Fatalf("body = %s, want ai message bulk sync payload", rec.Body.String())
	}
}

func TestNewRouter_DiscoverAIProviderModels(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})
	cookie := loginProtectedRoute(t, handler)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/ai/models", bytes.NewBufferString(`{"kind":"openai_compatible","vendor":"openai","base_url":"https://api.openai.com/v1","api_key":"sk-test"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"id":"gpt-test"`) || strings.Contains(rec.Body.String(), "sk-test") {
		t.Fatalf("body = %s, want model list without api key", rec.Body.String())
	}
}

func TestNewRouter_GetAIMessageDetail(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})
	cookie := loginProtectedRoute(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/ai/messages/msg-1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"vision_summary":"一张猫咪表情包"`) || !strings.Contains(rec.Body.String(), `"preview_url":"/api/admin/ai/messages/msg-1/images/0/content"`) {
		t.Fatalf("body = %s, want ai message detail payload", rec.Body.String())
	}
}

func TestNewRouter_GetAIMessageImagePreviewContent(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})
	cookie := loginProtectedRoute(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/ai/messages/msg-1/images/0/content", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307, body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "https://cdn.example.com/cat.png" {
		t.Fatalf("Location = %q, want preview redirect", got)
	}
}

func TestNewRouter_SaveWebUITheme(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})

	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/webui/theme", bytes.NewBufferString(`{"theme":"blue-light"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(loginRec.Result().Cookies()[0])
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"message":"WebUI 主题已保存并立即生效"`) {
		t.Fatalf("body = %s, want theme save message", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"restart_required":false`) {
		t.Fatalf("body = %s, want restart_required=false", rec.Body.String())
	}
}

func TestNewRouter_PromoteAICandidateMemory(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})

	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/ai/candidates/candidate-1/promote", nil)
	req.AddCookie(loginRec.Result().Cookies()[0])
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "候选记忆已晋升为长期记忆") {
		t.Fatalf("body = %s, want promote message", rec.Body.String())
	}
}

func TestNewRouter_RunAIReflection(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})

	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/ai/reflection/run", nil)
	req.AddCookie(loginRec.Result().Cookies()[0])
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "\"target\":\"reflection\"") || !strings.Contains(rec.Body.String(), "晋升 1 条候选记忆") {
		t.Fatalf("body = %s, want reflection action result", rec.Body.String())
	}
}

func TestNewRouter_AnalyzeAIRelations(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})

	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/ai/relations/analyze", bytes.NewBufferString(`{"group_id":"10001"}`))
	req.AddCookie(loginRec.Result().Cookies()[0])
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "\"task_id\":\"rel-1710000000000-1\"") || !strings.Contains(rec.Body.String(), "\"status\":\"running\"") {
		t.Fatalf("body = %s, want relation analysis task payload", rec.Body.String())
	}
}

func TestNewRouter_GetAIRelationAnalysisTask(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})

	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/ai/relations/analyze/rel-1710000000000-1", nil)
	req.AddCookie(loginRec.Result().Cookies()[0])
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "\"status\":\"succeeded\"") || !strings.Contains(rec.Body.String(), "\"markdown\":\"# 群友关系与性格分析\\n\"") {
		t.Fatalf("body = %s, want finished relation analysis task result", rec.Body.String())
	}
}

func TestNewRouter_AuthStateRequiresSetup(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), &setupMockProvider{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/auth/state", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"requires_setup":true`) {
		t.Fatalf("body = %s, want requires_setup=true", rec.Body.String())
	}
}

func TestNewRouter_AuthSetupAndProtectedRoute(t *testing.T) {
	provider := &setupMockProvider{}
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), provider)

	setupReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/setup", bytes.NewBufferString(`{"password":"secret123"}`))
	setupRec := httptest.NewRecorder()
	handler.ServeHTTP(setupRec, setupReq)

	if setupRec.Code != http.StatusOK {
		t.Fatalf("setup status = %d, want 200, body=%s", setupRec.Code, setupRec.Body.String())
	}
	cookies := setupRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("setup cookies = 0, want session cookie")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/webui/bootstrap", nil)
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"meta"`) {
		t.Fatalf("body = %s, want bootstrap payload", rec.Body.String())
	}
}

func TestNewRouter_ProtectedRouteRequiresLogin(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/webui/bootstrap", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body=%s", rec.Code, rec.Body.String())
	}
}

func TestNewRouter_LoginAndLogout(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("login cookies = 0, want session cookie")
	}

	okReq := httptest.NewRequest(http.MethodGet, "/api/admin/meta", nil)
	okReq.AddCookie(cookies[0])
	okRec := httptest.NewRecorder()
	handler.ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Fatalf("meta status = %d, want 200, body=%s", okRec.Code, okRec.Body.String())
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/logout", nil)
	logoutReq.AddCookie(cookies[0])
	logoutRec := httptest.NewRecorder()
	handler.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", logoutRec.Code)
	}

	failReq := httptest.NewRequest(http.MethodGet, "/api/admin/meta", nil)
	failReq.AddCookie(cookies[0])
	failRec := httptest.NewRecorder()
	handler.ServeHTTP(failRec, failReq)
	if failRec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 after logout, body=%s", failRec.Code, failRec.Body.String())
	}
}

func TestNewRouter_ChangePassword(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), protectedMockProvider{})

	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("login cookies = 0, want session cookie")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/password", bytes.NewBufferString(`{"current_password":"secret","new_password":"secret456"}`))
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"message":"后台密码已更新"`) {
		t.Fatalf("body = %s, want change password result", rec.Body.String())
	}
}

func TestNewRouter_PluginLifecycleActions(t *testing.T) {
	actions := []string{"install", "start", "reload", "recover", "stop", "uninstall"}
	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			provider := &actionTrackingMockProvider{}
			handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), provider)

			loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
			loginRec := httptest.NewRecorder()
			handler.ServeHTTP(loginRec, loginReq)
			if loginRec.Code != http.StatusOK {
				t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
			}
			cookies := loginRec.Result().Cookies()
			if len(cookies) == 0 {
				t.Fatalf("login cookies = 0, want session cookie")
			}

			req := httptest.NewRequest(http.MethodPost, "/api/admin/plugins/menu_hint/"+action, nil)
			req.AddCookie(cookies[0])
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
			}
			if provider.lastAction != action {
				t.Fatalf("lastAction = %q, want %q", provider.lastAction, action)
			}
			if !strings.Contains(rec.Body.String(), `"action":"`+action+`"`) {
				t.Fatalf("body = %s, want action=%s", rec.Body.String(), action)
			}
		})
	}
}

func TestNewRouter_SavePluginConfig(t *testing.T) {
	provider := &actionTrackingMockProvider{}
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), provider)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("login cookies = 0, want session cookie")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/plugins/menu_hint/config", bytes.NewBufferString(`{"enabled":true,"config":{"header_text":"菜单"}}`))
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if provider.lastAction != "config" {
		t.Fatalf("lastAction = %q, want config", provider.lastAction)
	}
	if !strings.Contains(rec.Body.String(), `"plugin_id":"menu_hint"`) {
		t.Fatalf("body = %s, want plugin save result", rec.Body.String())
	}
}

func TestNewRouter_DebugPluginAPI(t *testing.T) {
	provider := &actionTrackingMockProvider{}
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), provider)
	cookie := loginProtectedRoute(t, handler)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/plugins/ext_demo/debug-api", bytes.NewBufferString(`{
		"method":"bot.get_stranger_info",
		"payload":{"connection_id":"napcat-main","user_id":"123456"}
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if provider.lastAction != "debug-api" {
		t.Fatalf("lastAction = %q, want debug-api", provider.lastAction)
	}
	if provider.lastDebug.Method != "bot.get_stranger_info" {
		t.Fatalf("lastDebug.Method = %q, want bot.get_stranger_info", provider.lastDebug.Method)
	}
	if !strings.Contains(rec.Body.String(), `"plugin_id":"ext_demo"`) || !strings.Contains(rec.Body.String(), `"method":"bot.get_stranger_info"`) {
		t.Fatalf("body = %s, want plugin debug result", rec.Body.String())
	}
	if len(provider.auditEntries) == 0 || provider.auditEntries[0].Action != "debug-api" {
		t.Fatalf("auditEntries = %+v, want debug-api audit entry", provider.auditEntries)
	}
}

func TestNewRouter_DebugFrameworkPluginAPI(t *testing.T) {
	provider := &actionTrackingMockProvider{}
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), provider)
	cookie := loginProtectedRoute(t, handler)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/plugin-api/debug", bytes.NewBufferString(`{
		"method":"messenger.send_text",
		"payload":{
			"target":{"connection_id":"napcat-main","chat_type":"private","user_id":"123456"},
			"text":"hello"
		}
	}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if provider.lastAction != "framework-debug-api" {
		t.Fatalf("lastAction = %q, want framework-debug-api", provider.lastAction)
	}
	if provider.lastDebug.Method != "messenger.send_text" {
		t.Fatalf("lastDebug.Method = %q, want messenger.send_text", provider.lastDebug.Method)
	}
	if strings.Contains(rec.Body.String(), `"plugin_id":`) || !strings.Contains(rec.Body.String(), `"method":"messenger.send_text"`) {
		t.Fatalf("body = %s, want framework debug result without plugin id", rec.Body.String())
	}
	if len(provider.auditEntries) == 0 || provider.auditEntries[0].Action != "framework-debug-api" {
		t.Fatalf("auditEntries = %+v, want framework-debug-api audit entry", provider.auditEntries)
	}
}

func TestNewRouter_PluginUploadDefaultsToOverwrite(t *testing.T) {
	provider := &uploadTrackingMockProvider{}
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), provider)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("login cookies = 0, want session cookie")
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "uploaded_demo.zip")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write([]byte("zip-payload")); err != nil {
		t.Fatalf("part.Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/plugins/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if provider.fileName != "uploaded_demo.zip" {
		t.Fatalf("fileName = %q, want uploaded_demo.zip", provider.fileName)
	}
	if string(provider.payload) != "zip-payload" {
		t.Fatalf("payload = %q, want zip-payload", string(provider.payload))
	}
	if !provider.overwrite {
		t.Fatalf("overwrite = false, want true")
	}
	if !strings.Contains(rec.Body.String(), `"plugin_id":"uploaded_demo"`) {
		t.Fatalf("body = %s, want plugin upload result", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"replaced":true`) {
		t.Fatalf("body = %s, want replaced=true", rec.Body.String())
	}
}

func TestNewRouter_PluginDetail(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/plugins/menu_hint", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "menu_hint") {
		t.Fatalf("body = %s, want plugin detail", rec.Body.String())
	}
}

func TestNewRouter_AuditLogs(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit?limit=1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"category":"plugin"`) || !strings.Contains(rec.Body.String(), `"action":"recover"`) {
		t.Fatalf("body = %s, want audit entry", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"limit":1`) {
		t.Fatalf("body = %s, want limit=1", rec.Body.String())
	}
}

func TestNewRouter_AuditLogsFilter(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit?category=auth&result=failed&q=%E5%AF%86%E7%A0%81", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"category":"auth"`) || !strings.Contains(body, `"result":"failed"`) {
		t.Fatalf("body = %s, want filtered auth failed item", body)
	}
	if strings.Contains(body, `"category":"plugin"`) {
		t.Fatalf("body = %s, want plugin item filtered out", body)
	}
}

func TestNewRouter_AuditSystemLogs(t *testing.T) {
	logDir := t.TempDir()
	logPath := filepath.Join(logDir, "app.log")
	if err := os.WriteFile(logPath, []byte("first\nsecond\nthird\n"), 0o600); err != nil {
		t.Fatalf("write log file: %v", err)
	}

	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), logViewMockProvider{logDir: logDir})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit/logs?limit=2", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"second"`) || !strings.Contains(body, `"third"`) {
		t.Fatalf("body = %s, want tail log lines", body)
	}
	if strings.Contains(body, `"first"`) {
		t.Fatalf("body = %s, want old line trimmed", body)
	}
}

func TestNewRouter_PluginActionRecordsAudit(t *testing.T) {
	provider := &actionTrackingMockProvider{}
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), provider)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("login cookies = 0, want session cookie")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/plugins/menu_hint/recover", nil)
	req.AddCookie(cookies[0])
	req.RemoteAddr = "127.0.0.1:9000"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if len(provider.auditEntries) == 0 {
		t.Fatalf("auditEntries = 0, want >= 1")
	}
	var found bool
	for _, item := range provider.auditEntries {
		if item.Category == "plugin" && item.Action == "recover" {
			found = true
			if item.Target != "menu_hint" {
				t.Fatalf("audit target = %q, want menu_hint", item.Target)
			}
			if item.Result != "success" {
				t.Fatalf("audit result = %q, want success", item.Result)
			}
			if item.RemoteAddr != "127.0.0.1" {
				t.Fatalf("audit remote_addr = %q, want 127.0.0.1", item.RemoteAddr)
			}
			break
		}
	}
	if !found {
		t.Fatalf("auditEntries = %+v, want plugin recover entry", provider.auditEntries)
	}
}

func TestNewRouter_ConnectionDetailNotFound(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/connections/not-found", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestNewRouter_ConnectionProbe(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/connections/napcat-main/probe", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"online":true`) {
		t.Fatalf("body = %s, want online=true", rec.Body.String())
	}
}

func TestNewRouter_StartConnection(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/connections/napcat-main/start", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"connection_id":"napcat-main"`) {
		t.Fatalf("body = %s, want started connection id", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("body = %s, want enabled=true", rec.Body.String())
	}
}

func TestNewRouter_StopConnection(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/connections/napcat-main/stop", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"connection_id":"napcat-main"`) {
		t.Fatalf("body = %s, want stopped connection id", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("body = %s, want enabled=false", rec.Body.String())
	}
}

func TestNewRouter_SaveConnectionConfig(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	body := bytes.NewBufferString(`{
		"id":"napcat-secondary",
		"enabled":true,
		"platform":"onebot_v11",
		"ingress":{"type":"http_callback","listen":":8081","path":"/callback"},
		"action":{"type":"napcat_http","base_url":"http://127.0.0.1:3001","timeout_ms":10000}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/connections", body)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"connection_id":"napcat-secondary"`) {
		t.Fatalf("body = %s, want saved connection id", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"detail"`) {
		t.Fatalf("body = %s, want connection detail payload", rec.Body.String())
	}
}

func TestNewRouter_DeleteConnection(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/connections/napcat-main/delete", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"connection_id":"napcat-main"`) {
		t.Fatalf("body = %s, want deleted connection id", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"persisted":true`) {
		t.Fatalf("body = %s, want persisted=true", rec.Body.String())
	}
}

func TestNewRouter_ConfigValidate(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	body := bytes.NewBufferString(`{
		"app":{"name":"go-bot","env":"dev","data_dir":"./data","log_level":"info"},
		"server":{"admin":{"enabled":true,"listen":":8090"},"webui":{"enabled":true,"base_path":"/"}},
		"storage":{"sqlite":{"path":"./data/app.db"},"logs":{"dir":"./data/logs","max_size_mb":50,"max_backups":7,"max_age_days":30}},
		"connections":[{"id":"napcat-main","enabled":true,"platform":"onebot_v11","ingress":{"type":"ws_server","listen":":8080"},"action":{"type":"napcat_http","base_url":"http://127.0.0.1:3000","timeout_ms":10000}}],
		"plugins":[{"id":"menu_hint","kind":"builtin","enabled":true,"config":{"header_text":"菜单"}}],
		"security":{"admin_auth":{"enabled":false,"password":"test"}}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/config/validate", body)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"valid":true`) {
		t.Fatalf("body = %s, want valid=true", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `******`) || strings.Contains(rec.Body.String(), `"test"`) {
		t.Fatalf("body = %s, want redacted password", rec.Body.String())
	}
}

func TestNewRouter_ConfigValidateInvalid(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	body := bytes.NewBufferString(`{"app":{"name":"","data_dir":"","log_level":"bad"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/config/validate", body)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"valid":false`) {
		t.Fatalf("body = %s, want valid=false", rec.Body.String())
	}
}

func TestNewRouter_ConfigSave(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	body := bytes.NewBufferString(`{
		"app":{"name":"go-bot","env":"dev","data_dir":"./data","log_level":"info"},
		"server":{"admin":{"enabled":true,"listen":":8090"},"webui":{"enabled":true,"base_path":"/"}},
		"storage":{"sqlite":{"path":"./data/app.db"},"logs":{"dir":"./data/logs","max_size_mb":50,"max_backups":7,"max_age_days":30}},
		"connections":[{"id":"napcat-main","enabled":true,"platform":"onebot_v11","ingress":{"type":"ws_server","listen":":8080"},"action":{"type":"napcat_http","base_url":"http://127.0.0.1:3000","timeout_ms":10000}}],
		"plugins":[{"id":"menu_hint","kind":"builtin","enabled":true,"config":{"header_text":"菜单"}}],
		"security":{"admin_auth":{"enabled":false,"password":"test"}}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/config/save", body)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"persisted":true`) {
		t.Fatalf("body = %s, want persisted=true", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"path":"configs/config.yml"`) {
		t.Fatalf("body = %s, want saved path", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"backup_path":"configs/.bak/config-20260418-120000.000.yml"`) {
		t.Fatalf("body = %s, want backup path", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"hot_apply_attempted":true`) || !strings.Contains(rec.Body.String(), `"hot_apply_error":"启动插件 menu_hint 失败: boom"`) {
		t.Fatalf("body = %s, want hot apply diagnostics", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `******`) || strings.Contains(rec.Body.String(), `"test"`) {
		t.Fatalf("body = %s, want redacted password in normalized config", rec.Body.String())
	}
}

func TestNewRouter_ConfigSaveUsesRequestContext(t *testing.T) {
	provider := &configContextProvider{}
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), provider)
	body := bytes.NewBufferString(`{
		"app":{"name":"go-bot","env":"dev","data_dir":"./data","log_level":"info"},
		"server":{"admin":{"enabled":true,"listen":":8090"},"webui":{"enabled":true,"base_path":"/"}},
		"storage":{"sqlite":{"path":"./data/app.db"},"logs":{"dir":"./data/logs","max_size_mb":50,"max_backups":7,"max_age_days":30}},
		"connections":[{"id":"napcat-main","enabled":true,"platform":"onebot_v11","ingress":{"type":"ws_server","listen":":8080"},"action":{"type":"napcat_http","base_url":"http://127.0.0.1:3000","timeout_ms":10000}}],
		"plugins":[{"id":"menu_hint","kind":"builtin","enabled":true,"config":{"header_text":"菜单"}}],
		"security":{"admin_auth":{"enabled":false,"password":"test"}}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/config/save", body)
	req = req.WithContext(context.WithValue(req.Context(), requestContextKey("trace"), "config-save-trace"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if provider.trace != "config-save-trace" {
		t.Fatalf("trace = %q, want propagated request context", provider.trace)
	}
}

func TestNewRouter_ConfigRestart(t *testing.T) {
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), mockProvider{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/config/restart", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"restarted":true`) {
		t.Fatalf("body = %s, want restarted=true", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"state":"running"`) {
		t.Fatalf("body = %s, want running state", rec.Body.String())
	}
}

func TestNewRouter_AuthSetupUsesRequestContextAndSecureCookie(t *testing.T) {
	provider := &authSetupContextProvider{}
	handler := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), provider)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/setup", bytes.NewBufferString(`{"password":"secret"}`))
	req = req.WithContext(context.WithValue(req.Context(), requestContextKey("trace"), "auth-setup-trace"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if provider.trace != "auth-setup-trace" {
		t.Fatalf("trace = %q, want propagated request context", provider.trace)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("cookies = 0, want session cookie")
	}
	if !cookies[0].Secure {
		t.Fatalf("cookie secure = %v, want true when forwarded proto is https", cookies[0].Secure)
	}
}
