// Command relay accepts SMTP submission on a private network and delivers each
// message through the Cloudflare Email Service.
//
// It exists because Cloudflare's own SMTP endpoint requires a domain onboarded
// to Email Sending, while its HTTP surfaces can deliver to verified destination
// addresses without that. Applications keep speaking SMTP; the relay holds the
// credential and translates.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dagase/cloudflare-smtp-relay/internal/cloudflare"
	"github.com/dagase/cloudflare-smtp-relay/internal/config"
	"github.com/dagase/cloudflare-smtp-relay/internal/email"
	"github.com/dagase/cloudflare-smtp-relay/internal/health"
	"github.com/dagase/cloudflare-smtp-relay/internal/logging"
	"github.com/dagase/cloudflare-smtp-relay/internal/smtpserver"
)

// shutdownGrace bounds how long in-flight SMTP transactions may finish after a
// termination signal before the process exits anyway.
const shutdownGrace = 20 * time.Second

func main() {
	healthcheck := flag.Bool("healthcheck", false,
		"probe the local health endpoint and exit 0 when healthy; used by the container HEALTHCHECK")
	flag.Parse()

	if *healthcheck {
		if err := runHealthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, "healthcheck failed:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		// The logger may not exist yet when configuration itself failed, so
		// this last-resort report goes straight to stderr.
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

// healthcheckTimeout bounds the self-probe run by the container HEALTHCHECK.
const healthcheckTimeout = 5 * time.Second

// defaultHealthPort mirrors the documented default, used when configuration
// cannot be read during a probe.
const defaultHealthPort = 8080

// runHealthcheck probes the endpoint the process serves. The container image has
// no shell and no curl, so the binary probes itself.
//
// A configuration error here must not fail the probe: the question being asked
// is whether the running process answers, not whether this invocation's
// environment is complete. Fall back to the documented default port instead.
func runHealthcheck() error {
	port := defaultHealthPort
	if cfg, err := config.Load(); err == nil {
		port = cfg.HealthPort
	}

	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()

	return health.Check(ctx, fmt.Sprintf("http://127.0.0.1:%d/health", port))
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, err := logging.New(cfg.LogLevel, os.Stdout)
	if err != nil {
		return err
	}

	sender, err := buildSender(cfg)
	if err != nil {
		return fmt.Errorf("cloudflare transport: %w", err)
	}

	tlsConfig, err := buildTLS(cfg)
	if err != nil {
		return fmt.Errorf("smtp tls: %w", err)
	}

	smtpSrv, err := smtpserver.New(smtpserver.Options{
		Addr:            cfg.SMTPAddr(),
		Username:        cfg.SMTPUser,
		Password:        cfg.SMTPPassword,
		MaxMessageBytes: cfg.SMTPMaxMessageBytes,
		MaxRecipients:   cfg.SMTPMaxRecipients,
		ReadTimeout:     cfg.SMTPReadTimeout,
		WriteTimeout:    cfg.SMTPWriteTimeout,
		TLSConfig:       tlsConfig,
		Policy:          email.NewSenderPolicy(cfg.AllowedFromDomains),
		Sender:          sender,
		Logger:          logger,
	})
	if err != nil {
		return fmt.Errorf("smtp server: %w", err)
	}

	healthSrv := health.New(cfg.HealthAddr(), logger)

	// Bind both listeners before serving, so a port conflict fails startup
	// loudly instead of leaving the process half-up.
	smtpLn, err := net.Listen("tcp", cfg.SMTPAddr())
	if err != nil {
		return fmt.Errorf("listen smtp: %w", err)
	}
	healthLn, err := net.Listen("tcp", cfg.HealthAddr())
	if err != nil {
		_ = smtpLn.Close()
		return fmt.Errorf("listen health: %w", err)
	}

	// cfg redacts its own secrets, so logging it cannot leak the API token.
	logger.Info("relay starting",
		slog.String("smtp_addr", smtpLn.Addr().String()),
		slog.String("health_addr", healthLn.Addr().String()),
		slog.String("transport", string(cfg.CloudflareTransport)),
		slog.Int("allowed_from_domains", len(cfg.AllowedFromDomains)),
		slog.Bool("smtp_tls", tlsConfig != nil),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	serveErr := make(chan error, 2)
	go func() { serveErr <- smtpSrv.Serve(smtpLn) }()
	go func() { serveErr <- healthSrv.Serve(healthLn) }()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received", slog.Duration("grace", shutdownGrace))
	case err := <-serveErr:
		// A listener died on its own. Stop the other one and report why.
		if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			shutdown(smtpSrv, healthSrv, logger)
			return fmt.Errorf("serve: %w", err)
		}
	}

	shutdown(smtpSrv, healthSrv, logger)
	logger.Info("relay stopped")
	return nil
}

// shutdown drains both servers within the grace period. Draining the SMTP server
// first lets in-flight transactions finish before the health endpoint starts
// reporting the process as gone.
func shutdown(smtpSrv *smtpserver.Server, healthSrv *health.Server, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()

	if err := smtpSrv.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		logger.Warn("smtp shutdown", slog.String("error", err.Error()))
	}
	if err := healthSrv.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		logger.Warn("health shutdown", slog.String("error", err.Error()))
	}
}

// buildSender selects the outbound transport. REST talks to Cloudflare directly;
// worker posts to a user-deployed Worker holding the send_email binding, which is
// the free path when the Email Sending REST API is not available to the account.
func buildSender(cfg *config.Config) (email.Sender, error) {
	switch cfg.CloudflareTransport {
	case config.TransportREST:
		return cloudflare.NewREST(cloudflare.RESTConfig{
			BaseURL:    cfg.CloudflareAPIBaseURL,
			AccountID:  cfg.CloudflareAccountID,
			APIToken:   cfg.CloudflareAPIToken,
			Timeout:    cfg.CloudflareTimeout,
			MaxRetries: cfg.CloudflareMaxRetries,
		})
	case config.TransportWorker:
		return cloudflare.NewWorker(cloudflare.WorkerConfig{
			URL:        cfg.WorkerURL,
			Secret:     cfg.WorkerSecret,
			Timeout:    cfg.CloudflareTimeout,
			MaxRetries: cfg.CloudflareMaxRetries,
		})
	default:
		return nil, fmt.Errorf("unsupported transport %q", cfg.CloudflareTransport)
	}
}

// buildTLS returns nil when no certificate pair is configured, which is the
// intended setup for a plaintext port on a private Docker network.
func buildTLS(cfg *config.Config) (*tls.Config, error) {
	if cfg.SMTPTLSCert == "" || cfg.SMTPTLSKey == "" {
		return nil, nil
	}
	cert, err := tls.LoadX509KeyPair(cfg.SMTPTLSCert, cfg.SMTPTLSKey)
	if err != nil {
		return nil, fmt.Errorf("load key pair: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}
