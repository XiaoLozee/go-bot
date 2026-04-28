package api

import "net/http"

func registerAuditRoutes(mux *http.ServeMux, provider auditRouteProvider) {
	mux.HandleFunc("/api/admin/audit/logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		limit := parseAuditSystemLogLimit(r.URL.Query().Get("limit"))
		view, err := loadAuditSystemLogView(provider, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":   "system log unavailable",
				"message": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, view)
	})

	mux.HandleFunc("/api/admin/audit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		limit := parseAuditLogLimit(r.URL.Query().Get("limit"))
		query := parseAuditQuery(r)
		sourceLimit := limit
		if query.HasFilter() {
			sourceLimit = maxAuditLogLimit
		}
		items := provider.AuditLogs(sourceLimit)
		if query.HasFilter() {
			items = applyAuditQueryFilters(items, query, limit)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": items,
			"limit": limit,
		})
	})
}
