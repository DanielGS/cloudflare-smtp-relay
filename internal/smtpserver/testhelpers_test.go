package smtpserver_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"

	"github.com/dagase/cloudflare-smtp-relay/internal/email"
	"github.com/dagase/cloudflare-smtp-relay/internal/logging"
	"github.com/dagase/cloudflare-smtp-relay/internal/smtpserver"
)

const (
	testUsername = "relay-user"
	testPassword = "relay-secret"
)

// fakeSender is a test double for email.Sender that records every call and
// returns a configurable, possibly per-call, result or error.
type fakeSender struct {
	mu       sync.Mutex
	messages []*email.Message

	// result and err are used when resultFunc is nil.
	result *email.Result
	err    error

	// resultFunc, when set, overrides result/err and lets a test decide the
	// outcome from the message itself.
	resultFunc func(*email.Message) (*email.Result, error)
}

func (f *fakeSender) Send(_ context.Context, msg *email.Message) (*email.Result, error) {
	f.mu.Lock()
	f.messages = append(f.messages, msg)
	f.mu.Unlock()

	if f.resultFunc != nil {
		return f.resultFunc(msg)
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &email.Result{ProviderID: "test-provider-id", HTTPStatus: 200, Attempts: 1}, nil
}

func (f *fakeSender) calls() []*email.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*email.Message, len(f.messages))
	copy(out, f.messages)
	return out
}

// testServer bundles a running smtpserver.Server with its listener address
// and the fake sender it was built with, plus a buffer capturing every log
// record emitted through the shared logger.
type testServer struct {
	t      *testing.T
	server *smtpserver.Server
	sender *fakeSender
	logBuf *bytes.Buffer

	addr string
}

// newTestServer builds and starts an smtpserver.Server bound to a loopback
// port, ready to accept connections. Callers may override fields on the
// returned Options via mutate before the server is built.
func newTestServer(t *testing.T, mutate func(*smtpserver.Options)) *testServer {
	t.Helper()

	sender := &fakeSender{}
	logBuf := &bytes.Buffer{}
	logger, err := logging.New("debug", logBuf)
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	opts := smtpserver.Options{
		Addr:            "127.0.0.1:0",
		Domain:          "relay.test",
		Username:        testUsername,
		Password:        testPassword,
		MaxMessageBytes: 1 << 20, // 1 MiB
		MaxRecipients:   10,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		Policy:          email.NewSenderPolicy(nil),
		Sender:          sender,
		Logger:          logger,
	}
	if mutate != nil {
		mutate(&opts)
	}

	srv, err := smtpserver.New(opts)
	if err != nil {
		t.Fatalf("smtpserver.New: %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(l)
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		select {
		case err := <-errCh:
			if err != nil && err != gosmtp.ErrServerClosed {
				t.Errorf("Serve returned unexpected error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("timed out waiting for Serve to return after Shutdown")
		}
	})

	return &testServer{
		t:      t,
		server: srv,
		sender: sender,
		logBuf: logBuf,
		addr:   l.Addr().String(),
	}
}

// dial opens a plaintext SMTP client connection to the test server and
// issues EHLO.
func (ts *testServer) dial() *gosmtp.Client {
	ts.t.Helper()

	conn, err := net.DialTimeout("tcp", ts.addr, 2*time.Second)
	if err != nil {
		ts.t.Fatalf("dial %s: %v", ts.addr, err)
	}

	client := gosmtp.NewClient(conn)
	if err := client.Hello("client.test"); err != nil {
		ts.t.Fatalf("EHLO: %v", err)
	}
	return client
}

// authenticate runs AUTH with the given mechanism ("PLAIN" or "LOGIN") and
// credentials, returning the resulting error (nil on success).
func authenticate(t *testing.T, client *gosmtp.Client, mech, username, password string) error {
	t.Helper()

	var authClient sasl.Client
	switch mech {
	case sasl.Plain:
		authClient = sasl.NewPlainClient("", username, password)
	case sasl.Login:
		authClient = sasl.NewLoginClient(username, password)
	default:
		t.Fatalf("unsupported test mechanism %q", mech)
	}

	return client.Auth(authClient)
}

// smtpErrorCode extracts the numeric SMTP code from err, failing the test
// if err is not a *gosmtp.SMTPError.
func smtpErrorCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		t.Fatal("expected an *smtp.SMTPError, got nil")
	}
	var smtpErr *gosmtp.SMTPError
	if !asSMTPError(err, &smtpErr) {
		t.Fatalf("expected an *smtp.SMTPError, got %T: %v", err, err)
	}
	return smtpErr.Code
}

func asSMTPError(err error, target **gosmtp.SMTPError) bool {
	if se, ok := err.(*gosmtp.SMTPError); ok {
		*target = se
		return true
	}
	return false
}

// rawMessage builds a minimal, valid RFC 5322 message with the given
// headers and body, CRLF-terminated as SMTP DATA requires.
func rawMessage(from, to, subject, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	b.WriteString("\r\n")
	return b.String()
}

// rawHTMLMessage builds a minimal valid RFC 5322 message with an HTML body.
func rawHTMLMessage(from, to, subject, htmlBody string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/html; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(htmlBody)
	b.WriteString("\r\n")
	return b.String()
}
