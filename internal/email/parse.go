package email

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	gomessage "github.com/emersion/go-message"
	emmail "github.com/emersion/go-message/mail"
)

// excludedHeaders lists the header keys, matched case-insensitively, that the
// provider rebuilds itself and therefore must not be copied into
// Message.Headers: address fields, Subject, MIME framing, and routing
// metadata added by intermediate relays.
var excludedHeaders = map[string]struct{}{
	"from":                      {},
	"to":                        {},
	"cc":                        {},
	"bcc":                       {},
	"subject":                   {},
	"message-id":                {},
	"reply-to":                  {},
	"date":                      {},
	"mime-version":              {},
	"content-type":              {},
	"content-transfer-encoding": {},
	"content-disposition":       {},
	"received":                  {},
	"return-path":               {},
}

// Parse reads an RFC 5322 message and combines it with the SMTP envelope into
// a Message ready for delivery.
//
// From is always built from envelopeFrom, since that is the address the relay
// authorized and the provider bills; a display name is adopted from the
// message's From header only when that header names the same address. To, Cc
// and Bcc are derived by matching envelopeRcpts against the To and Cc
// headers so that blind recipients (present only in the envelope) end up in
// Bcc. Attachments and other non-text parts are skipped without failing the
// parse. Parse does not set Message.ID; the caller assigns it on receipt.
func Parse(raw []byte, envelopeFrom string, envelopeRcpts []string) (*Message, error) {
	r, err := emmail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		if !gomessage.IsUnknownCharset(err) {
			return nil, fmt.Errorf("email: parse message: %w", err)
		}
	}
	defer func() { _ = r.Close() }()

	msg := &Message{
		Headers:    map[string]string{},
		Size:       int64(len(raw)),
		ReceivedAt: time.Now().UTC(),
	}

	if subject, serr := r.Header.Subject(); serr == nil {
		msg.Subject = subject
	} else {
		msg.Subject = r.Header.Get("Subject")
	}

	if id, ierr := r.Header.MessageID(); ierr == nil {
		msg.RFCMessageID = id
	}

	if replyTo, rerr := r.Header.Text("Reply-To"); rerr == nil {
		msg.ReplyTo = replyTo
	} else {
		msg.ReplyTo = r.Header.Get("Reply-To")
	}

	msg.From = resolveFrom(&r.Header, envelopeFrom)

	headerTo := headerAddresses(&r.Header, "To")
	headerCc := headerAddresses(&r.Header, "Cc")
	msg.To, msg.Cc, msg.Bcc = splitRecipients(headerTo, headerCc, envelopeRcpts)

	msg.Headers = extractHeaders(&r.Header)

	if err := readBodyParts(r, msg); err != nil {
		return nil, err
	}

	return msg, nil
}

// resolveFrom builds the delivery From address from the envelope sender,
// adopting the message header's display name only when the header address
// matches the envelope address. This prevents a client from spoofing the
// authorized sender through the message headers while still preserving a
// legitimate display name.
func resolveFrom(h *emmail.Header, envelopeFrom string) Address {
	from := Address{Address: strings.TrimSpace(envelopeFrom)}

	addrs, err := h.AddressList("From")
	if err != nil || len(addrs) == 0 {
		return from
	}

	headerFrom := addrs[0]
	if strings.EqualFold(headerFrom.Address, from.Address) {
		from.Name = headerFrom.Name
	}
	return from
}

// headerAddresses parses the address list for the given header key. A
// missing or malformed header yields an empty result rather than an error,
// since a malformed To/Cc header should not abort the whole parse.
func headerAddresses(h *emmail.Header, key string) []Address {
	addrs, err := h.AddressList(key)
	if err != nil || len(addrs) == 0 {
		return nil
	}

	out := make([]Address, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, Address{Name: a.Name, Address: a.Address})
	}
	return out
}

// splitRecipients derives the To, Cc and Bcc delivery lists from the message
// headers and the SMTP envelope recipients.
//
// To and Cc are the header addresses that also appear in envelopeRcpts. Bcc
// is every envelope recipient that appears in neither header. When the
// message has no To and no Cc header at all, every envelope recipient goes
// to To instead of Bcc, so that a plain single-recipient submission is not
// silently treated as entirely blind. Matching is case-insensitive on the
// whole address, and every address is deduplicated across the three lists.
func splitRecipients(headerTo, headerCc []Address, envelopeRcpts []string) (to, cc, bcc []Address) {
	envelopeSet := make(map[string]struct{}, len(envelopeRcpts))
	for _, r := range envelopeRcpts {
		key := strings.ToLower(strings.TrimSpace(r))
		if key != "" {
			envelopeSet[key] = struct{}{}
		}
	}

	assigned := make(map[string]struct{})

	if len(headerTo) == 0 && len(headerCc) == 0 {
		for _, r := range envelopeRcpts {
			addr := strings.TrimSpace(r)
			key := strings.ToLower(addr)
			if key == "" {
				continue
			}
			if _, dup := assigned[key]; dup {
				continue
			}
			assigned[key] = struct{}{}
			to = append(to, Address{Address: addr})
		}
		return to, cc, bcc
	}

	for _, a := range headerTo {
		key := strings.ToLower(a.Address)
		if key == "" {
			continue
		}
		if _, ok := envelopeSet[key]; !ok {
			continue
		}
		if _, dup := assigned[key]; dup {
			continue
		}
		assigned[key] = struct{}{}
		to = append(to, a)
	}

	for _, a := range headerCc {
		key := strings.ToLower(a.Address)
		if key == "" {
			continue
		}
		if _, ok := envelopeSet[key]; !ok {
			continue
		}
		if _, dup := assigned[key]; dup {
			continue
		}
		assigned[key] = struct{}{}
		cc = append(cc, a)
	}

	for _, r := range envelopeRcpts {
		addr := strings.TrimSpace(r)
		key := strings.ToLower(addr)
		if key == "" {
			continue
		}
		if _, dup := assigned[key]; dup {
			continue
		}
		assigned[key] = struct{}{}
		bcc = append(bcc, Address{Address: addr})
	}

	return to, cc, bcc
}

// extractHeaders copies every header field not in excludedHeaders into a map,
// decoding MIME-encoded words where possible.
func extractHeaders(h *emmail.Header) map[string]string {
	out := map[string]string{}

	fields := h.Fields()
	for fields.Next() {
		key := fields.Key()
		if _, excluded := excludedHeaders[strings.ToLower(key)]; excluded {
			continue
		}

		value, err := fields.Text()
		if err != nil {
			value = fields.Value()
		}
		out[key] = value
	}

	return out
}

// readBodyParts walks the MIME tree and fills msg.Text and msg.HTML from the
// text/plain and text/html parts. Non-text parts (attachments) are skipped.
func readBodyParts(r *emmail.Reader, msg *Message) error {
	for {
		part, err := r.NextPart()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if !gomessage.IsUnknownCharset(err) && !gomessage.IsUnknownEncoding(err) {
				return fmt.Errorf("email: parse message body: %w", err)
			}
		}
		if part == nil {
			continue
		}

		inline, ok := part.Header.(*emmail.InlineHeader)
		if !ok {
			// Attachment or other non-inline part: out of scope, skip it.
			continue
		}

		contentType, _, _ := inline.ContentType()
		body, rerr := io.ReadAll(part.Body)
		if rerr != nil {
			return fmt.Errorf("email: read message part: %w", rerr)
		}

		switch {
		case strings.HasPrefix(contentType, "text/plain"):
			msg.Text = string(body)
		case strings.HasPrefix(contentType, "text/html"):
			msg.HTML = string(body)
		}
	}
}
