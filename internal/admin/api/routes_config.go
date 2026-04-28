package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/XiaoLozee/go-bot/internal/config"
)

func registerConfigRoutes(mux *http.ServeMux, logger *slog.Logger, provider configRouteProvider) {
	mux.HandleFunc("/api/admin/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, provider.ConfigView())
	})

	mux.HandleFunc("/api/admin/config/validate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}

		defer func() { _ = r.Body.Close() }()

		var raw map[string]any
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"valid":  false,
				"error":  "invalid_json",
				"detail": err.Error(),
			})
			return
		}

		cfg, err := config.DecodeDraftMap(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"valid":  false,
				"error":  "invalid_config",
				"detail": err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"valid":             true,
			"normalized_config": config.SanitizedMap(cfg),
		})
	})

	mux.HandleFunc("/api/admin/config/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}

		defer func() { _ = r.Body.Close() }()

		var raw map[string]any
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "invalid_json",
				"detail":   err.Error(),
			})
			return
		}

		cfg, err := config.DecodeDraftMap(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"accepted": false,
				"error":    "invalid_config",
				"detail":   err.Error(),
			})
			return
		}

		result, err := provider.SaveConfig(r.Context(), cfg)
		if err != nil {
			logger.Error("保存配置失败", "error", err)
			recordAdminAudit(provider, r, "config", "save", "system", "failed", "保存系统配置失败", err.Error(), "")
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"accepted":          false,
				"persisted":         false,
				"error":             "save_failed",
				"detail":            err.Error(),
				"normalized_config": config.SanitizedMap(cfg),
			})
			return
		}
		recordAdminAudit(provider, r, "config", "save", "system", "success", firstNonEmpty(result.Message, "系统配置已保存"), "", "")
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/api/admin/config/restart", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}

		result, err := provider.HotRestart(r.Context())
		if err != nil {
			logger.Error("热重启运行时失败", "error", err)
			recordAdminAudit(provider, r, "config", "restart", "system", "failed", "热重启运行时失败", err.Error(), "")
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"accepted": false,
				"error":    "restart_failed",
				"detail":   err.Error(),
			})
			return
		}

		recordAdminAudit(provider, r, "config", "restart", "system", "success", firstNonEmpty(result.Message, "系统配置已热重启"), "", "")
		writeJSON(w, http.StatusOK, result)
	})
}
