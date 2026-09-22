// Package cloudflare implements outbound email.Sender adapters that hand a
// submitted message to Cloudflare Email Routing, either through Cloudflare's
// own REST API or through a user-deployed Cloudflare Worker.
package cloudflare

import (
	"context"
	"errors"
	"fmt"

	"github.com/DanielGS/cloudflare-smtp-relay/internal/email"
)

// codeBucket is the classification outcome tied to one specific Cloudflare
// numeric error code.
type codeBucket struct {
	temporary bool
	retryable bool
	smtp      int
	enhanced  [3]int
	reason    string
}

// codeBuckets maps Cloudflare's documented numeric error codes to their
// classification. See the doc comment on classifyHTTP for the rationale
// behind the 401/403 entries.
var codeBuckets = map[int]codeBucket{
	// Rate limited.
	10004: {temporary: true, retryable: true, smtp: 451, enhanced: [3]int{4, 4, 5}, reason: "upstream rate limited"},

	// Upstream server error.
	10002: {temporary: true, retryable: true, smtp: 451, enhanced: [3]int{4, 3, 0}, reason: "upstream server error"},
	10003: {temporary: true, retryable: true, smtp: 451, enhanced: [3]int{4, 3, 0}, reason: "upstream server error"},
	10100: {temporary: true, retryable: true, smtp: 451, enhanced: [3]int{4, 3, 0}, reason: "upstream server error"},

	// Relay credential rejected. Temporary, not retryable: the relay's own
	// token is wrong or unentitled, not the message. See classifyHTTP.
	10101: {temporary: true, retryable: false, smtp: 451, enhanced: [3]int{4, 7, 0}, reason: "relay credential rejected"},
	10103: {temporary: true, retryable: false, smtp: 451, enhanced: [3]int{4, 7, 0}, reason: "relay credential rejected"},

	// Relay not entitled to send. Same rationale as above.
	10102: {temporary: true, retryable: false, smtp: 451, enhanced: [3]int{4, 7, 0}, reason: "relay not entitled"},
	10105: {temporary: true, retryable: false, smtp: 451, enhanced: [3]int{4, 7, 0}, reason: "relay not entitled"},
	10203: {temporary: true, retryable: false, smtp: 451, enhanced: [3]int{4, 7, 0}, reason: "relay not entitled"},

	// Message rejected as invalid.
	10001: {temporary: false, retryable: false, smtp: 550, enhanced: [3]int{5, 6, 0}, reason: "upstream rejected message"},
	10200: {temporary: false, retryable: false, smtp: 550, enhanced: [3]int{5, 6, 0}, reason: "upstream rejected message"},
	10201: {temporary: false, retryable: false, smtp: 550, enhanced: [3]int{5, 6, 0}, reason: "upstream rejected message"},
	10202: {temporary: false, retryable: false, smtp: 550, enhanced: [3]int{5, 6, 0}, reason: "upstream rejected message"},

	// Account or resource not found.
	10000: {temporary: false, retryable: false, smtp: 550, enhanced: [3]int{5, 1, 2}, reason: "upstream resource not found"},
}

// classifyHTTP maps an upstream HTTP outcome to a classified *email.DeliveryError.
//
// status is the HTTP status code Cloudflare returned (or, for a "200 with
// success: false" body, the transport-level status, which is 200). providerCode
// is Cloudflare's own numeric error code from the response body, or 0 when
// absent. When providerCode is one of Cloudflare's documented codes, its
// classification wins over the generic status-based fallback.
//
// Rationale for 401/403 being Temporary=true: those statuses mean the relay's
// own credential is wrong or unentitled, not that the submitted message is
// bad. Replying with a permanent 5xx would make the client discard a message
// that was otherwise perfectly valid. Replying with a temporary 4xx makes the
// client retry, and the message goes out once the relay's credential is
// fixed. isRetryable still refuses to retry a 401/403 internally, since
// hammering a broken credential against the API cannot help within the same
// attempt loop.
func classifyHTTP(status int, providerCode int, providerMessage string, attempts int) *email.DeliveryError {
	if b, ok := codeBuckets[providerCode]; ok {
		return newDeliveryError(b.temporary, b.smtp, b.enhanced, b.reason, status, providerCode, attempts, providerMessage)
	}

	switch {
	case status == 429:
		return newDeliveryError(true, 451, [3]int{4, 4, 5}, "upstream rate limited", status, providerCode, attempts, providerMessage)
	case status == 500, status == 502, status == 503, status == 504:
		return newDeliveryError(true, 451, [3]int{4, 3, 0}, "upstream server error", status, providerCode, attempts, providerMessage)
	case status == 401:
		return newDeliveryError(true, 451, [3]int{4, 7, 0}, "relay credential rejected", status, providerCode, attempts, providerMessage)
	case status == 403:
		return newDeliveryError(true, 451, [3]int{4, 7, 0}, "relay not entitled", status, providerCode, attempts, providerMessage)
	case status == 400:
		return newDeliveryError(false, 550, [3]int{5, 6, 0}, "upstream rejected message", status, providerCode, attempts, providerMessage)
	case status == 404:
		return newDeliveryError(false, 550, [3]int{5, 1, 2}, "upstream resource not found", status, providerCode, attempts, providerMessage)
	case status >= 400 && status < 500:
		return newDeliveryError(false, 550, [3]int{5, 6, 0}, "upstream rejected message", status, providerCode, attempts, providerMessage)
	case status >= 500 && status < 600:
		return newDeliveryError(true, 451, [3]int{4, 3, 0}, "upstream server error", status, providerCode, attempts, providerMessage)
	default:
		// Covers "HTTP 200 but success: false" with an unrecognized embedded
		// error code: conservative permanent failure.
		return newDeliveryError(false, 550, [3]int{5, 6, 0}, "upstream rejected message", status, providerCode, attempts, providerMessage)
	}
}

// classifyTransport maps a transport-level failure (the request never
// completed, or its response could not be parsed) to a classified
// *email.DeliveryError. Every such failure is treated as temporary: a client
// or DNS hiccup, a connection refused, a deadline, or an unreadable body are
// all conditions that may clear up on retry.
func classifyTransport(err error, attempts int) *email.DeliveryError {
	reason := "upstream connection failed"
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "upstream request timed out"
	}
	return &email.DeliveryError{
		Temporary:    true,
		SMTPCode:     451,
		EnhancedCode: [3]int{4, 4, 1},
		Reason:       reason,
		HTTPStatus:   0,
		ProviderCode: 0,
		Attempts:     attempts,
		Err:          err,
	}
}

// isRetryable reports whether the adapter's own retry loop should issue
// another attempt for the given outcome. status is the HTTP status (0 when
// there was none, e.g. a transport failure or an embedded 200-body error);
// providerCode is Cloudflare's numeric error code when known, or 0.
//
// This is deliberately narrower than DeliveryError.Temporary: a 401/403 is
// reported to the client as temporary (see classifyHTTP), but the adapter
// must never retry it internally, since resending the same bad credential
// cannot succeed within the same attempt loop.
func isRetryable(status int, providerCode int) bool {
	if b, ok := codeBuckets[providerCode]; ok {
		return b.retryable
	}
	if status == 429 {
		return true
	}
	if status >= 500 && status < 600 {
		return true
	}
	return false
}

// newDeliveryError builds a *email.DeliveryError, keeping providerMessage out
// of the client-safe Reason and instead attaching it to Err for logs, since
// Reason must never carry upstream body content.
func newDeliveryError(temporary bool, smtp int, enhanced [3]int, reason string, status, providerCode, attempts int, providerMessage string) *email.DeliveryError {
	de := &email.DeliveryError{
		Temporary:    temporary,
		SMTPCode:     smtp,
		EnhancedCode: enhanced,
		Reason:       reason,
		HTTPStatus:   status,
		ProviderCode: providerCode,
		Attempts:     attempts,
	}
	if providerMessage != "" {
		de.Err = fmt.Errorf("provider message: %s", providerMessage)
	}
	return de
}
