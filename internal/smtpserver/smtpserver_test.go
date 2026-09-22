package smtpserver_test

import (
	"strings"
	"testing"

	"github.com/emersion/go-sasl"

	"github.com/DanielGS/cloudflare-smtp-relay/internal/email"
	"github.com/DanielGS/cloudflare-smtp-relay/internal/smtpserver"
)

// 1. Successful authentication with PLAIN, then a full valid transaction
// returning 250.
func TestAuthPlainThenFullTransaction(t *testing.T) {
	ts := newTestServer(t, nil)
	client := ts.dial()
	defer client.Close()

	if err := authenticate(t, client, sasl.Plain, testUsername, testPassword); err != nil {
		t.Fatalf("PLAIN auth: %v", err)
	}

	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt("recipient@example.com", nil); err != nil {
		t.Fatalf("RCPT TO: %v", err)
	}

	wc, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	msg := rawMessage("sender@example.com", "recipient@example.com", "Hello", "Hi there")
	if _, err := wc.Write([]byte(msg)); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close DATA: %v", err)
	}

	if got := len(ts.sender.calls()); got != 1 {
		t.Fatalf("sender received %d messages, want 1", got)
	}
}

// 2. Successful authentication with LOGIN.
func TestAuthLoginThenFullTransaction(t *testing.T) {
	ts := newTestServer(t, nil)
	client := ts.dial()
	defer client.Close()

	if err := authenticate(t, client, sasl.Login, testUsername, testPassword); err != nil {
		t.Fatalf("LOGIN auth: %v", err)
	}

	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt("recipient@example.com", nil); err != nil {
		t.Fatalf("RCPT TO: %v", err)
	}

	wc, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	msg := rawMessage("sender@example.com", "recipient@example.com", "Hello", "Hi there")
	if _, err := wc.Write([]byte(msg)); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close DATA: %v", err)
	}

	if got := len(ts.sender.calls()); got != 1 {
		t.Fatalf("sender received %d messages, want 1", got)
	}
}

// 3. Wrong password rejected with 535; wrong username rejected with 535.
func TestAuthWrongCredentialsRejected(t *testing.T) {
	t.Run("wrong password", func(t *testing.T) {
		ts := newTestServer(t, nil)
		client := ts.dial()
		defer client.Close()

		err := authenticate(t, client, sasl.Plain, testUsername, "not-the-password")
		if code := smtpErrorCode(t, err); code != 535 {
			t.Fatalf("auth error code = %d, want 535", code)
		}
	})

	t.Run("wrong username", func(t *testing.T) {
		ts := newTestServer(t, nil)
		client := ts.dial()
		defer client.Close()

		err := authenticate(t, client, sasl.Plain, "not-the-user", testPassword)
		if code := smtpErrorCode(t, err); code != 535 {
			t.Fatalf("auth error code = %d, want 535", code)
		}
	})
}

// 4. MAIL FROM before AUTH rejected with 530.
func TestMailFromBeforeAuthRejected(t *testing.T) {
	ts := newTestServer(t, nil)
	client := ts.dial()
	defer client.Close()

	err := client.Mail("sender@example.com", nil)
	if code := smtpErrorCode(t, err); code != 530 {
		t.Fatalf("MAIL FROM error code = %d, want 530", code)
	}
}

// 5. Valid message end to end: assert the fake Sender received the
// expected From, recipients, Subject, Text and a non-empty Message.ID.
func TestValidMessageReachesSenderWithExpectedFields(t *testing.T) {
	ts := newTestServer(t, nil)
	client := ts.dial()
	defer client.Close()

	if err := authenticate(t, client, sasl.Plain, testUsername, testPassword); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt("recipient@example.com", nil); err != nil {
		t.Fatalf("RCPT TO: %v", err)
	}

	wc, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	body := "This is the plain text body."
	msg := rawMessage("sender@example.com", "recipient@example.com", "Test Subject", body)
	if _, err := wc.Write([]byte(msg)); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close DATA: %v", err)
	}

	calls := ts.sender.calls()
	if len(calls) != 1 {
		t.Fatalf("sender received %d messages, want 1", len(calls))
	}
	got := calls[0]

	if got.From.Address != "sender@example.com" {
		t.Errorf("From.Address = %q, want %q", got.From.Address, "sender@example.com")
	}
	if got.Subject != "Test Subject" {
		t.Errorf("Subject = %q, want %q", got.Subject, "Test Subject")
	}
	// SMTP DATA always terminates the final body line with CRLF before the
	// closing dot, so the parsed body carries that trailing line
	// terminator; trim it before comparing against the literal we sent.
	if strings.TrimRight(got.Text, "\r\n") != body {
		t.Errorf("Text = %q, want %q", got.Text, body)
	}
	if got.ID == "" {
		t.Error("Message.ID is empty, want a generated identifier")
	}

	found := false
	for _, addr := range got.AllRecipients() {
		if addr == "recipient@example.com" {
			found = true
		}
	}
	if !found {
		t.Errorf("recipients %v do not contain %q", got.AllRecipients(), "recipient@example.com")
	}
}

// 6. Multiple recipients across RCPT TO reach the Sender.
func TestMultipleRecipientsReachSender(t *testing.T) {
	ts := newTestServer(t, nil)
	client := ts.dial()
	defer client.Close()

	if err := authenticate(t, client, sasl.Plain, testUsername, testPassword); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}

	recipients := []string{"r1@example.com", "r2@example.com", "r3@example.com"}
	for _, r := range recipients {
		if err := client.Rcpt(r, nil); err != nil {
			t.Fatalf("RCPT TO %s: %v", r, err)
		}
	}

	wc, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	msg := rawMessage("sender@example.com", strings.Join(recipients, ", "), "Multi", "Body")
	if _, err := wc.Write([]byte(msg)); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close DATA: %v", err)
	}

	calls := ts.sender.calls()
	if len(calls) != 1 {
		t.Fatalf("sender received %d messages, want 1", len(calls))
	}
	got := calls[0].AllRecipients()
	for _, r := range recipients {
		found := false
		for _, addr := range got {
			if addr == r {
				found = true
			}
		}
		if !found {
			t.Errorf("recipients %v do not contain %q", got, r)
		}
	}
}

// 7. HTML message body reaches the Sender in Message.HTML.
func TestHTMLBodyReachesSender(t *testing.T) {
	ts := newTestServer(t, nil)
	client := ts.dial()
	defer client.Close()

	if err := authenticate(t, client, sasl.Plain, testUsername, testPassword); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt("recipient@example.com", nil); err != nil {
		t.Fatalf("RCPT TO: %v", err)
	}

	wc, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	html := "<p>Hello HTML</p>"
	msg := rawHTMLMessage("sender@example.com", "recipient@example.com", "HTML Test", html)
	if _, err := wc.Write([]byte(msg)); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close DATA: %v", err)
	}

	calls := ts.sender.calls()
	if len(calls) != 1 {
		t.Fatalf("sender received %d messages, want 1", len(calls))
	}
	// See the trailing-CRLF note in TestValidMessageReachesSenderWithExpectedFields.
	if strings.TrimRight(calls[0].HTML, "\r\n") != html {
		t.Errorf("HTML = %q, want %q", calls[0].HTML, html)
	}
}

// 8. Plain text message body reaches Message.Text.
func TestPlainTextBodyReachesSender(t *testing.T) {
	ts := newTestServer(t, nil)
	client := ts.dial()
	defer client.Close()

	if err := authenticate(t, client, sasl.Plain, testUsername, testPassword); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt("recipient@example.com", nil); err != nil {
		t.Fatalf("RCPT TO: %v", err)
	}

	wc, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	body := "Plain text only."
	msg := rawMessage("sender@example.com", "recipient@example.com", "Text Test", body)
	if _, err := wc.Write([]byte(msg)); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close DATA: %v", err)
	}

	calls := ts.sender.calls()
	if len(calls) != 1 {
		t.Fatalf("sender received %d messages, want 1", len(calls))
	}
	// See the trailing-CRLF note in TestValidMessageReachesSenderWithExpectedFields.
	if strings.TrimRight(calls[0].Text, "\r\n") != body {
		t.Errorf("Text = %q, want %q", calls[0].Text, body)
	}
}

// 9. Disallowed sender domain rejected at MAIL FROM with 550.
func TestDisallowedSenderDomainRejected(t *testing.T) {
	ts := newTestServer(t, func(o *smtpserver.Options) {
		o.Policy = email.NewSenderPolicy([]string{"allowed.example"})
	})
	client := ts.dial()
	defer client.Close()

	if err := authenticate(t, client, sasl.Plain, testUsername, testPassword); err != nil {
		t.Fatalf("auth: %v", err)
	}

	err := client.Mail("sender@not-allowed.example", nil)
	if code := smtpErrorCode(t, err); code != 550 {
		t.Fatalf("MAIL FROM error code = %d, want 550", code)
	}
}

// 10. Oversized message rejected with 552.
func TestOversizedMessageRejected(t *testing.T) {
	ts := newTestServer(t, func(o *smtpserver.Options) {
		o.MaxMessageBytes = 32
	})
	client := ts.dial()
	defer client.Close()

	if err := authenticate(t, client, sasl.Plain, testUsername, testPassword); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt("recipient@example.com", nil); err != nil {
		t.Fatalf("RCPT TO: %v", err)
	}

	wc, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	msg := rawMessage("sender@example.com", "recipient@example.com", "Too Big", strings.Repeat("x", 1024))
	// The write itself may or may not fail depending on buffering; the
	// authoritative check is the error returned by Close.
	_, _ = wc.Write([]byte(msg))
	err = wc.Close()

	if code := smtpErrorCode(t, err); code != 552 {
		t.Fatalf("DATA error code = %d, want 552", code)
	}
}

// 11. Sender returning a temporary *email.DeliveryError produces a 451 reply.
func TestTemporaryDeliveryErrorProduces451(t *testing.T) {
	ts := newTestServer(t, nil)
	ts.sender.err = &email.DeliveryError{
		Temporary:    true,
		SMTPCode:     451,
		EnhancedCode: [3]int{4, 4, 1},
		Reason:       "upstream temporarily unavailable",
	}

	client := ts.dial()
	defer client.Close()

	if err := authenticate(t, client, sasl.Plain, testUsername, testPassword); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt("recipient@example.com", nil); err != nil {
		t.Fatalf("RCPT TO: %v", err)
	}

	wc, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	msg := rawMessage("sender@example.com", "recipient@example.com", "Temp Fail", "body")
	if _, err := wc.Write([]byte(msg)); err != nil {
		t.Fatalf("write body: %v", err)
	}
	err = wc.Close()

	if code := smtpErrorCode(t, err); code != 451 {
		t.Fatalf("DATA error code = %d, want 451", code)
	}
}

// 12. Sender returning a permanent *email.DeliveryError produces a 550 reply.
func TestPermanentDeliveryErrorProduces550(t *testing.T) {
	ts := newTestServer(t, nil)
	ts.sender.err = &email.DeliveryError{
		Temporary:    false,
		SMTPCode:     550,
		EnhancedCode: [3]int{5, 1, 1},
		Reason:       "mailbox unavailable",
	}

	client := ts.dial()
	defer client.Close()

	if err := authenticate(t, client, sasl.Plain, testUsername, testPassword); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt("recipient@example.com", nil); err != nil {
		t.Fatalf("RCPT TO: %v", err)
	}

	wc, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	msg := rawMessage("sender@example.com", "recipient@example.com", "Perm Fail", "body")
	if _, err := wc.Write([]byte(msg)); err != nil {
		t.Fatalf("write body: %v", err)
	}
	err = wc.Close()

	if code := smtpErrorCode(t, err); code != 550 {
		t.Fatalf("DATA error code = %d, want 550", code)
	}
}

// 13. RSET clears envelope state: after MAIL FROM + RSET, a bare RCPT TO
// must fail.
func TestRSETClearsEnvelopeState(t *testing.T) {
	ts := newTestServer(t, nil)
	client := ts.dial()
	defer client.Close()

	if err := authenticate(t, client, sasl.Plain, testUsername, testPassword); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}
	if err := client.Reset(); err != nil {
		t.Fatalf("RSET: %v", err)
	}

	err := client.Rcpt("recipient@example.com", nil)
	if err == nil {
		t.Fatal("RCPT TO after RSET succeeded, want an error")
	}
}

// 14. A second transaction on the same connection does not inherit the
// first one's recipients.
func TestSecondTransactionDoesNotInheritRecipients(t *testing.T) {
	ts := newTestServer(t, nil)
	client := ts.dial()
	defer client.Close()

	if err := authenticate(t, client, sasl.Plain, testUsername, testPassword); err != nil {
		t.Fatalf("auth: %v", err)
	}

	// First transaction, two recipients.
	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("first MAIL FROM: %v", err)
	}
	if err := client.Rcpt("first-a@example.com", nil); err != nil {
		t.Fatalf("first RCPT TO a: %v", err)
	}
	if err := client.Rcpt("first-b@example.com", nil); err != nil {
		t.Fatalf("first RCPT TO b: %v", err)
	}
	wc, err := client.Data()
	if err != nil {
		t.Fatalf("first DATA: %v", err)
	}
	msg1 := rawMessage("sender@example.com", "first-a@example.com, first-b@example.com", "First", "body one")
	if _, err := wc.Write([]byte(msg1)); err != nil {
		t.Fatalf("write first body: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close first DATA: %v", err)
	}

	// Second transaction, a single, different recipient.
	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("second MAIL FROM: %v", err)
	}
	if err := client.Rcpt("second@example.com", nil); err != nil {
		t.Fatalf("second RCPT TO: %v", err)
	}
	wc2, err := client.Data()
	if err != nil {
		t.Fatalf("second DATA: %v", err)
	}
	msg2 := rawMessage("sender@example.com", "second@example.com", "Second", "body two")
	if _, err := wc2.Write([]byte(msg2)); err != nil {
		t.Fatalf("write second body: %v", err)
	}
	if err := wc2.Close(); err != nil {
		t.Fatalf("close second DATA: %v", err)
	}

	calls := ts.sender.calls()
	if len(calls) != 2 {
		t.Fatalf("sender received %d messages, want 2", len(calls))
	}

	secondRecipients := calls[1].AllRecipients()
	for _, r := range secondRecipients {
		if r == "first-a@example.com" || r == "first-b@example.com" {
			t.Errorf("second transaction recipients %v leaked a recipient from the first transaction", secondRecipients)
		}
	}
	found := false
	for _, r := range secondRecipients {
		if r == "second@example.com" {
			found = true
		}
	}
	if !found {
		t.Errorf("second transaction recipients %v do not contain %q", secondRecipients, "second@example.com")
	}
}

// 15. Exactly one log record is emitted per message.
func TestExactlyOneLogRecordPerMessage(t *testing.T) {
	ts := newTestServer(t, nil)
	client := ts.dial()
	defer client.Close()

	if err := authenticate(t, client, sasl.Plain, testUsername, testPassword); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := client.Mail("sender@example.com", nil); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt("recipient@example.com", nil); err != nil {
		t.Fatalf("RCPT TO: %v", err)
	}

	wc, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	msg := rawMessage("sender@example.com", "recipient@example.com", "Logging", "body")
	if _, err := wc.Write([]byte(msg)); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("close DATA: %v", err)
	}

	logged := ts.logBuf.String()
	count := strings.Count(logged, "message delivery handled")
	if count != 1 {
		t.Fatalf("log contains %d delivery records, want exactly 1; log:\n%s", count, logged)
	}
}
