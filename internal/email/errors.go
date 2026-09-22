package email

import (
	"errors"
	"fmt"
)

// DeliveryError is a provider failure that has already been classified as temporary
// or permanent, carrying the SMTP reply the client should receive.
//
// Adapters build it; the SMTP layer translates it into a wire response. Keeping the
// classification here means the decision lives next to the provider knowledge that
// justifies it, instead of being re-derived from a status code further up.
type DeliveryError struct {
	// Temporary reports whether retrying the same message could succeed later.
	Temporary bool
	// SMTPCode is the reply code to send to the client, such as 451 or 550.
	SMTPCode int
	// EnhancedCode is the RFC 3463 status, such as {4, 4, 1}.
	EnhancedCode [3]int
	// Reason is client-safe text. It must never embed credentials or message content.
	Reason string
	// HTTPStatus is the upstream status code, or 0 when the request never completed.
	HTTPStatus int
	// ProviderCode is the provider's own numeric error code, or 0 when absent.
	ProviderCode int
	// Attempts counts how many requests were issued before giving up.
	Attempts int
	// Err is the underlying cause, kept for logs and errors.Is/As.
	Err error
}

func (e *DeliveryError) Error() string {
	if e.HTTPStatus > 0 {
		return fmt.Sprintf("delivery failed (smtp %d, http %d): %s", e.SMTPCode, e.HTTPStatus, e.Reason)
	}
	return fmt.Sprintf("delivery failed (smtp %d): %s", e.SMTPCode, e.Reason)
}

func (e *DeliveryError) Unwrap() error { return e.Err }

// AsDeliveryError extracts a *DeliveryError from err, reporting whether one was found.
func AsDeliveryError(err error) (*DeliveryError, bool) {
	var de *DeliveryError
	ok := errors.As(err, &de)
	return de, ok
}

// ErrSenderNotAllowed is returned when the envelope sender falls outside the
// configured allowlist of sender domains.
var ErrSenderNotAllowed = errors.New("sender domain not allowed")
