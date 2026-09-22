package config

import (
	"fmt"
	"log/slog"
)

// redactedMarker replaces a secret value that is set.
const redactedMarker = "[REDACTED]"

// emptyMarker replaces a secret value that is unset, so an operator can
// tell "set but hidden" apart from "missing".
const emptyMarker = "[EMPTY]"

// redact renders a secret value for display: never the raw value.
func redact(secret string) string {
	if secret == "" {
		return emptyMarker
	}
	return redactedMarker
}

// String renders the configuration for logs and diagnostics. SMTPPassword,
// CloudflareAPIToken and WorkerSecret are never printed: they are replaced
// with a redaction marker that still distinguishes "set" from "unset".
func (c *Config) String() string {
	return fmt.Sprintf(
		"config.Config{SMTPHost: %s, SMTPPort: %d, SMTPUser: %s, SMTPPassword: %s, "+
			"SMTPMaxMessageBytes: %d, SMTPMaxRecipients: %d, SMTPReadTimeout: %s, SMTPWriteTimeout: %s, "+
			"SMTPTLSCert: %s, SMTPTLSKey: %s, CloudflareTransport: %s, CloudflareAccountID: %s, "+
			"CloudflareAPIToken: %s, CloudflareAPIBaseURL: %s, CloudflareTimeout: %s, CloudflareMaxRetries: %d, "+
			"WorkerURL: %s, WorkerSecret: %s, AllowedFromDomains: %v, HealthHost: %s, HealthPort: %d, LogLevel: %s}",
		c.SMTPHost, c.SMTPPort, c.SMTPUser, redact(c.SMTPPassword),
		c.SMTPMaxMessageBytes, c.SMTPMaxRecipients, c.SMTPReadTimeout, c.SMTPWriteTimeout,
		c.SMTPTLSCert, c.SMTPTLSKey, c.CloudflareTransport, c.CloudflareAccountID,
		redact(c.CloudflareAPIToken), c.CloudflareAPIBaseURL, c.CloudflareTimeout, c.CloudflareMaxRetries,
		c.WorkerURL, redact(c.WorkerSecret), c.AllowedFromDomains, c.HealthHost, c.HealthPort, c.LogLevel,
	)
}

// LogValue implements slog.LogValuer so a *Config passed directly to slog
// renders the same redacted view as String, and can never leak
// SMTPPassword, CloudflareAPIToken or WorkerSecret through structured logs.
func (c *Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("smtp_host", c.SMTPHost),
		slog.Int("smtp_port", c.SMTPPort),
		slog.String("smtp_user", c.SMTPUser),
		slog.String("smtp_password", redact(c.SMTPPassword)),
		slog.Int64("smtp_max_message_bytes", c.SMTPMaxMessageBytes),
		slog.Int("smtp_max_recipients", c.SMTPMaxRecipients),
		slog.Duration("smtp_read_timeout", c.SMTPReadTimeout),
		slog.Duration("smtp_write_timeout", c.SMTPWriteTimeout),
		slog.String("smtp_tls_cert", c.SMTPTLSCert),
		slog.String("smtp_tls_key", c.SMTPTLSKey),
		slog.String("cloudflare_transport", string(c.CloudflareTransport)),
		slog.String("cloudflare_account_id", c.CloudflareAccountID),
		slog.String("cloudflare_api_token", redact(c.CloudflareAPIToken)),
		slog.String("cloudflare_api_base_url", c.CloudflareAPIBaseURL),
		slog.Duration("cloudflare_timeout", c.CloudflareTimeout),
		slog.Int("cloudflare_max_retries", c.CloudflareMaxRetries),
		slog.String("worker_url", c.WorkerURL),
		slog.String("worker_secret", redact(c.WorkerSecret)),
		slog.Any("allowed_from_domains", c.AllowedFromDomains),
		slog.String("health_host", c.HealthHost),
		slog.Int("health_port", c.HealthPort),
		slog.String("log_level", c.LogLevel),
	)
}
