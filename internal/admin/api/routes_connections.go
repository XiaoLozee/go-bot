package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/XiaoLozee/go-bot/internal/config"
)

func registerConnectionRoutes(mux *http.ServeMux, logger *slog.Logger, provider connectionRouteProvider) {
	mux.HandleFunc("/api/admin/connections", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, provider.ConnectionSnapshots())
		case http.MethodPost:
			defer func() { _ = r.Body.Close() }()

			var conn config.ConnectionConfig
			if err := json.NewDecoder(r.Body).Decode(&conn); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{
					"error":  "invalid_json",
					"detail": err.Error(),
				})
				return
			}

			result, err := provider.SaveConnectionConfig(r.Context(), conn)
			if err != nil {
				logger.Error("保存网络配置失败", "connection", conn.ID, "error", err)
				recordAdminAudit(provider, r, "connection", "save", conn.ID, "failed", "保存网络配置失败", err.Error(), "")
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error":      "save_connection_failed",
					"connection": conn.ID,
					"detail":     err.Error(),
				})
				return
			}
			recordAdminAudit(provider, r, "connection", "save", conn.ID, "success", firstNonEmpty(result.Message, "网络配置已保存"), "", "")
			writeJSON(w, http.StatusOK, result)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	})

	mux.HandleFunc("/api/admin/connections/", func(w http.ResponseWriter, r *http.Request) {
		parts := splitRouteParts(r.URL.Path, "/api/admin/connections/")
		if len(parts) == 1 && r.Method == http.MethodGet {
			id := parts[0]
			if id == "" {
				http.NotFound(w, r)
				return
			}

			detail, ok := provider.ConnectionDetail(id)
			if !ok {
				writeJSON(w, http.StatusNotFound, map[string]any{
					"error":      "connection not found",
					"connection": id,
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
		switch action {
		case "probe":
			detail, err := provider.RefreshConnection(r.Context(), id)
			if err != nil {
				logger.Error("连接探活失败", "connection", id, "error", err)
				recordAdminAudit(provider, r, "connection", "probe", id, "failed", "连接探活失败", err.Error(), "")
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error":      "probe_failed",
					"connection": id,
					"detail":     err.Error(),
				})
				return
			}
			recordAdminAudit(provider, r, "connection", "probe", id, "success", "连接探活成功", "", "")
			writeJSON(w, http.StatusOK, detail)
		case "start", "stop":
			enabled := action == "start"
			result, err := provider.SetConnectionEnabled(r.Context(), id, enabled)
			if err != nil {
				logger.Error("连接启停失败", "connection", id, "action", action, "error", err)
				recordAdminAudit(provider, r, "connection", action, id, "failed", "连接启停失败", err.Error(), "")
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error":      "toggle_connection_failed",
					"connection": id,
					"action":     action,
					"detail":     err.Error(),
				})
				return
			}
			recordAdminAudit(provider, r, "connection", action, id, "success", firstNonEmpty(result.Message, "连接启停成功"), "", "")
			writeJSON(w, http.StatusOK, result)
		case "delete":
			result, err := provider.DeleteConnection(r.Context(), id)
			if err != nil {
				logger.Error("删除网络配置失败", "connection", id, "error", err)
				recordAdminAudit(provider, r, "connection", "delete", id, "failed", "删除网络配置失败", err.Error(), "")
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error":      "delete_connection_failed",
					"connection": id,
					"detail":     err.Error(),
				})
				return
			}
			recordAdminAudit(provider, r, "connection", "delete", id, "success", firstNonEmpty(result.Message, "网络配置已删除"), "", "")
			writeJSON(w, http.StatusOK, result)
		default:
			http.NotFound(w, r)
		}
	})
}
