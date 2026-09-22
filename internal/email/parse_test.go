package email

import (
	"strings"
	"testing"
)

func crlf(s string) []byte {
	return []byte(strings.ReplaceAll(s, "\n", "\r\n"))
}

func TestParse_BarePlainText(t *testing.T) {
	raw := crlf(`From: Alice <alice@mydomainexample.com>
To: Bob <bob@example.com>
Subject: Hello there
Message-ID: <abc123@mydomainexample.com>
Date: Mon, 02 Jan 2006 15:04:05 -0700

Hello, this is the body.
`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if msg.Subject != "Hello there" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "Hello there")
	}
	if msg.RFCMessageID != "abc123@mydomainexample.com" {
		t.Errorf("RFCMessageID = %q, want %q", msg.RFCMessageID, "abc123@mydomainexample.com")
	}
	if !strings.Contains(msg.Text, "Hello, this is the body.") {
		t.Errorf("Text = %q, want it to contain the body", msg.Text)
	}
	if msg.HTML != "" {
		t.Errorf("HTML = %q, want empty", msg.HTML)
	}
	if msg.Size != int64(len(raw)) {
		t.Errorf("Size = %d, want %d", msg.Size, len(raw))
	}
	if msg.ReceivedAt.IsZero() {
		t.Error("ReceivedAt is zero, want it set")
	}
	if msg.ReceivedAt.Location().String() != "UTC" {
		t.Errorf("ReceivedAt location = %v, want UTC", msg.ReceivedAt.Location())
	}
	if msg.ID != "" {
		t.Errorf("ID = %q, want empty (caller assigns it)", msg.ID)
	}
}

func TestParse_BareHTML(t *testing.T) {
	raw := crlf(`From: alice@mydomainexample.com
To: bob@example.com
Subject: HTML only
Content-Type: text/html; charset=utf-8

<p>Hello HTML</p>
`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if msg.Text != "" {
		t.Errorf("Text = %q, want empty", msg.Text)
	}
	if !strings.Contains(msg.HTML, "<p>Hello HTML</p>") {
		t.Errorf("HTML = %q, want it to contain the markup", msg.HTML)
	}
}

func TestParse_MultipartAlternative(t *testing.T) {
	raw := crlf(`From: alice@mydomainexample.com
To: bob@example.com
Subject: Alt
Content-Type: multipart/alternative; boundary="BOUND1"

--BOUND1
Content-Type: text/plain; charset=utf-8

Plain version.
--BOUND1
Content-Type: text/html; charset=utf-8

<p>HTML version.</p>
--BOUND1--
`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !strings.Contains(msg.Text, "Plain version.") {
		t.Errorf("Text = %q, want it to contain the plain part", msg.Text)
	}
	if !strings.Contains(msg.HTML, "<p>HTML version.</p>") {
		t.Errorf("HTML = %q, want it to contain the html part", msg.HTML)
	}
}

func TestParse_MultipartMixedWrappingAlternative(t *testing.T) {
	raw := crlf(`From: alice@mydomainexample.com
To: bob@example.com
Subject: Mixed
Content-Type: multipart/mixed; boundary="OUTER"

--OUTER
Content-Type: multipart/alternative; boundary="INNER"

--INNER
Content-Type: text/plain; charset=utf-8

Plain in mixed.
--INNER
Content-Type: text/html; charset=utf-8

<p>HTML in mixed.</p>
--INNER--
--OUTER
Content-Type: application/pdf; name="report.pdf"
Content-Disposition: attachment; filename="report.pdf"
Content-Transfer-Encoding: base64

JVBERi0xLjQK
--OUTER--
`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !strings.Contains(msg.Text, "Plain in mixed.") {
		t.Errorf("Text = %q, want it to contain the plain part", msg.Text)
	}
	if !strings.Contains(msg.HTML, "<p>HTML in mixed.</p>") {
		t.Errorf("HTML = %q, want it to contain the html part", msg.HTML)
	}
}

func TestParse_EmptyBodyIsNotAnError(t *testing.T) {
	raw := crlf(`From: alice@mydomainexample.com
To: bob@example.com
Subject: No body

`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil for a headers-only message", err)
	}
	if msg.Text != "" {
		t.Errorf("Text = %q, want empty", msg.Text)
	}
	if msg.HTML != "" {
		t.Errorf("HTML = %q, want empty", msg.HTML)
	}
}

func TestParse_UnparseableInputReturnsError(t *testing.T) {
	// A message with no header/body separation and an unterminated header line
	// is not a parseable RFC 5322 message.
	raw := []byte("this is not a mime message at all, no headers, no body separator")

	_, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com"})
	if err == nil {
		t.Fatal("Parse() error = nil, want an error for unparseable input")
	}
}

func TestParse_SubjectIsMIMEWordDecoded(t *testing.T) {
	raw := crlf(`From: alice@mydomainexample.com
To: bob@example.com
Subject: =?UTF-8?B?SG9sYSBtdW5kbw==?=

Body.
`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if msg.Subject != "Hola mundo" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "Hola mundo")
	}
}

func TestParse_ReplyTo(t *testing.T) {
	raw := crlf(`From: alice@mydomainexample.com
To: bob@example.com
Subject: Reply-To test
Reply-To: support@mydomainexample.com

Body.
`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !strings.Contains(msg.ReplyTo, "support@mydomainexample.com") {
		t.Errorf("ReplyTo = %q, want it to contain %q", msg.ReplyTo, "support@mydomainexample.com")
	}
}

func TestParse_HeadersExcludesRebuiltAndFramingHeaders(t *testing.T) {
	raw := crlf(`From: alice@mydomainexample.com
To: bob@example.com
Cc: carol@example.com
Subject: Headers test
Message-ID: <keep-me-out@mydomainexample.com>
Reply-To: support@mydomainexample.com
Date: Mon, 02 Jan 2006 15:04:05 -0700
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8
Content-Transfer-Encoding: 7bit
Content-Disposition: inline
Received: from mx.example.com by relay.example.com
Return-Path: <alice@mydomainexample.com>
X-Custom-Header: custom-value
In-Reply-To: <parent@mydomainexample.com>
References: <parent@mydomainexample.com>
List-Unsubscribe: <mailto:unsub@mydomainexample.com>

Body.
`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com", "carol@example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	excluded := []string{
		"From", "To", "Cc", "Bcc", "Subject", "Message-Id", "Reply-To", "Date",
		"Mime-Version", "Content-Type", "Content-Transfer-Encoding",
		"Content-Disposition", "Received", "Return-Path",
	}
	for _, key := range excluded {
		if _, ok := msg.Headers[key]; ok {
			t.Errorf("Headers contains excluded key %q", key)
		}
	}

	kept := map[string]string{
		"X-Custom-Header":  "custom-value",
		"In-Reply-To":      "<parent@mydomainexample.com>",
		"References":       "<parent@mydomainexample.com>",
		"List-Unsubscribe": "<mailto:unsub@mydomainexample.com>",
	}
	for key, want := range kept {
		got, ok := msg.Headers[key]
		if !ok {
			t.Errorf("Headers missing kept key %q", key)
			continue
		}
		if got != want {
			t.Errorf("Headers[%q] = %q, want %q", key, got, want)
		}
	}
}

func TestParse_FromUsesEnvelopeAddressAndAdoptsHeaderDisplayName(t *testing.T) {
	raw := crlf(`From: Alice Smith <alice@mydomainexample.com>
To: bob@example.com
Subject: From precedence

Body.
`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if msg.From.Address != "alice@mydomainexample.com" {
		t.Errorf("From.Address = %q, want %q", msg.From.Address, "alice@mydomainexample.com")
	}
	if msg.From.Name != "Alice Smith" {
		t.Errorf("From.Name = %q, want %q", msg.From.Name, "Alice Smith")
	}
}

func TestParse_FromEnvelopeWinsWhenHeaderAddressDiffers(t *testing.T) {
	raw := crlf(`From: Spoofed Name <someone-else@attacker.example>
To: bob@example.com
Subject: From precedence mismatch

Body.
`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if msg.From.Address != "alice@mydomainexample.com" {
		t.Errorf("From.Address = %q, want the envelope address %q", msg.From.Address, "alice@mydomainexample.com")
	}
	if msg.From.Name == "Spoofed Name" {
		t.Errorf("From.Name = %q, must not adopt the mismatched header display name", msg.From.Name)
	}
}

func TestParse_RecipientSplit_ToAndCcFromHeaders(t *testing.T) {
	raw := crlf(`From: alice@mydomainexample.com
To: Bob <bob@example.com>
Cc: Carol <carol@example.com>
Subject: Split test

Body.
`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com", "carol@example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(msg.To) != 1 || msg.To[0].Address != "bob@example.com" {
		t.Errorf("To = %+v, want [bob@example.com]", msg.To)
	}
	if len(msg.Cc) != 1 || msg.Cc[0].Address != "carol@example.com" {
		t.Errorf("Cc = %+v, want [carol@example.com]", msg.Cc)
	}
	if len(msg.Bcc) != 0 {
		t.Errorf("Bcc = %+v, want empty", msg.Bcc)
	}
}

func TestParse_RecipientSplit_BlindRecipientLandsInBccOnly(t *testing.T) {
	raw := crlf(`From: alice@mydomainexample.com
To: Bob <bob@example.com>
Cc: Carol <carol@example.com>
Subject: BCC test

Body.
`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{
		"bob@example.com", "carol@example.com", "dave-blind@example.com",
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(msg.Bcc) != 1 || msg.Bcc[0].Address != "dave-blind@example.com" {
		t.Fatalf("Bcc = %+v, want [dave-blind@example.com]", msg.Bcc)
	}

	for _, a := range msg.To {
		if strings.EqualFold(a.Address, "dave-blind@example.com") {
			t.Error("blind recipient must not appear in To")
		}
	}
	for _, a := range msg.Cc {
		if strings.EqualFold(a.Address, "dave-blind@example.com") {
			t.Error("blind recipient must not appear in Cc")
		}
	}
}

func TestParse_RecipientSplit_NoToOrCcHeaderFallsBackToTo(t *testing.T) {
	raw := crlf(`From: alice@mydomainexample.com
Subject: No recipient headers at all

Body.
`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com", "carol@example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(msg.To) != 2 {
		t.Fatalf("To = %+v, want both envelope recipients when no To/Cc header is present", msg.To)
	}
	if len(msg.Cc) != 0 || len(msg.Bcc) != 0 {
		t.Errorf("Cc = %+v, Bcc = %+v, want both empty in the fallback case", msg.Cc, msg.Bcc)
	}
}

func TestParse_RecipientSplit_Deduplicated(t *testing.T) {
	raw := crlf(`From: alice@mydomainexample.com
To: Bob <bob@example.com>
Subject: Dedup test

Body.
`)

	// The envelope repeats the same recipient (case-varied); it must not be
	// duplicated across the delivery lists.
	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com", "Bob@Example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if msg.RecipientCount() != 1 {
		t.Fatalf("RecipientCount() = %d, want 1 (deduplicated)", msg.RecipientCount())
	}
}

func TestParse_RecipientSplit_CaseInsensitiveMatch(t *testing.T) {
	raw := crlf(`From: alice@mydomainexample.com
To: Bob <Bob@Example.com>
Subject: Case test

Body.
`)

	msg, err := Parse(raw, "alice@mydomainexample.com", []string{"bob@example.com"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(msg.To) != 1 {
		t.Fatalf("To = %+v, want a single case-insensitive match", msg.To)
	}
	if len(msg.Bcc) != 0 {
		t.Errorf("Bcc = %+v, want empty; the recipient matched To case-insensitively", msg.Bcc)
	}
}
