package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/XiaoLozee/go-bot/internal/runtime"
)

const maxPluginUploadSize = 128 << 20

func NewRouter(logger *slog.Logger, provider runtime.Provider) http.Handler {
	mux := http.NewServeMux()
	auth := newAdminAuthManager(logger, provider)

	registerSystemRoutes(mux, auth, provider)
	registerAIRoutes(mux, logger, provider)
	registerAuditRoutes(mux, provider)
	registerConfigRoutes(mux, logger, provider)
	registerConnectionRoutes(mux, logger, provider)
	registerPluginRoutes(mux, logger, provider)
	registerWebUIRoutes(mux, logger, provider)
	mountWebUI(mux, logger, provider.Metadata())

	return auth.middleware(mux)
}

func parseMessageLimit(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 30
	}
	if value > 200 {
		return 200
	}
	return value
}

func parseSuggestionLimit(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 8
	}
	if value > 20 {
		return 20
	}
	return value
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	if len(methods) > 0 {
		w.Header().Set("Allow", strings.Join(methods, ", "))
	}
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
		"error": "method not allowed",
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
