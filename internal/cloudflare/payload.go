package cloudflare

import "github.com/dagase/cloudflare-smtp-relay/internal/email"

// addressPayload is one sender/recipient entry on the wire.
//
// Cloudflare's own REST API and its Workers Email Bindings API disagree on
// this key: the REST API uses "address" (snake_case throughout, e.g.
// "reply_to"), while the Workers binding uses "email" (camelCase, e.g.
// "replyTo"). This relay always talks to Cloudflare over the REST API, and a
// user-deployed Worker receives the exact same REST-shaped JSON from this
// relay; if that Worker in turn calls the Workers binding, translating
// "address" to the binding's "email" key is the Worker's own responsibility,
// not this relay's. Keeping one shared shape means there is only one payload
// to reason about.
type addressPayload struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
}

// sendPayload is the shared REST-shaped request body posted to both the
// Cloudflare REST API and a user-deployed Worker.
type sendPayload struct {
	From    addressPayload    `json:"from"`
	To      []addressPayload  `json:"to"`
	Cc      []addressPayload  `json:"cc,omitempty"`
	Bcc     []addressPayload  `json:"bcc,omitempty"`
	ReplyTo string            `json:"reply_to,omitempty"`
	Subject string            `json:"subject"`
	Text    string            `json:"text,omitempty"`
	HTML    string            `json:"html,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// buildPayload converts a domain email.Message into the wire payload shared
// by both adapters. Cc, Bcc and Headers are left nil (not empty slices/maps)
// when the message carries none, so "omitempty" drops them entirely instead
// of emitting an empty array or object.
func buildPayload(msg *email.Message) *sendPayload {
	p := &sendPayload{
		From:    toAddressPayload(msg.From),
		To:      toAddressPayloads(msg.To),
		ReplyTo: msg.ReplyTo,
		Subject: msg.Subject,
		Text:    msg.Text,
		HTML:    msg.HTML,
	}
	if len(msg.Cc) > 0 {
		p.Cc = toAddressPayloads(msg.Cc)
	}
	if len(msg.Bcc) > 0 {
		p.Bcc = toAddressPayloads(msg.Bcc)
	}
	if len(msg.Headers) > 0 {
		p.Headers = msg.Headers
	}
	return p
}

func toAddressPayload(a email.Address) addressPayload {
	return addressPayload{Address: a.Address, Name: a.Name}
}

func toAddressPayloads(addrs []email.Address) []addressPayload {
	out := make([]addressPayload, len(addrs))
	for i, a := range addrs {
		out[i] = toAddressPayload(a)
	}
	return out
}
