package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dagase/cloudflare-smtp-relay/internal/email"
)

// RESTConfig configures a REST adapter talking directly to Cloudflare's
// Email Routing REST API.
type RESTConfig struct {
	// BaseURL is the API root, e.g. "https://api.cloudflare.com/client/v4".
	BaseURL string
	// AccountID is the Cloudflare account owning the Email Routing zone.
	AccountID string
	// APIToken authenticates the request as a Bearer token.
	APIToken string
	// Timeout bounds a single attempt, not the whole retry loop.
	Timeout time.Duration
	// MaxRetries is how many additional attempts are made after the first.
	MaxRetries int
	// HTTPClient is optional; a default one built from Timeout is used when nil.
	HTTPClient *http.Client
	// Now is an optional seam for tests; defaults to time.Now.
	Now func() time.Time
	// Sleep is an optional seam for tests; defaults to a context-aware sleep.
	Sleep func(context.Context, time.Duration) error
}

// REST is an email.Sender that posts messages to Cloudflare's Email Routing
// REST API.
type REST struct {
	baseURL    string
	accountID  string
	apiToken   string
	timeout    time.Duration
	maxRetries int
	httpClient *http.Client
	now        func() time.Time
	sleep      func(context.Context, time.Duration) error
}

// NewREST validates cfg and builds a REST adapter.
func NewREST(cfg RESTConfig) (*REST, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("cloudflare: RESTConfig.BaseURL must not be empty")
	}
	if cfg.AccountID == "" {
		return nil, errors.New("cloudflare: RESTConfig.AccountID must not be empty")
	}
	if cfg.APIToken == "" {
		return nil, errors.New("cloudflare: RESTConfig.APIToken must not be empty")
	}
	if cfg.MaxRetries < 0 {
		return nil, errors.New("cloudflare: RESTConfig.MaxRetries must not be negative")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = defaultSleep
	}

	return &REST{
		baseURL:    cfg.BaseURL,
		accountID:  cfg.AccountID,
		apiToken:   cfg.APIToken,
		timeout:    timeout,
		maxRetries: cfg.MaxRetries,
		httpClient: httpClient,
		now:        now,
		sleep:      sleep,
	}, nil
}

// Send implements email.Sender.
func (c *REST) Send(ctx context.Context, msg *email.Message) (*email.Result, error) {
	payload, err := json.Marshal(buildPayload(msg))
	if err != nil {
		return nil, classifyTransport(fmt.Errorf("encode request: %w", err), 0)
	}

	endpoint := c.baseURL + "/accounts/" + c.accountID + "/email/sending/send"

	build := func(ctx context.Context, body []byte) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
		req.Header.Set("Content-Type", "application/json")
		return req, nil
	}

	return sendLoop(ctx, c.httpClient, c.timeout, c.maxRetries, c.sleep, c.now, payload, build, parseRESTResponse)
}

// restError is one entry of a Cloudflare REST API error response.
type restError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// restResponse is the shape of both success and error REST API responses.
type restResponse struct {
	Success bool        `json:"success"`
	Errors  []restError `json:"errors"`
	Result  *struct {
		Delivered []string `json:"delivered"`
	} `json:"result"`
}

// parseRESTResponse implements responseParser for the Cloudflare REST API.
func parseRESTResponse(status int, body []byte, attempts int) (*email.Result, *email.DeliveryError, bool) {
	var resp restResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		de := classifyTransport(fmt.Errorf("decode response: %w", err), attempts)
		return nil, de, true
	}

	if resp.Success {
		providerID := ""
		if resp.Result != nil && len(resp.Result.Delivered) > 0 {
			providerID = resp.Result.Delivered[0]
		}
		return &email.Result{ProviderID: providerID, HTTPStatus: status, Attempts: attempts}, nil, false
	}

	code := 0
	message := ""
	if len(resp.Errors) > 0 {
		code = resp.Errors[0].Code
		message = resp.Errors[0].Message
	}
	de := classifyHTTP(status, code, message, attempts)
	return nil, de, isRetryable(status, code)
}

// defaultSleep is the default Sleep seam: a context-aware sleep.
func defaultSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
