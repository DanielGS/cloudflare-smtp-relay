// Package email holds the transport-agnostic representation of a submitted message
// together with the outbound port used to deliver it. Nothing in this package may
// import an SMTP or HTTP client: adapters depend on the domain, never the reverse.
package email

import (
	"context"
	"time"
)

// Address is an RFC 5322 address with an optional display name.
type Address struct {
	Name    string
	Address string
}

// Domain returns the lowercased part after the "@", or an empty string when the
// address has no domain part.
func (a Address) Domain() string {
	for i := len(a.Address) - 1; i >= 0; i-- {
		if a.Address[i] == '@' {
			return lowerASCII(a.Address[i+1:])
		}
	}
	return ""
}

// String renders the address in "Display Name <local@domain>" form, falling back
// to the bare address when no display name is set.
func (a Address) String() string {
	if a.Name == "" {
		return a.Address
	}
	return a.Name + " <" + a.Address + ">"
}

// Message is one submission, normalized from the SMTP envelope and the DATA payload.
//
// To, Cc and Bcc are the delivery lists the upstream provider is asked to use. They
// are derived from the envelope recipients combined with the message headers, so that
// blind recipients stay blind: an address that arrived only via RCPT TO, and appears
// in neither the To nor the Cc header, belongs in Bcc.
type Message struct {
	// ID is the relay's own identifier, generated on receipt and used in logs.
	ID string
	// RFCMessageID is the Message-ID header of the original message, when present.
	RFCMessageID string

	From    Address
	To      []Address
	Cc      []Address
	Bcc     []Address
	ReplyTo string

	Subject string
	Text    string
	HTML    string

	// Headers carries the preserved headers worth forwarding, excluding the ones
	// the provider rebuilds itself (From, To, Cc, Bcc, Subject, MIME framing).
	Headers map[string]string

	Size       int64
	ReceivedAt time.Time
}

// RecipientCount returns the combined number of To, Cc and Bcc addresses, which is
// what providers cap.
func (m *Message) RecipientCount() int {
	return len(m.To) + len(m.Cc) + len(m.Bcc)
}

// AllRecipients returns every delivery address in To, Cc and Bcc order.
func (m *Message) AllRecipients() []string {
	out := make([]string, 0, m.RecipientCount())
	for _, group := range [][]Address{m.To, m.Cc, m.Bcc} {
		for _, a := range group {
			out = append(out, a.Address)
		}
	}
	return out
}

// Result describes an accepted delivery.
type Result struct {
	// ProviderID is the upstream message identifier, when the provider returns one.
	ProviderID string
	// HTTPStatus is the upstream status code, or 0 for non-HTTP transports.
	HTTPStatus int
	// Attempts counts how many requests were issued, including the successful one.
	Attempts int
}

// Sender is the outbound port. Adapters implement it against a concrete provider.
//
// A nil error means the provider accepted the message. Any failure that a client
// should be able to act on must be returned as a *DeliveryError so the SMTP layer
// can reply with a correctly classified code.
type Sender interface {
	Send(ctx context.Context, msg *Message) (*Result, error)
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
