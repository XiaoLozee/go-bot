package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/XiaoLozee/go-bot/internal/ai"
	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/runtime"
)

func registerAIRoutes(mux *http.ServeMux, logger *slog.Logger, provider aiRouteProvider) {
	mux.HandleFunc("/api/admin/ai", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, provider.AIView())
	})

	mux.HandleFunc("/api/admin/ai/skills", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		items, err := provider.ListAIInstalledSkills(r.Context())
		if err != nil {
			logger.Error("查询 AI 技能中心列表失败", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":  "ai_skill_list_failed",
				"detail": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, items)
	})

	mux.HandleFunc("/api/admin/ai/skills/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxPluginUploadSize)
		if err := r.ParseMultipartForm(maxPluginUploadSize); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":  "invalid_multipart",
				"detail": err.Error(),
			})
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":  "missing_file",
				"detail": err.Error(),
			})
			return
		}
		defer func() { _ = file.Close() }()
		payload, err := io.ReadAll(file)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":  "read_upload_failed",
				"detail": err.Error(),
			})
			return
		}

		overwrite, _ := strconv.ParseBool(strings.TrimSpace(r.FormValue("overwrite")))
		result, err := provider.InstallAIInstalledSkillPackage(r.Context(), header.Filename, payload, overwrite)
		if err != nil {
			logger.Error("上传 AI 技能包失败", "file", header.Filename, "error", err)
			recordAdminAudit(provider, r, "ai_skill", "upload", header.Filename, "failed", "上传技能包失败", err.Error(), "")
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":  "ai_skill_upload_failed",
				"detail": err.Error(),
				"file":   header.Filename,
			})
			return
		}
		recordAdminAudit(provider, r, "ai_skill", "upload", firstNonEmpty(result.Skill.ID, header.Filename), "success", firstNonEmpty(result.Message, "技能包已安装"), "", "")
		status := http.StatusCreated
		if result.Replaced {
			status = http.StatusOK
		}
		writeJSON(w, status, result)
	})

	mux.HandleFunc("/api/admin/ai/skills/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		defer func() { _ = r.Body.Close() }()
		var payload struct {
			SourceURL string `json:"source_url"`
			Overwrite bool   `json:"overwrite"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":  "invalid_json",
				"detail": err.Error(),
			})
			return
		}
		result, err := provider.InstallAIInstalledSkillFromURL(r.Context(), payload.SourceURL, payload.Overwrite)
		if err != nil {
			logger.Error("导入 AI 技能失败", "source_url", payload.SourceURL, "error", err)
			recordAdminAudit(provider, r, "ai_skill", "import", payload.SourceURL, "failed", "导入技能失败", err.Error(), "")
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":  "ai_skill_import_failed",
				"detail": err.Error(),
			})
			return
		}
		recordAdminAudit(provider, r, "ai_skill", "import", firstNonEmpty(result.Skill.ID, payload.SourceURL), "success", firstNonEmpty(result.Message, "技能已导入"), "", "")
		status := http.StatusCreated
		if result.Replaced {
			status = http.StatusOK
		}
		writeJSON(w, status, result)
	})

	mux.HandleFunc("/api/admin/ai/skills/", func(w http.ResponseWriter, r *http.Request) {
		parts := splitRouteParts(r.URL.Path, "/api/admin/ai/skills/")
		if len(parts) == 1 && r.Method == http.MethodGet {
			detail, err := provider.GetAIInstalledSkill(r.Context(), parts[0])
			if err != nil {
				logger.Error("查询 AI 技能详情失败", "skill_id", parts[0], "error", err)
				writeJSON(w, http.StatusNotFound, map[string]any{
					"error":  "ai_skill_not_found",
					"detail": err.Error(),
				})
				return
			}
			writeJSON(w, http.StatusOK, detail)
			return
		}
		if len(parts) != 2 || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		id, action := parts[0], parts[1]
		var (
			result runtime.AISkillActionResult
			err    error
		)
		switch action {
		case "enable":
			result, err = provider.SetAIInstalledSkillEnabled(r.Context(), id, true)
		case "disable":
			result, err = provider.SetAIInstalledSkillEnabled(r.Context(), id, false)
		case "uninstall":
			result, err = provider.UninstallAIInstalledSkill(r.Context(), id)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			logger.Error("AI 技能操作失败", "skill_id", id, "action", action, "error", err)
			recordAdminAudit(provider, r, "ai_skill", action, id, "failed", "AI 技能操作失败", err.Error(), "")
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "ai_skill_action_failed",
				"detail":   err.Error(),
			})
			return
		}
		recordAdminAudit(provider, r, "ai_skill", action, id, "success", firstNonEmpty(result.Message, "AI 技能操作成功"), "", "")
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/ai/messages", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		query := ai.MessageLogQuery{
			ChatType: r.URL.Query().Get("chat_type"),
			GroupID:  r.URL.Query().Get("group_id"),
			UserID:   r.URL.Query().Get("user_id"),
			Keyword:  r.URL.Query().Get("keyword"),
			Limit:    parseMessageLimit(r.URL.Query().Get("limit")),
		}
		result, err := provider.ListAIMessageLogs(r.Context(), query)
		if err != nil {
			logger.Error("查询 AI 聊天消息失败", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":  "ai_messages_failed",
				"detail": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/ai/messages/suggestions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		query := ai.MessageSuggestionQuery{
			ChatType: r.URL.Query().Get("chat_type"),
			GroupID:  r.URL.Query().Get("group_id"),
			UserID:   r.URL.Query().Get("user_id"),
			Limit:    parseSuggestionLimit(r.URL.Query().Get("limit")),
		}
		result, err := provider.ListAIMessageSuggestions(r.Context(), query)
		if err != nil {
			logger.Error("查询 AI 聊天记录联想失败", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":  "ai_message_suggestions_failed",
				"detail": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/ai/messages/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		defer func() { _ = r.Body.Close() }()

		var req runtime.AIMessageSendRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "invalid_json",
				"detail":   err.Error(),
			})
			return
		}

		result, err := provider.SendAIMessage(r.Context(), req)
		if err != nil {
			logger.Error("后台发送 AI 会话消息失败", "connection_id", req.ConnectionID, "chat_type", req.ChatType, "group_id", req.GroupID, "user_id", req.UserID, "error", err)
			recordAdminAudit(provider, r, "ai_message", "send", req.ConnectionID, "failed", "后台发送消息失败", err.Error(), "")
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "ai_message_send_failed",
				"detail":   err.Error(),
			})
			return
		}
		recordAdminAudit(provider, r, "ai_message", "send", result.ConnectionID, "success", "后台消息已发送", "", "")
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/ai/messages/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		defer func() { _ = r.Body.Close() }()

		var req runtime.AIRecentMessagesSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "invalid_json",
				"detail":   err.Error(),
			})
			return
		}

		result, err := provider.SyncAIRecentMessages(r.Context(), req)
		if err != nil {
			logger.Error("同步 AI 最近消息失败", "connection_id", req.ConnectionID, "chat_type", req.ChatType, "group_id", req.GroupID, "user_id", req.UserID, "error", err)
			recordAdminAudit(provider, r, "ai_message", "sync", req.ConnectionID, "failed", "同步最近消息失败", err.Error(), "")
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "ai_message_sync_failed",
				"detail":   err.Error(),
			})
			return
		}
		recordAdminAudit(provider, r, "ai_message", "sync", result.ConnectionID, "success", firstNonEmpty(result.Message, "最近消息已同步"), "", "")
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/ai/messages/sync-all", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		defer func() { _ = r.Body.Close() }()

		var req runtime.AIRecentMessagesBulkSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "invalid_json",
				"detail":   err.Error(),
			})
			return
		}

		result, err := provider.SyncAllAIRecentMessages(r.Context(), req)
		if err != nil {
			logger.Error("批量同步 AI 最近消息失败", "connection_id", req.ConnectionID, "chat_type", req.ChatType, "error", err)
			recordAdminAudit(provider, r, "ai_message", "sync_all", req.ConnectionID, "failed", "批量同步最近消息失败", err.Error(), "")
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "ai_message_sync_all_failed",
				"detail":   err.Error(),
			})
			return
		}
		recordAdminAudit(provider, r, "ai_message", "sync_all", result.ConnectionID, "success", firstNonEmpty(result.Message, "最近消息已批量同步"), "", "")
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/ai/forward-messages/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		parts := splitRouteParts(r.URL.Path, "/api/admin/ai/forward-messages/")
		if len(parts) == 0 || len(parts) > 2 {
			http.NotFound(w, r)
			return
		}

		connectionID := strings.TrimSpace(r.URL.Query().Get("connection_id"))
		forwardIDPart := parts[0]
		if len(parts) == 2 {
			var err error
			connectionID, err = url.PathUnescape(parts[0])
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error":  "invalid_connection_id",
					"detail": "连接 ID 不合法",
				})
				return
			}
			forwardIDPart = parts[1]
		}

		forwardID, err := url.PathUnescape(forwardIDPart)
		if err != nil || strings.TrimSpace(forwardID) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":  "invalid_forward_id",
				"detail": "合并转发 ID 不合法",
			})
			return
		}

		result, err := provider.GetAIForwardMessage(r.Context(), connectionID, forwardID)
		if err != nil {
			logger.Error("查询合并转发消息失败", "connection_id", connectionID, "forward_id", forwardID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":  "ai_forward_message_failed",
				"detail": err.Error(),
			})
			return
		}
		w.Header().Set("Cache-Control", "private, max-age=300")
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/ai/messages/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		parts := splitRouteParts(r.URL.Path, "/api/admin/ai/messages/")
		if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
			http.NotFound(w, r)
			return
		}

		messageID, err := url.PathUnescape(parts[0])
		if err != nil || strings.TrimSpace(messageID) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":  "invalid_message_id",
				"detail": "消息 ID 不合法",
			})
			return
		}

		if len(parts) == 1 {
			result, err := provider.GetAIMessageDetail(r.Context(), messageID)
			if err != nil {
				logger.Error("查询 AI 聊天消息详情失败", "message_id", messageID, "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error":  "ai_message_detail_failed",
					"detail": err.Error(),
				})
				return
			}
			writeJSON(w, http.StatusOK, result)
			return
		}

		if len(parts) == 4 && parts[1] == "images" && parts[3] == "content" {
			index, err := strconv.Atoi(strings.TrimSpace(parts[2]))
			if err != nil || index < 0 {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error":  "invalid_image_index",
					"detail": "图片序号不合法",
				})
				return
			}

			preview, err := provider.ResolveAIMessageImagePreview(r.Context(), messageID, index)
			if err != nil {
				logger.Error("解析 AI 图片预览失败", "message_id", messageID, "segment_index", index, "error", err)
				writeJSON(w, http.StatusNotFound, map[string]any{
					"error":  "ai_message_image_preview_failed",
					"detail": err.Error(),
				})
				return
			}
			if strings.TrimSpace(preview.RedirectURL) != "" {
				http.Redirect(w, r, preview.RedirectURL, http.StatusTemporaryRedirect)
				return
			}
			if strings.TrimSpace(preview.LocalPath) == "" {
				writeJSON(w, http.StatusNotFound, map[string]any{
					"error":  "ai_message_image_not_ready",
					"detail": "图片资源尚未可预览",
				})
				return
			}
			if strings.TrimSpace(preview.MimeType) != "" {
				w.Header().Set("Content-Type", preview.MimeType)
			}
			w.Header().Set("Cache-Control", "private, max-age=300")
			http.ServeFile(w, r, preview.LocalPath)
			return
		}

		http.NotFound(w, r)
	})

	mux.HandleFunc("/api/admin/ai/reflection/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		result, err := provider.RunAIReflection(r.Context())
		if err != nil {
			logger.Error("执行 AI 后台整理失败", "error", err)
			recordAdminAudit(provider, r, "ai", "reflect", "core", "failed", "执行 AI 后台整理失败", err.Error(), "")
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"accepted": false,
				"error":    "ai_reflection_failed",
				"detail":   err.Error(),
			})
			return
		}
		recordAdminAudit(provider, r, "ai", "reflect", "core", "success", firstNonEmpty(result.Message, "AI 后台整理已执行"), "", "")
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/ai/relations/analyze", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		defer func() { _ = r.Body.Close() }()

		var req runtime.AIRelationAnalysisRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "invalid_json",
				"detail":   err.Error(),
			})
			return
		}

		result, err := provider.StartAIRelationAnalysis(r.Context(), req)
		if err != nil {
			logger.Error("提交 AI 关系分析任务失败", "group_id", req.GroupID, "error", err)
			recordAdminAudit(provider, r, "ai", "analyze_relations", req.GroupID, "failed", "提交 AI 关系分析任务失败", err.Error(), "")
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"accepted": false,
				"error":    "ai_relation_analysis_task_failed",
				"detail":   err.Error(),
			})
			return
		}
		recordAdminAudit(provider, r, "ai", "analyze_relations", req.GroupID, "success", firstNonEmpty(result.Message, "AI 关系分析任务已提交"), "", "")
		writeJSON(w, http.StatusAccepted, result)
	})

	mux.HandleFunc("/api/admin/ai/relations/analyze/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		parts := splitRouteParts(r.URL.Path, "/api/admin/ai/relations/analyze/")
		if len(parts) != 1 || strings.TrimSpace(parts[0]) == "" {
			http.NotFound(w, r)
			return
		}
		taskID, err := url.PathUnescape(parts[0])
		if err != nil || strings.TrimSpace(taskID) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "invalid_task_id",
				"detail":   "任务 ID 不合法",
			})
			return
		}
		result, err := provider.GetAIRelationAnalysisTask(r.Context(), taskID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{
				"accepted": false,
				"error":    "ai_relation_analysis_task_not_found",
				"detail":   err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/ai/candidates/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		parts := splitRouteParts(r.URL.Path, "/api/admin/ai/candidates/")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			http.NotFound(w, r)
			return
		}
		id, action := parts[0], parts[1]

		var (
			result runtime.AIMemoryActionResult
			err    error
		)
		switch action {
		case "promote":
			result, err = provider.PromoteAICandidateMemory(r.Context(), id)
		case "delete":
			result, err = provider.DeleteAICandidateMemory(r.Context(), id)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			logger.Error("AI 候选记忆操作失败", "id", id, "action", action, "error", err)
			recordAdminAudit(provider, r, "ai_memory", action, id, "failed", "AI 候选记忆操作失败", err.Error(), "")
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"accepted": false,
				"error":    "ai_candidate_action_failed",
				"detail":   err.Error(),
				"id":       id,
				"action":   action,
			})
			return
		}
		recordAdminAudit(provider, r, "ai_memory", action, id, "success", firstNonEmpty(result.Message, "AI 候选记忆操作成功"), "", "")
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/ai/long-term/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		parts := splitRouteParts(r.URL.Path, "/api/admin/ai/long-term/")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			http.NotFound(w, r)
			return
		}
		id, action := parts[0], parts[1]
		if action != "delete" {
			http.NotFound(w, r)
			return
		}

		result, err := provider.DeleteAILongTermMemory(r.Context(), id)
		if err != nil {
			logger.Error("AI 长期记忆删除失败", "id", id, "error", err)
			recordAdminAudit(provider, r, "ai_memory", action, id, "failed", "AI 长期记忆操作失败", err.Error(), "")
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"accepted": false,
				"error":    "ai_long_term_action_failed",
				"detail":   err.Error(),
				"id":       id,
				"action":   action,
			})
			return
		}
		recordAdminAudit(provider, r, "ai_memory", action, id, "success", firstNonEmpty(result.Message, "AI 长期记忆操作成功"), "", "")
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/ai/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		defer func() { _ = r.Body.Close() }()

		var req config.AIProviderConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "invalid_json",
				"detail":   err.Error(),
			})
			return
		}

		result, err := provider.DiscoverAIProviderModels(r.Context(), req)
		if err != nil {
			logger.Error("获取 AI 模型列表失败", "vendor", req.Vendor, "kind", req.Kind, "base_url", req.BaseURL, "error", err)
			recordAdminAudit(provider, r, "ai", "models", req.Vendor, "failed", "获取 AI 模型列表失败", err.Error(), "")
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "ai_models_failed",
				"detail":   err.Error(),
			})
			return
		}
		recordAdminAudit(provider, r, "ai", "models", req.Vendor, "success", firstNonEmpty(result.Message, "AI 模型列表已获取"), "", "")
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/ai/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}

		defer func() { _ = r.Body.Close() }()

		var aiCfg config.AIConfig
		if err := json.NewDecoder(r.Body).Decode(&aiCfg); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "invalid_json",
				"detail":   err.Error(),
			})
			return
		}

		result, err := provider.SaveAIConfig(r.Context(), aiCfg)
		if err != nil {
			logger.Error("保存 AI 配置失败", "error", err)
			recordAdminAudit(provider, r, "ai", "save", "core", "failed", "保存 AI 配置失败", err.Error(), "")
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"accepted": false,
				"error":    "save_ai_failed",
				"detail":   err.Error(),
			})
			return
		}

		recordAdminAudit(provider, r, "ai", "save", "core", "success", firstNonEmpty(result.Message, "AI 配置已保存"), "", "")
		writeJSON(w, http.StatusOK, result)
	})
}
