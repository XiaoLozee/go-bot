package ingress

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

func applyAccessTokenHeader(header http.Header, accessToken string) {
	if header == nil {
		return
	}
	if token := strings.TrimSpace(accessToken); token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
}

func requestAccessToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if token := parseAuthorizationToken(r.Header.Get("Authorization")); token != "" {
		return token
	}
	return strings.TrimSpace(r.URL.Query().Get("access_token"))
}

func parseAuthorizationToken(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) >= 7 && strings.EqualFold(trimmed[:7], "Bearer ") {
		return strings.TrimSpace(trimmed[7:])
	}
	return trimmed
}

func accessTokenMatches(expected, actual string) bool {
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)
	if expected == "" {
		return true
	}
	if len(expected) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}
