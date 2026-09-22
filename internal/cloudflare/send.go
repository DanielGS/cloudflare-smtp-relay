package cloudflare

import (
	"context"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/DanielGS/cloudflare-smtp-relay/internal/email"
)

// maxResponseBodyBytes caps how much of an upstream response body is read,
// so a hostile or broken upstream cannot exhaust memory.
const maxResponseBodyBytes = 64 * 1024

const (
	retryBaseDelay = 500 * time.Millisecond
	retryCapDelay  = 5 * time.Second
)

// requestBuilder returns a fresh *http.Request for one attempt, given the
// per-attempt context and the already-marshaled JSON payload bytes. It must
// build a new request (and a new body reader) on every call, since an
// *http.Request's body cannot be replayed across attempts.
type requestBuilder func(ctx context.Context, payload []byte) (*http.Request, error)

// responseParser turns one HTTP response's status and fully-read body into
// either a success *email.Result or a classified *email.DeliveryError, plus
// whether the shared retry loop should retry this particular outcome.
type responseParser func(status int, body []byte, attempts int) (*email.Result, *email.DeliveryError, bool)

// sendLoop is the retry/backoff engine shared by the REST and Worker
// adapters. It issues one request per attempt through build, classifies the
// outcome through parse, and retries only retryable outcomes up to
// maxRetries extra attempts, using exponential backoff with full jitter
// (base 500ms, capped at 5s) and honoring an upstream Retry-After header on
// HTTP 429.
func sendLoop(
	ctx context.Context,
	client *http.Client,
	timeout time.Duration,
	maxRetries int,
	sleep func(context.Context, time.Duration) error,
	now func() time.Time,
	payload []byte,
	build requestBuilder,
	parse responseParser,
) (*email.Result, error) {
	var lastErr *email.DeliveryError

	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, classifyTransport(err, attempt-1)
		}

		result, de, retryable, retryAfter := doAttempt(ctx, client, timeout, payload, attempt, now, build, parse)
		if de == nil {
			return result, nil
		}
		lastErr = de

		if !shouldRetry(ctx, retryable, attempt, maxRetries) {
			return nil, de
		}

		delay := backoffDelay(attempt, retryAfter)
		if delay > retryCapDelay {
			// A deferred Retry-After that exceeds the cap is treated as
			// terminal: do not retry further, return the deferred error.
			return nil, de
		}
		if err := sleep(ctx, delay); err != nil {
			return nil, classifyTransport(err, attempt)
		}
	}
}

// doAttempt issues exactly one HTTP request and classifies its outcome. The
// returned time.Duration is the upstream Retry-After hint (0 when absent or
// not applicable), which only ever comes from an HTTP 429 response header.
func doAttempt(
	ctx context.Context,
	client *http.Client,
	timeout time.Duration,
	payload []byte,
	attempt int,
	now func() time.Time,
	build requestBuilder,
	parse responseParser,
) (*email.Result, *email.DeliveryError, bool, time.Duration) {
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := build(attemptCtx, payload)
	if err != nil {
		return nil, classifyTransport(err, attempt), true, 0
	}

	//nolint:bodyclose // readAndDrainBody closes resp.Body; bodyclose cannot
	// follow the close through a helper function.
	resp, err := client.Do(req)
	if err != nil {
		return nil, classifyTransport(err, attempt), true, 0
	}

	body, readErr := readAndDrainBody(resp.Body)
	if readErr != nil {
		return nil, classifyTransport(readErr, attempt), true, 0
	}

	result, de, retryable := parse(resp.StatusCode, body, attempt)

	var retryAfter time.Duration
	if de != nil && resp.StatusCode == http.StatusTooManyRequests {
		if d, ok := parseRetryAfter(resp.Header.Get("Retry-After"), now); ok {
			retryAfter = d
		}
	}
	return result, de, retryable, retryAfter
}

// readAndDrainBody reads up to maxResponseBodyBytes from r, then drains and
// closes it so the underlying connection can be reused.
func readAndDrainBody(r io.ReadCloser) ([]byte, error) {
	defer func() { _ = r.Close() }()
	limited := io.LimitReader(r, maxResponseBodyBytes)
	body, err := io.ReadAll(limited)
	io.Copy(io.Discard, r) //nolint:errcheck // best-effort drain
	return body, err
}

// shouldRetry reports whether the shared loop should issue another attempt.
func shouldRetry(ctx context.Context, retryable bool, attempt, maxRetries int) bool {
	return retryable && attempt <= maxRetries && ctx.Err() == nil
}

// backoffDelay computes the delay before the next attempt. When retryAfter is
// set (from an upstream Retry-After header) it is honored directly; otherwise
// exponential backoff with full jitter is used, base 500ms, capped at 5s.
func backoffDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	maxDelay := retryBaseDelay * time.Duration(1<<uint(attempt-1))
	if maxDelay > retryCapDelay || maxDelay <= 0 {
		maxDelay = retryCapDelay
	}
	//nolint:gosec // G404: retry jitter only needs to desynchronize clients,
	// not to be unpredictable. crypto/rand would be wasteful here.
	return time.Duration(rand.Int63n(int64(maxDelay) + 1))
}

// parseRetryAfter parses a Retry-After header value in either the
// delta-seconds form ("120") or the HTTP-date form, returning the duration to
// wait and whether a value was present and parseable.
func parseRetryAfter(value string, now func() time.Time) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(value); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(value); err == nil {
		d := t.Sub(now())
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}
