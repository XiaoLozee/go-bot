package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/XiaoLozee/go-bot/internal/runtime"
)

const adminSessionCookieName = "gobot_admin_session"

type adminAuthManager struct {
	logger     *slog.Logger
	provider   runtime.Provider
	sessionTTL time.Duration

	mu       sync.Mutex
	sessions map[string]time.Time
}

type authPayload struct {
	Password string `json:"password"`
}

type passwordChangePayload struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func newAdminAuthManager(logger *slog.Logger, provider runtime.Provider) *adminAuthManager {
	return &adminAuthManager{
		logger:     logger,
		provider:   provider,
		sessionTTL: 24 * time.Hour,
		sessions:   make(map[string]time.Time),
	}
}

func (a *adminAuthManager) state(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	status := a.provider.AdminAuthStatus()
	meta := a.provider.Metadata()
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":        status.Enabled,
		"configured":     status.Configured,
		"requires_setup": status.RequiresSetup,
		"authenticated":  a.isAuthenticated(r),
		"webui_theme":    meta.WebUITheme,
	})
}

func (a *adminAuthManager) setup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	status := a.provider.AdminAuthStatus()
	if status.Configured {
		recordAdminAudit(a.provider, r, "auth", "setup", "后台密码", "failed", "初始化后台密码失败", "后台密码已设置，请直接登录", "")
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":  "already_configured",
			"detail": "后台密码已设置，请直接登录",
		})
		return
	}

	payload, ok := a.decodePayload(w, r)
	if !ok {
		return
	}

	result, err := a.provider.ConfigureAdminAuth(r.Context(), payload.Password)
	if err != nil {
		recordAdminAudit(a.provider, r, "auth", "setup", "后台密码", "failed", "初始化后台密码失败", err.Error(), "")
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "setup_failed",
			"detail": err.Error(),
		})
		return
	}

	if err := a.issueSessionCookie(w, r); err != nil {
		a.logger.Error("创建后台会话失败", "error", err)
		recordAdminAudit(a.provider, r, "auth", "setup", "后台密码", "failed", "初始化后台密码失败", err.Error(), "")
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":  "session_failed",
			"detail": err.Error(),
		})
		return
	}

	recordAdminAudit(a.provider, r, "auth", "setup", "后台密码", "success", firstNonEmpty(result.Message, "后台密码已设置"), "", "")

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"auth": map[string]any{
			"configured":     true,
			"requires_setup": false,
			"authenticated":  true,
		},
		"result": result,
	})
}

func (a *adminAuthManager) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	status := a.provider.AdminAuthStatus()
	if status.RequiresSetup {
		recordAdminAudit(a.provider, r, "auth", "login", "后台密码", "failed", "后台登录失败", "后台尚未设置密码，请先完成初始化", "")
		writeJSON(w, http.StatusPreconditionRequired, map[string]any{
			"error":  "setup_required",
			"detail": "后台尚未设置密码，请先完成初始化",
		})
		return
	}

	payload, ok := a.decodePayload(w, r)
	if !ok {
		return
	}

	if !a.provider.VerifyAdminPassword(payload.Password) {
		recordAdminAudit(a.provider, r, "auth", "login", "后台密码", "failed", "后台登录失败", "密码错误", "")
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":  "invalid_credentials",
			"detail": "密码错误",
		})
		return
	}

	if err := a.issueSessionCookie(w, r); err != nil {
		a.logger.Error("创建后台会话失败", "error", err)
		recordAdminAudit(a.provider, r, "auth", "login", "后台密码", "failed", "后台登录失败", err.Error(), "")
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":  "session_failed",
			"detail": err.Error(),
		})
		return
	}

	recordAdminAudit(a.provider, r, "auth", "login", "后台密码", "success", "后台登录成功", "", "")
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"auth": map[string]any{
			"configured":     true,
			"requires_setup": false,
			"authenticated":  true,
		},
	})
}

func (a *adminAuthManager) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	a.clearSession(r)
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		SameSite: http.SameSiteLaxMode,
	})

	recordAdminAudit(a.provider, r, "auth", "logout", "后台密码", "success", "后台已退出登录", "", "")
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
	})
}

func (a *adminAuthManager) changePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !a.isAuthenticated(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":  "unauthorized",
			"detail": "请先登录后台",
		})
		return
	}

	defer func() { _ = r.Body.Close() }()
	var payload passwordChangePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "invalid_json",
			"detail": err.Error(),
		})
		return
	}
	payload.CurrentPassword = strings.TrimSpace(payload.CurrentPassword)
	payload.NewPassword = strings.TrimSpace(payload.NewPassword)
	if payload.CurrentPassword == "" || payload.NewPassword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "invalid_password",
			"detail": "当前密码和新密码不能为空",
		})
		return
	}

	result, err := a.provider.ChangeAdminPassword(r.Context(), payload.CurrentPassword, payload.NewPassword)
	if err != nil {
		recordAdminAudit(a.provider, r, "auth", "change_password", "后台密码", "failed", "更新后台密码失败", err.Error(), "")
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "change_password_failed",
			"detail": err.Error(),
		})
		return
	}

	recordAdminAudit(a.provider, r, "auth", "change_password", "后台密码", "success", firstNonEmpty(result.Message, "后台密码已更新"), "", "")
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": result,
	})
}

func (a *adminAuthManager) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/admin/") || strings.HasPrefix(r.URL.Path, "/api/admin/auth/") {
			next.ServeHTTP(w, r)
			return
		}

		status := a.provider.AdminAuthStatus()
		if status.RequiresSetup {
			writeJSON(w, http.StatusPreconditionRequired, map[string]any{
				"error":  "setup_required",
				"detail": "后台尚未设置密码，请先完成初始化",
			})
			return
		}
		if status.Enabled && !a.isAuthenticated(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error":  "unauthorized",
				"detail": "请先登录后台",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (a *adminAuthManager) decodePayload(w http.ResponseWriter, r *http.Request) (authPayload, bool) {
	defer func() { _ = r.Body.Close() }()

	var payload authPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "invalid_json",
			"detail": err.Error(),
		})
		return authPayload{}, false
	}
	payload.Password = strings.TrimSpace(payload.Password)
	if payload.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "invalid_password",
			"detail": "密码不能为空",
		})
		return authPayload{}, false
	}
	return payload, true
}

func (a *adminAuthManager) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	expiresAt, ok := a.sessions[cookie.Value]
	if !ok {
		return false
	}
	if time.Now().After(expiresAt) {
		delete(a.sessions, cookie.Value)
		return false
	}
	a.sessions[cookie.Value] = time.Now().Add(a.sessionTTL)
	return true
}

func (a *adminAuthManager) issueSessionCookie(w http.ResponseWriter, r *http.Request) error {
	token, err := generateAdminSessionToken()
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.sessions[token] = time.Now().Add(a.sessionTTL)
	a.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(a.sessionTTL),
	})
	return nil
}

func requestIsSecure(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); strings.EqualFold(forwardedProto, "https") {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(r.Header.Get("Forwarded"))), "proto=https")
}

func (a *adminAuthManager) clearSession(r *http.Request) {
	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil || cookie.Value == "" {
		return
	}
	a.mu.Lock()
	delete(a.sessions, cookie.Value)
	a.mu.Unlock()
}

func generateAdminSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
