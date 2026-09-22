package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/DanielGS/cloudflare-smtp-relay/internal/email"
)

// WorkerConfig configures a Worker adapter that posts to a user-deployed
// Cloudflare Worker rather than to Cloudflare's own REST API directly.
type WorkerConfig struct {
	// URL is the Worker's HTTP endpoint.
	URL string
	// Secret authenticates the request as a Bearer token.
	Secret string
	// Timeout bounds a single attempt, not the whole retry loop.
	Timeout time.Duration
	// MaxRetries is how many additional attempts are made after the first.
	MaxRetries int
	// HTTPClient is optional; a default one built from Timeout is used when nil.
	HTTPClient *http.Client
	// Sleep is an optional seam for tests; defaults to a context-aware sleep.
	Sleep func(context.Context, time.Duration) error
}

// Worker is an email.Sender that posts messages to a user-deployed Cloudflare
// Worker, using the same REST-shaped payload as the REST adapter (see the doc
// comment on addressPayload for why).
type Worker struct {
	url        string
	secret     string
	timeout    time.Duration
	maxRetries int
	httpClient *http.Client
	sleep      func(context.Context, time.Duration) error
}

// NewWorker validates cfg and builds a Worker adapter.
func NewWorker(cfg WorkerConfig) (*Worker, error) {
	if cfg.URL == "" {
		return nil, errors.New("cloudflare: WorkerConfig.URL must not be empty")
	}
	if cfg.Secret == "" {
		return nil, errors.New("cloudflare: WorkerConfig.Secret must not be empty")
	}
	if cfg.MaxRetries < 0 {
		return nil, errors.New("cloudflare: WorkerConfig.MaxRetries must not be negative")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = defaultSleep
	}

	return &Worker{
		url:        cfg.URL,
		secret:     cfg.Secret,
		timeout:    timeout,
		maxRetries: cfg.MaxRetries,
		httpClient: httpClient,
		sleep:      sleep,
	}, nil
}

// Send implements email.Sender.
func (c *Worker) Send(ctx context.Context, msg *email.Message) (*email.Result, error) {
	payload, err := json.Marshal(buildPayload(msg))
	if err != nil {
		return nil, classifyTransport(fmt.Errorf("encode request: %w", err), 0)
	}

	build := func(ctx context.Context, body []byte) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.secret)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}

	return sendLoop(ctx, c.httpClient, c.timeout, c.maxRetries, c.sleep, time.Now, payload, build, parseWorkerResponse)
}

// workerResponse is the shape of both success and error Worker responses.
type workerResponse struct {
	Success   bool   `json:"success"`
	MessageID string `json:"messageId"`
	Code      string `json:"code"`
	Error     string `json:"error"`
}

// workerCodeBucket is the classification tied to one specific Worker string
// error code.
type workerCodeBucket struct {
	temporary bool
	retryable bool
	smtp      int
	enhanced  [3]int
	reason    string
}

// workerCodeBuckets maps the Worker's documented string error codes to their
// classification. Any code not in this table is classified from the HTTP
// status instead (see parseWorkerResponse).
var workerCodeBuckets = map[string]workerCodeBucket{
	"E_RATE_LIMIT_EXCEEDED":  {temporary: true, retryable: true, smtp: 451, enhanced: [3]int{4, 4, 5}, reason: "upstream rate limited"},
	"E_DAILY_LIMIT_EXCEEDED": {temporary: true, retryable: true, smtp: 451, enhanced: [3]int{4, 4, 5}, reason: "upstream rate limited"},

	"E_SENDER_NOT_VERIFIED":         {temporary: false, retryable: false, smtp: 550, enhanced: [3]int{5, 7, 1}, reason: "upstream rejected sender"},
	"E_SENDER_DOMAIN_NOT_AVAILABLE": {temporary: false, retryable: false, smtp: 550, enhanced: [3]int{5, 7, 1}, reason: "upstream rejected sender"},
	"E_RECIPIENT_NOT_ALLOWED":       {temporary: false, retryable: false, smtp: 550, enhanced: [3]int{5, 7, 1}, reason: "upstream rejected recipient"},
}

// parseWorkerResponse implements responseParser for a user-deployed Worker.
func parseWorkerResponse(status int, body []byte, attempts int) (*email.Result, *email.DeliveryError, bool) {
	var resp workerResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		de := classifyTransport(fmt.Errorf("decode response: %w", err), attempts)
		return nil, de, true
	}

	if resp.Success {
		return &email.Result{ProviderID: resp.MessageID, HTTPStatus: status, Attempts: attempts}, nil, false
	}

	de := classifyWorkerCode(status, resp.Code, resp.Error, attempts)
	retryable := isWorkerRetryable(status, resp.Code)
	return nil, de, retryable
}

// classifyWorkerCode maps a Worker's string error code (and, when the code is
// unrecognized, the HTTP status) to a classified *email.DeliveryError.
func classifyWorkerCode(status int, code, message string, attempts int) *email.DeliveryError {
	if b, ok := workerCodeBuckets[code]; ok {
		return newDeliveryError(b.temporary, b.smtp, b.enhanced, b.reason, status, 0, attempts, message)
	}
	return classifyHTTP(status, 0, message, attempts)
}

// isWorkerRetryable mirrors isRetryable for the Worker's string error codes.
func isWorkerRetryable(status int, code string) bool {
	if b, ok := workerCodeBuckets[code]; ok {
		return b.retryable
	}
	return isRetryable(status, 0)
}
