package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/XiaoLozee/go-bot/internal/plugin/externalexec"
	"github.com/XiaoLozee/go-bot/internal/runtime"
)

func registerPluginRoutes(mux *http.ServeMux, logger *slog.Logger, provider pluginRouteProvider) {
	mux.HandleFunc("/api/admin/plugins", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, provider.PluginSnapshots())
			return
		}
		methodNotAllowed(w, http.MethodGet)
	})

	mux.HandleFunc("/api/admin/plugins/upload", func(w http.ResponseWriter, r *http.Request) {
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

		overwrite := true
		if raw := strings.TrimSpace(r.FormValue("overwrite")); raw != "" {
			if parsed, err := strconv.ParseBool(raw); err == nil {
				overwrite = parsed
			}
		}
		result, err := provider.InstallPluginPackage(r.Context(), header.Filename, payload, overwrite)
		if err != nil {
			logger.Error("上传插件包失败", "file", header.Filename, "error", err)
			recordAdminAudit(provider, r, "plugin", "upload", header.Filename, "failed", "上传插件包失败", err.Error(), "")
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":  "plugin_upload_failed",
				"detail": err.Error(),
				"file":   header.Filename,
			})
			return
		}
		status := http.StatusCreated
		if result.Replaced {
			status = http.StatusOK
		}
		recordAdminAudit(provider, r, "plugin", "upload", firstNonEmpty(result.PluginID, header.Filename), "success", firstNonEmpty(result.Message, "插件包已安装"), "", "")
		writeJSON(w, status, result)
	})

	mux.HandleFunc("/api/admin/plugin-api/debug", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}

		ctx := r.Context()
		defer func() { _ = r.Body.Close() }()

		var payload runtime.PluginAPIDebugRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			recordAdminAudit(provider, r, "plugin", "framework-debug-api", "framework", "failed", "框架插件接口调试请求 JSON 无效", err.Error(), "")
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":  "invalid_json",
				"detail": err.Error(),
			})
			return
		}

		result, err := provider.DebugFrameworkPluginAPI(ctx, payload)
		if err != nil {
			logger.Warn("框架插件接口调试失败", "method", payload.Method, "error", err)
			recordAdminAudit(provider, r, "plugin", "framework-debug-api", firstNonEmpty(payload.Method, "framework"), "failed", "框架插件接口调试失败", err.Error(), "")
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":  "plugin_debug_failed",
				"detail": err.Error(),
			})
			return
		}

		auditResult := "success"
		auditSummary := firstNonEmpty(result.Message, "框架插件接口调试完成")
		auditDetail := ""
		if strings.TrimSpace(result.Error) != "" {
			auditResult = "failed"
			auditSummary = "框架插件接口调试返回错误"
			auditDetail = result.Error
		}
		recordAdminAudit(provider, r, "plugin", "framework-debug-api", firstNonEmpty(result.Method, payload.Method, "framework"), auditResult, auditSummary, auditDetail, "")
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/plugins/", func(w http.ResponseWriter, r *http.Request) {
		parts := splitRouteParts(r.URL.Path, "/api/admin/plugins/")
		if len(parts) == 1 && r.Method == http.MethodGet {
			pluginID := parts[0]
			detail, ok := provider.PluginDetail(pluginID)
			if !ok {
				writeJSON(w, http.StatusNotFound, map[string]any{
					"error":  "plugin not found",
					"plugin": pluginID,
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

		pluginID, action := parts[0], parts[1]
		ctx := r.Context()

		switch action {
		case "config":
			defer func() { _ = r.Body.Close() }()

			var payload struct {
				Enabled bool           `json:"enabled"`
				Config  map[string]any `json:"config"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error":  "invalid_json",
					"plugin": pluginID,
					"action": action,
					"detail": err.Error(),
				})
				return
			}

			result, err := provider.SavePluginConfig(ctx, pluginID, payload.Enabled, payload.Config)
			if err != nil {
				logger.Error("保存插件配置失败", "plugin", pluginID, "error", err)
				recordAdminAudit(provider, r, "plugin", "config", pluginID, "failed", "保存插件配置失败", err.Error(), "")
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error":  "save_plugin_config_failed",
					"plugin": pluginID,
					"action": action,
					"detail": err.Error(),
				})
				return
			}
			recordAdminAudit(provider, r, "plugin", "config", pluginID, "success", firstNonEmpty(result.Message, "插件配置已保存"), "", "")
			writeJSON(w, http.StatusOK, result)
			return
		case "debug-api":
			detail, ok := provider.PluginDetail(pluginID)
			if !ok {
				writeJSON(w, http.StatusNotFound, map[string]any{
					"error":  "plugin not found",
					"plugin": pluginID,
					"action": action,
				})
				return
			}
			if strings.TrimSpace(detail.Snapshot.Kind) != externalexec.KindExternalExec {
				recordAdminAudit(provider, r, "plugin", "debug-api", pluginID, "failed", "插件 API 调试仅支持 external_exec", "", "")
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error":  "plugin_debug_not_supported",
					"plugin": pluginID,
					"action": action,
					"detail": "当前仅 external_exec 插件支持 API 调试",
				})
				return
			}

			defer func() { _ = r.Body.Close() }()

			var payload runtime.PluginAPIDebugRequest
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				recordAdminAudit(provider, r, "plugin", "debug-api", pluginID, "failed", "插件 API 调试请求 JSON 无效", err.Error(), "")
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error":  "invalid_json",
					"plugin": pluginID,
					"action": action,
					"detail": err.Error(),
				})
				return
			}

			result, err := provider.DebugPluginAPI(ctx, pluginID, payload)
			if err != nil {
				logger.Warn("插件 API 调试失败", "plugin", pluginID, "method", payload.Method, "error", err)
				recordAdminAudit(provider, r, "plugin", "debug-api", pluginID, "failed", "插件 API 调试失败", err.Error(), "")
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error":  "plugin_debug_failed",
					"plugin": pluginID,
					"action": action,
					"detail": err.Error(),
				})
				return
			}

			auditResult := "success"
			auditSummary := firstNonEmpty(result.Message, "插件 API 调试完成")
			auditDetail := ""
			if strings.TrimSpace(result.Error) != "" {
				auditResult = "failed"
				auditSummary = "插件 API 调试返回错误"
				auditDetail = result.Error
			}
			recordAdminAudit(provider, r, "plugin", "debug-api", pluginID, auditResult, auditSummary, auditDetail, "")
			writeJSON(w, http.StatusOK, result)
			return
		case "install", "start", "stop", "reload", "recover", "uninstall":
			var err error
			switch action {
			case "install":
				err = provider.InstallPlugin(ctx, pluginID)
			case "start":
				err = provider.StartPlugin(ctx, pluginID)
			case "stop":
				err = provider.StopPlugin(ctx, pluginID)
			case "reload":
				err = provider.ReloadPlugin(ctx, pluginID)
			case "recover":
				err = provider.RecoverPlugin(ctx, pluginID)
			case "uninstall":
				err = provider.UninstallPlugin(ctx, pluginID)
			}
			if err != nil {
				logger.Error("插件操作失败", "plugin", pluginID, "action", action, "error", err)
				recordAdminAudit(provider, r, "plugin", action, pluginID, "failed", "插件操作失败", err.Error(), "")
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error":  err.Error(),
					"plugin": pluginID,
					"action": action,
				})
				return
			}

			recordAdminAudit(provider, r, "plugin", action, pluginID, "success", "插件操作成功", "", "")
			writeJSON(w, http.StatusOK, map[string]any{
				"plugin": pluginID,
				"action": action,
				"status": "ok",
			})
			return
		default:
			http.NotFound(w, r)
			return
		}
	})
}
