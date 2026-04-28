package api

import "net/http"

func registerSystemRoutes(mux *http.ServeMux, auth *adminAuthManager, provider systemRouteProvider) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ready",
			"runtime": provider.Snapshot(),
		})
	})

	mux.HandleFunc("/api/admin/auth/state", auth.state)
	mux.HandleFunc("/api/admin/auth/setup", auth.setup)
	mux.HandleFunc("/api/admin/auth/login", auth.login)
	mux.HandleFunc("/api/admin/auth/logout", auth.logout)
	mux.HandleFunc("/api/admin/auth/password", auth.changePassword)

	mux.HandleFunc("/api/admin/runtime", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, provider.Snapshot())
	})

	mux.HandleFunc("/api/admin/meta", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, provider.Metadata())
	})
}
