package config

import "testing"

func TestSanitizedMap_UsesYAMLFieldNames(t *testing.T) {
	cfg := &Config{
		App: AppConfig{
			Name:    "go-bot",
			OwnerQQ: "123456789",
		},
		Security: SecurityConfig{
			AdminAuth: AdminAuthConfig{
				Enabled:  true,
				Password: "secret123",
			},
		},
	}

	view := SanitizedMap(cfg)
	if _, ok := view["app"]; !ok {
		t.Fatalf("sanitized map should expose lower-case app key, got keys=%v", view)
	}
	if _, ok := view["App"]; ok {
		t.Fatalf("sanitized map should not expose Go struct field name App")
	}
	securityValue, ok := view["security"].(map[string]any)
	if !ok {
		t.Fatalf("sanitized security missing or invalid: %#v", view["security"])
	}
	adminAuthValue, ok := securityValue["admin_auth"].(map[string]any)
	if !ok {
		t.Fatalf("sanitized admin_auth missing or invalid: %#v", securityValue["admin_auth"])
	}
	if got := adminAuthValue["password"]; got != redactedSecret {
		t.Fatalf("sanitized admin auth password = %#v, want %q", got, redactedSecret)
	}
}
