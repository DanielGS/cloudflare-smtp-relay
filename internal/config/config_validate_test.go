package config

import (
	"strings"
	"testing"
)

// withEnv returns a copy of base with overrides applied (empty string values
// are set explicitly, which differs from being unset).
func withEnv(base map[string]string, overrides map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}

func TestLoadFrom_SMTPPortOutOfRange(t *testing.T) {
	for _, bad := range []string{"0", "-1", "65536", "100000"} {
		_, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"SMTP_PORT": bad})))
		if err == nil {
			t.Errorf("SMTP_PORT=%q: expected a validation error, got nil", bad)
			continue
		}
		if !strings.Contains(err.Error(), "SMTP_PORT") {
			t.Errorf("SMTP_PORT=%q: expected error to mention SMTP_PORT, got: %v", bad, err)
		}
	}
}

func TestLoadFrom_HealthPortOutOfRange(t *testing.T) {
	_, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"HEALTH_PORT": "0"})))
	if err == nil || !strings.Contains(err.Error(), "HEALTH_PORT") {
		t.Fatalf("expected an error mentioning HEALTH_PORT, got: %v", err)
	}
}

func TestLoadFrom_MaxMessageBytesMustBePositiveAndUnderCloudflareCap(t *testing.T) {
	cases := []string{"0", "-1", "5242881", "10000000"}
	for _, bad := range cases {
		_, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"SMTP_MAX_MESSAGE_BYTES": bad})))
		if err == nil {
			t.Errorf("SMTP_MAX_MESSAGE_BYTES=%q: expected a validation error, got nil", bad)
			continue
		}
		if !strings.Contains(err.Error(), "SMTP_MAX_MESSAGE_BYTES") {
			t.Errorf("SMTP_MAX_MESSAGE_BYTES=%q: expected error to mention the variable, got: %v", bad, err)
		}
	}

	// The Cloudflare hard cap itself must be accepted.
	_, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"SMTP_MAX_MESSAGE_BYTES": "5242880"})))
	if err != nil {
		t.Fatalf("expected the Cloudflare cap value to be accepted, got error: %v", err)
	}
}

func TestLoadFrom_MaxRecipientsMustBePositiveAndUnderCloudflareCap(t *testing.T) {
	for _, bad := range []string{"0", "-5", "51", "1000"} {
		_, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"SMTP_MAX_RECIPIENTS": bad})))
		if err == nil || !strings.Contains(err.Error(), "SMTP_MAX_RECIPIENTS") {
			t.Errorf("SMTP_MAX_RECIPIENTS=%q: expected error mentioning the variable, got: %v", bad, err)
		}
	}
}

func TestLoadFrom_InvalidReadWriteTimeoutDurations(t *testing.T) {
	_, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"SMTP_READ_TIMEOUT": "not-a-duration"})))
	if err == nil || !strings.Contains(err.Error(), "SMTP_READ_TIMEOUT") {
		t.Fatalf("expected error mentioning SMTP_READ_TIMEOUT, got: %v", err)
	}

	_, err = loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"SMTP_WRITE_TIMEOUT": "banana"})))
	if err == nil || !strings.Contains(err.Error(), "SMTP_WRITE_TIMEOUT") {
		t.Fatalf("expected error mentioning SMTP_WRITE_TIMEOUT, got: %v", err)
	}
}

func TestLoadFrom_TLSCertRequiresKeyAndViceVersa(t *testing.T) {
	_, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"SMTP_TLS_CERT": "/path/cert.pem"})))
	if err == nil || !strings.Contains(err.Error(), "SMTP_TLS_KEY") {
		t.Fatalf("expected an error requiring SMTP_TLS_KEY when only the cert is set, got: %v", err)
	}

	_, err = loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"SMTP_TLS_KEY": "/path/key.pem"})))
	if err == nil || !strings.Contains(err.Error(), "SMTP_TLS_CERT") {
		t.Fatalf("expected an error requiring SMTP_TLS_CERT when only the key is set, got: %v", err)
	}

	// Both set together must be accepted.
	_, err = loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{
		"SMTP_TLS_CERT": "/path/cert.pem",
		"SMTP_TLS_KEY":  "/path/key.pem",
	})))
	if err != nil {
		t.Fatalf("expected no error when both TLS cert and key are set, got: %v", err)
	}
}

func TestLoadFrom_CloudflareTransportMustBeRestOrWorker(t *testing.T) {
	_, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"CLOUDFLARE_TRANSPORT": "carrier-pigeon"})))
	if err == nil || !strings.Contains(err.Error(), "CLOUDFLARE_TRANSPORT") {
		t.Fatalf("expected an error mentioning CLOUDFLARE_TRANSPORT, got: %v", err)
	}
}

func TestLoadFrom_WorkerTransportRequiresWorkerURLAndSecret(t *testing.T) {
	env := map[string]string{
		"SMTP_USER":            "relay-user",
		"SMTP_PASSWORD":        "relay-pass",
		"CLOUDFLARE_TRANSPORT": "worker",
	}
	_, err := loadFrom(testGetenv(env))
	if err == nil {
		t.Fatalf("expected an error when worker transport is missing URL and secret")
	}
	if !strings.Contains(err.Error(), "WORKER_URL") || !strings.Contains(err.Error(), "WORKER_SECRET") {
		t.Fatalf("expected error to mention both WORKER_URL and WORKER_SECRET, got: %v", err)
	}

	env["WORKER_URL"] = "https://worker.example.com/relay"
	env["WORKER_SECRET"] = "wk-secret"
	cfg, err := loadFrom(testGetenv(env))
	if err != nil {
		t.Fatalf("expected worker transport with URL and secret to succeed, got: %v", err)
	}
	if cfg.WorkerURL != "https://worker.example.com/relay" {
		t.Errorf("expected WorkerURL to be set, got %q", cfg.WorkerURL)
	}

	// Worker transport must not require Cloudflare REST credentials.
	if cfg.CloudflareAccountID != "" || cfg.CloudflareAPIToken != "" {
		t.Errorf("expected no Cloudflare REST credentials required for worker transport")
	}
}

func TestLoadFrom_WorkerURLMustBeAbsoluteHTTPOrHTTPS(t *testing.T) {
	env := map[string]string{
		"SMTP_USER":            "relay-user",
		"SMTP_PASSWORD":        "relay-pass",
		"CLOUDFLARE_TRANSPORT": "worker",
		"WORKER_SECRET":        "wk-secret",
	}

	for _, bad := range []string{"not-a-url", "ftp://worker.example.com", "/relative/path", "worker.example.com"} {
		env["WORKER_URL"] = bad
		_, err := loadFrom(testGetenv(env))
		if err == nil || !strings.Contains(err.Error(), "WORKER_URL") {
			t.Errorf("WORKER_URL=%q: expected an error mentioning WORKER_URL, got: %v", bad, err)
		}
	}

	env["WORKER_URL"] = "https://worker.example.com/relay"
	if _, err := loadFrom(testGetenv(env)); err != nil {
		t.Fatalf("expected a valid absolute https URL to be accepted, got: %v", err)
	}
}

func TestLoadFrom_CloudflareMaxRetriesRange(t *testing.T) {
	for _, bad := range []string{"-1", "6", "100"} {
		_, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"CLOUDFLARE_MAX_RETRIES": bad})))
		if err == nil || !strings.Contains(err.Error(), "CLOUDFLARE_MAX_RETRIES") {
			t.Errorf("CLOUDFLARE_MAX_RETRIES=%q: expected error mentioning the variable, got: %v", bad, err)
		}
	}
	for _, ok := range []string{"0", "5"} {
		_, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"CLOUDFLARE_MAX_RETRIES": ok})))
		if err != nil {
			t.Errorf("CLOUDFLARE_MAX_RETRIES=%q: expected no error, got: %v", ok, err)
		}
	}
}

func TestLoadFrom_LogLevelMustBeKnownValueCaseInsensitive(t *testing.T) {
	_, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"LOG_LEVEL": "verbose"})))
	if err == nil || !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Fatalf("expected an error mentioning LOG_LEVEL, got: %v", err)
	}

	cfg, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{"LOG_LEVEL": "WARN"})))
	if err != nil {
		t.Fatalf("expected LOG_LEVEL=WARN to be accepted, got: %v", err)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("expected LogLevel to be normalized to lowercase \"warn\", got %q", cfg.LogLevel)
	}
}

func TestNormalizeDomains(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"empty", "", nil},
		{"single", "example.com", []string{"example.com"}},
		{"trims and lowercases", " Example.COM , Foo.Org ", []string{"example.com", "foo.org"}},
		{"drops empties", "a.com,,b.com,", []string{"a.com", "b.com"}},
		{"deduplicates", "a.com,A.com,a.com", []string{"a.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeDomains(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("normalizeDomains(%q) = %v, want %v", tt.raw, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("normalizeDomains(%q) = %v, want %v", tt.raw, got, tt.want)
				}
			}
		})
	}
}

func TestLoadFrom_AllowedFromDomainsIsNormalizedEndToEnd(t *testing.T) {
	cfg, err := loadFrom(testGetenv(withEnv(minimalRequiredEnv(), map[string]string{
		"ALLOWED_FROM_DOMAINS": " Example.com, example.com ,Other.ORG",
	})))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	want := []string{"example.com", "other.org"}
	if len(cfg.AllowedFromDomains) != len(want) {
		t.Fatalf("AllowedFromDomains = %v, want %v", cfg.AllowedFromDomains, want)
	}
	for i := range want {
		if cfg.AllowedFromDomains[i] != want[i] {
			t.Fatalf("AllowedFromDomains = %v, want %v", cfg.AllowedFromDomains, want)
		}
	}
}
