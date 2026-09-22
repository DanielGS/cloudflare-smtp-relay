// Package config loads and validates the relay's configuration from the
// process environment. It fails fast, aggregating every validation problem
// into one returned error so an operator fixing a .env file sees every
// mistake at once, and it never lets secret values leak through String or
// LogValue.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Transport selects how the relay delivers outbound mail to Cloudflare.
type Transport string

const (
	// TransportREST delivers via the Cloudflare Email Routing REST API.
	TransportREST Transport = "rest"
	// TransportWorker delivers via a Cloudflare Worker HTTP endpoint.
	TransportWorker Transport = "worker"
)

// Cloudflare's hard limits, used both as defaults and as validation caps.
const (
	cloudflareMaxMessageBytes = 5242880 // 5 MiB
	cloudflareMaxRecipients   = 50
)

// Config holds the fully loaded and validated relay configuration.
type Config struct {
	SMTPHost            string
	SMTPPort            int
	SMTPUser            string
	SMTPPassword        string
	SMTPMaxMessageBytes int64
	SMTPMaxRecipients   int
	SMTPReadTimeout     time.Duration
	SMTPWriteTimeout    time.Duration
	SMTPTLSCert         string
	SMTPTLSKey          string

	CloudflareTransport  Transport
	CloudflareAccountID  string
	CloudflareAPIToken   string
	CloudflareAPIBaseURL string
	CloudflareTimeout    time.Duration
	CloudflareMaxRetries int

	WorkerURL    string
	WorkerSecret string

	AllowedFromDomains []string

	HealthHost string
	HealthPort int

	LogLevel string
}

// getenvFunc is the environment-lookup seam used by loadFrom, matching
// os.LookupEnv's signature so production code and tests share the same
// shape without tests needing to mutate the real process environment.
type getenvFunc func(string) (string, bool)

// Load reads and validates the relay configuration from the process
// environment.
func Load() (*Config, error) {
	return loadFrom(os.LookupEnv)
}

// loadFrom builds a Config using the given environment lookup function. It
// is the seam Load delegates to, kept unexported so tests can supply an
// in-memory environment instead of mutating the real one.
func loadFrom(getenv getenvFunc) (*Config, error) {
	var problems []string

	addProblem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	cfg := &Config{
		SMTPHost:             stringOrDefault(getenv, "SMTP_HOST", "0.0.0.0"),
		SMTPPort:             intOrDefault(getenv, "SMTP_PORT", 2525, addProblem),
		SMTPUser:             requireString(getenv, "SMTP_USER", addProblem),
		SMTPPassword:         requireString(getenv, "SMTP_PASSWORD", addProblem),
		SMTPMaxMessageBytes:  int64OrDefault(getenv, "SMTP_MAX_MESSAGE_BYTES", cloudflareMaxMessageBytes, addProblem),
		SMTPMaxRecipients:    intOrDefault(getenv, "SMTP_MAX_RECIPIENTS", cloudflareMaxRecipients, addProblem),
		SMTPReadTimeout:      durationOrDefault(getenv, "SMTP_READ_TIMEOUT", 30*time.Second, addProblem),
		SMTPWriteTimeout:     durationOrDefault(getenv, "SMTP_WRITE_TIMEOUT", 30*time.Second, addProblem),
		SMTPTLSCert:          stringOrDefault(getenv, "SMTP_TLS_CERT", ""),
		SMTPTLSKey:           stringOrDefault(getenv, "SMTP_TLS_KEY", ""),
		CloudflareTransport:  Transport(stringOrDefault(getenv, "CLOUDFLARE_TRANSPORT", string(TransportREST))),
		CloudflareAccountID:  stringOrDefault(getenv, "CLOUDFLARE_ACCOUNT_ID", ""),
		CloudflareAPIToken:   stringOrDefault(getenv, "CLOUDFLARE_API_TOKEN", ""),
		CloudflareAPIBaseURL: stringOrDefault(getenv, "CLOUDFLARE_API_BASE_URL", "https://api.cloudflare.com/client/v4"),
		CloudflareTimeout:    durationOrDefault(getenv, "CLOUDFLARE_TIMEOUT", 15*time.Second, addProblem),
		CloudflareMaxRetries: intOrDefault(getenv, "CLOUDFLARE_MAX_RETRIES", 2, addProblem),
		WorkerURL:            stringOrDefault(getenv, "WORKER_URL", ""),
		WorkerSecret:         stringOrDefault(getenv, "WORKER_SECRET", ""),
		AllowedFromDomains:   normalizeDomains(stringOrDefault(getenv, "ALLOWED_FROM_DOMAINS", "")),
		HealthHost:           stringOrDefault(getenv, "HEALTH_HOST", "0.0.0.0"),
		HealthPort:           intOrDefault(getenv, "HEALTH_PORT", 8080, addProblem),
	}

	validatePortRange(cfg.SMTPPort, "SMTP_PORT", addProblem)
	validatePortRange(cfg.HealthPort, "HEALTH_PORT", addProblem)

	if cfg.SMTPMaxMessageBytes <= 0 || cfg.SMTPMaxMessageBytes > cloudflareMaxMessageBytes {
		addProblem("SMTP_MAX_MESSAGE_BYTES: must be > 0 and <= %d (Cloudflare hard cap), got %d", cloudflareMaxMessageBytes, cfg.SMTPMaxMessageBytes)
	}
	if cfg.SMTPMaxRecipients <= 0 || cfg.SMTPMaxRecipients > cloudflareMaxRecipients {
		addProblem("SMTP_MAX_RECIPIENTS: must be > 0 and <= %d (Cloudflare cap), got %d", cloudflareMaxRecipients, cfg.SMTPMaxRecipients)
	}
	if cfg.CloudflareMaxRetries < 0 || cfg.CloudflareMaxRetries > 5 {
		addProblem("CLOUDFLARE_MAX_RETRIES: must be >= 0 and <= 5, got %d", cfg.CloudflareMaxRetries)
	}

	if (cfg.SMTPTLSCert == "") != (cfg.SMTPTLSKey == "") {
		if cfg.SMTPTLSCert == "" {
			addProblem("SMTP_TLS_CERT: required when SMTP_TLS_KEY is set")
		} else {
			addProblem("SMTP_TLS_KEY: required when SMTP_TLS_CERT is set")
		}
	}

	rawLogLevel := stringOrDefault(getenv, "LOG_LEVEL", "info")
	switch strings.ToLower(rawLogLevel) {
	case "debug", "info", "warn", "error":
		cfg.LogLevel = strings.ToLower(rawLogLevel)
	default:
		addProblem("LOG_LEVEL: must be one of debug, info, warn, error (case-insensitive), got %q", rawLogLevel)
	}

	switch cfg.CloudflareTransport {
	case TransportREST:
		if cfg.CloudflareAccountID == "" {
			addProblem("CLOUDFLARE_ACCOUNT_ID: required when CLOUDFLARE_TRANSPORT is %q", TransportREST)
		}
		if cfg.CloudflareAPIToken == "" {
			addProblem("CLOUDFLARE_API_TOKEN: required when CLOUDFLARE_TRANSPORT is %q", TransportREST)
		}
	case TransportWorker:
		if cfg.WorkerURL == "" {
			addProblem("WORKER_URL: required when CLOUDFLARE_TRANSPORT is %q", TransportWorker)
		} else {
			validateAbsoluteHTTPURL(cfg.WorkerURL, "WORKER_URL", addProblem)
		}
		if cfg.WorkerSecret == "" {
			addProblem("WORKER_SECRET: required when CLOUDFLARE_TRANSPORT is %q", TransportWorker)
		}
	default:
		addProblem("CLOUDFLARE_TRANSPORT: must be %q or %q, got %q", TransportREST, TransportWorker, cfg.CloudflareTransport)
	}

	if len(problems) > 0 {
		return nil, aggregateError(problems)
	}
	return cfg, nil
}

func stringOrDefault(getenv getenvFunc, key, def string) string {
	if v, ok := getenv(key); ok {
		return v
	}
	return def
}

func requireString(getenv getenvFunc, key string, addProblem func(string, ...any)) string {
	v, ok := getenv(key)
	if !ok || v == "" {
		addProblem("%s: required, no default", key)
		return ""
	}
	return v
}
