package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func fullyPopulatedConfig(t *testing.T) *Config {
	t.Helper()
	cfg, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{
		"SMTP_USER":             "op-user",
		"SMTP_PASSWORD":         "super-secret-password",
		"CLOUDFLARE_ACCOUNT_ID": "acct-999",
		"CLOUDFLARE_API_TOKEN":  "cf-secret-token",
		"CLOUDFLARE_TRANSPORT":  "rest",
	})))
	if err != nil {
		t.Fatalf("unexpected error building config fixture: %v", err)
	}
	// WorkerSecret is not populated by REST transport env; set it directly on
	// the struct so String()/LogValue() redaction can be exercised even
	// though loadFrom validation only requires it for worker transport.
	cfg.WorkerSecret = "worker-secret-value"
	return cfg
}

func TestConfigString_RedactsSecrets(t *testing.T) {
	cfg := fullyPopulatedConfig(t)
	out := cfg.String()

	for _, secret := range []string{"super-secret-password", "cf-secret-token", "worker-secret-value"} {
		if strings.Contains(out, secret) {
			t.Errorf("String() output leaks secret %q: %s", secret, out)
		}
	}

	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected String() output to contain [REDACTED] markers, got: %s", out)
	}
	// Non-secret fields must still be visible.
	if !strings.Contains(out, "op-user") {
		t.Errorf("expected String() output to include non-secret SMTPUser, got: %s", out)
	}
	if !strings.Contains(out, "acct-999") {
		t.Errorf("expected String() output to include non-secret CloudflareAccountID, got: %s", out)
	}
}

func TestConfigString_MarksEmptySecretsDistinctlyFromSet(t *testing.T) {
	cfg := fullyPopulatedConfig(t)
	cfg.WorkerSecret = ""
	out := cfg.String()

	if !strings.Contains(out, "[EMPTY]") {
		t.Errorf("expected an unset secret to be rendered as [EMPTY], got: %s", out)
	}
}

func TestConfigLogValue_RedactsSecrets(t *testing.T) {
	cfg := fullyPopulatedConfig(t)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("config loaded", "config", cfg)

	out := buf.String()
	for _, secret := range []string{"super-secret-password", "cf-secret-token", "worker-secret-value"} {
		if strings.Contains(out, secret) {
			t.Errorf("slog output leaks secret %q: %s", secret, out)
		}
	}
	if !strings.Contains(out, "REDACTED") {
		t.Errorf("expected slog output to contain a REDACTED marker, got: %s", out)
	}
}

func TestConfigLogValue_ReturnsGroupWithNonSecretFields(t *testing.T) {
	cfg := fullyPopulatedConfig(t)
	val := cfg.LogValue()

	if val.Kind() != slog.KindGroup {
		t.Fatalf("expected LogValue() to return a group, got kind %v", val.Kind())
	}

	found := false
	for _, attr := range val.Group() {
		if attr.Key == "smtp_user" && attr.Value.String() == "op-user" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected LogValue() group to include a non-secret smtp_user attribute")
	}
}
