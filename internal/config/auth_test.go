package config

import "testing"

func TestHashAndVerifyAdminPassword(t *testing.T) {
	hash, err := HashAdminPassword("secret123")
	if err != nil {
		t.Fatalf("HashAdminPassword() error = %v", err)
	}
	if hash == "secret123" {
		t.Fatalf("hash should not equal raw password")
	}
	if !VerifyAdminPassword(hash, "secret123") {
		t.Fatalf("VerifyAdminPassword() = false, want true")
	}
	if VerifyAdminPassword(hash, "bad-password") {
		t.Fatalf("VerifyAdminPassword() = true, want false")
	}
}

func TestMergeSensitiveValuesPreservesSecrets(t *testing.T) {
	base := testConfig()
	base.Connections[0].Action.AccessToken = "token-1"
	base.Plugins[0].Config["api_key"] = "api-key-1"
	base.Storage.Media.R2.SecretAccessKey = "r2-secret"

	draft := testConfig()
	draft.App.Name = "go-bot-next"
	draft.Connections[0].Action.AccessToken = "******"
	draft.Plugins[0].Config["api_key"] = "******"
	draft.Security.AdminAuth.Password = "******"
	draft.Storage.Media.R2.SecretAccessKey = "******"

	merged, err := MergeSensitiveValues(base, draft)
	if err != nil {
		t.Fatalf("MergeSensitiveValues() error = %v", err)
	}
	if merged.App.Name != "go-bot-next" {
		t.Fatalf("App.Name = %s, want go-bot-next", merged.App.Name)
	}
	if merged.Connections[0].Action.AccessToken != "token-1" {
		t.Fatalf("AccessToken = %s, want preserved token", merged.Connections[0].Action.AccessToken)
	}
	if merged.Security.AdminAuth.Password != "secret" {
		t.Fatalf("Password = %s, want preserved password", merged.Security.AdminAuth.Password)
	}
	if value := merged.Plugins[0].Config["api_key"]; value != "api-key-1" {
		t.Fatalf("plugin api_key = %v, want preserved api-key-1", value)
	}
	if merged.Storage.Media.R2.SecretAccessKey != "r2-secret" {
		t.Fatalf("R2.SecretAccessKey = %s, want preserved secret", merged.Storage.Media.R2.SecretAccessKey)
	}
}
