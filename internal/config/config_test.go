package config

import (
	"strings"
	"testing"
	"time"
)

// testGetenv builds a getenv seam (matching os.LookupEnv's signature) backed
// by an in-memory map, so tests never need to mutate the real process
// environment.
func testGetenv(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	}
}

// minimalRequiredEnv returns the smallest set of environment variables that
// must be present for a REST-transport config to load successfully: SMTP
// credentials plus the Cloudflare REST credentials required for the default
// transport.
func minimalRequiredEnv() map[string]string {
	return map[string]string{
		"SMTP_USER":             "relay-user",
		"SMTP_PASSWORD":         "relay-pass",
		"CLOUDFLARE_ACCOUNT_ID": "acct-123",
		"CLOUDFLARE_API_TOKEN":  "cf-token",
	}
}

func TestLoadFrom_AppliesDefaultsWhenOnlyRequiredFieldsAreSet(t *testing.T) {
	cfg, err := loadFrom(testGetenv(minimalRequiredEnv()))
	if err != nil {
		t.Fatalf("expected no error with minimal required env set, got: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected a non-nil *Config")
	}

	if cfg.SMTPHost != "0.0.0.0" {
		t.Errorf("SMTPHost: expected default 0.0.0.0, got %q", cfg.SMTPHost)
	}
	if cfg.SMTPPort != 2525 {
		t.Errorf("SMTPPort: expected default 2525, got %d", cfg.SMTPPort)
	}
	if cfg.SMTPUser != "relay-user" {
		t.Errorf("SMTPUser: expected relay-user, got %q", cfg.SMTPUser)
	}
	if cfg.SMTPPassword != "relay-pass" {
		t.Errorf("SMTPPassword: expected relay-pass, got %q", cfg.SMTPPassword)
	}
	if cfg.SMTPMaxMessageBytes != 5242880 {
		t.Errorf("SMTPMaxMessageBytes: expected default 5242880, got %d", cfg.SMTPMaxMessageBytes)
	}
	if cfg.SMTPMaxRecipients != 50 {
		t.Errorf("SMTPMaxRecipients: expected default 50, got %d", cfg.SMTPMaxRecipients)
	}
	if cfg.SMTPReadTimeout != 30*time.Second {
		t.Errorf("SMTPReadTimeout: expected default 30s, got %v", cfg.SMTPReadTimeout)
	}
	if cfg.SMTPWriteTimeout != 30*time.Second {
		t.Errorf("SMTPWriteTimeout: expected default 30s, got %v", cfg.SMTPWriteTimeout)
	}
	if cfg.SMTPTLSCert != "" || cfg.SMTPTLSKey != "" {
		t.Errorf("expected empty TLS cert/key by default, got cert=%q key=%q", cfg.SMTPTLSCert, cfg.SMTPTLSKey)
	}
	if cfg.CloudflareTransport != TransportREST {
		t.Errorf("CloudflareTransport: expected default rest, got %q", cfg.CloudflareTransport)
	}
	if cfg.CloudflareAccountID != "acct-123" {
		t.Errorf("CloudflareAccountID: expected acct-123, got %q", cfg.CloudflareAccountID)
	}
	if cfg.CloudflareAPIToken != "cf-token" {
		t.Errorf("CloudflareAPIToken: expected cf-token, got %q", cfg.CloudflareAPIToken)
	}
	if cfg.CloudflareAPIBaseURL != "https://api.cloudflare.com/client/v4" {
		t.Errorf("CloudflareAPIBaseURL: expected default, got %q", cfg.CloudflareAPIBaseURL)
	}
	if cfg.CloudflareTimeout != 15*time.Second {
		t.Errorf("CloudflareTimeout: expected default 15s, got %v", cfg.CloudflareTimeout)
	}
	if cfg.CloudflareMaxRetries != 2 {
		t.Errorf("CloudflareMaxRetries: expected default 2, got %d", cfg.CloudflareMaxRetries)
	}
	if cfg.WorkerURL != "" || cfg.WorkerSecret != "" {
		t.Errorf("expected empty worker fields by default, got url=%q secret=%q", cfg.WorkerURL, cfg.WorkerSecret)
	}
	if len(cfg.AllowedFromDomains) != 0 {
		t.Errorf("AllowedFromDomains: expected empty by default, got %v", cfg.AllowedFromDomains)
	}
	if cfg.HealthHost != "0.0.0.0" {
		t.Errorf("HealthHost: expected default 0.0.0.0, got %q", cfg.HealthHost)
	}
	if cfg.HealthPort != 8080 {
		t.Errorf("HealthPort: expected default 8080, got %d", cfg.HealthPort)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel: expected default info, got %q", cfg.LogLevel)
	}
}

func TestLoadFrom_MissingRequiredFieldsAggregatesAllErrors(t *testing.T) {
	// No SMTP_USER, no SMTP_PASSWORD, no Cloudflare credentials at all.
	_, err := loadFrom(testGetenv(map[string]string{}))
	if err == nil {
		t.Fatalf("expected an error when required fields are missing, got nil")
	}

	msg := err.Error()
	for _, want := range []string{"SMTP_USER", "SMTP_PASSWORD", "CLOUDFLARE_ACCOUNT_ID", "CLOUDFLARE_API_TOKEN"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected aggregated error to mention %q, got: %s", want, msg)
		}
	}
}
