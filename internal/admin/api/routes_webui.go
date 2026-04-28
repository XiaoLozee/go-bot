package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/XiaoLozee/go-bot/internal/admin/webui"
	"github.com/XiaoLozee/go-bot/internal/config"
	"github.com/XiaoLozee/go-bot/internal/runtime"
)

func registerWebUIRoutes(mux *http.ServeMux, logger *slog.Logger, provider webUIRouteProvider) {
	mux.HandleFunc("/api/admin/webui/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, provider.WebUIBootstrap())
	})

	mux.HandleFunc("/api/admin/webui/theme", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}

		defer func() { _ = r.Body.Close() }()

		var payload struct {
			Theme string `json:"theme"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "invalid_json",
				"detail":   err.Error(),
			})
			return
		}

		theme := config.NormalizeWebUITheme(payload.Theme)
		if !config.IsSupportedWebUITheme(theme) {
			detail := "仅支持以下主题：" + config.SupportedWebUIThemeList("、")
			recordAdminAudit(provider, r, "webui", "theme", strings.TrimSpace(payload.Theme), "failed", "切换 WebUI 主题失败", detail, "")
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "invalid_theme",
				"detail":   detail,
			})
			return
		}

		result, err := provider.SaveWebUITheme(r.Context(), theme)
		if err != nil {
			logger.Error("保存 WebUI 主题失败", "theme", theme, "error", err)
			recordAdminAudit(provider, r, "webui", "theme", theme, "failed", "切换 WebUI 主题失败", err.Error(), "")
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"accepted": false,
				"error":    "save_webui_theme_failed",
				"detail":   err.Error(),
			})
			return
		}

		recordAdminAudit(provider, r, "webui", "theme", theme, "success", firstNonEmpty(result.Message, "WebUI 主题已更新"), "", "")
		writeJSON(w, http.StatusOK, result)
	})
}

func mountWebUI(mux *http.ServeMux, logger *slog.Logger, meta runtime.Metadata) {
	if !meta.WebUIEnabled {
		return
	}
	webUIHandler, err := webui.NewHandler(meta.WebUIBaseURL)
	if err != nil {
		logger.Error("创建 WebUI 处理器失败", "error", err, "base_path", meta.WebUIBaseURL)
		return
	}
	webUIHandler.Mount(mux)
}
