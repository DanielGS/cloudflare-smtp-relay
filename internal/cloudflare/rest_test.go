package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dagase/cloudflare-smtp-relay/internal/email"
)

// noopSleep is a fake Sleep seam that never actually waits, so retry tests
// run instantly.
func noopSleep(ctx context.Context, d time.Duration) error {
	return ctx.Err()
}

func baseMessage() *email.Message {
	return &email.Message{
		From:    email.Address{Name: "Support Team", Address: "support@yourdomain.com"},
		To:      []email.Address{{Name: "Jane Doe", Address: "jane@example.com"}},
		Subject: "hello",
		Text:    "plain body",
	}
}

func newTestREST(t *testing.T, handler http.HandlerFunc, maxRetries int) (*REST, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	c, err := NewREST(RESTConfig{
		BaseURL:    srv.URL,
		AccountID:  "acct123",
		APIToken:   "test-token-abc",
		Timeout:    2 * time.Second,
		MaxRetries: maxRetries,
		Sleep:      noopSleep,
	})
	if err != nil {
		t.Fatalf("NewREST() error = %v", err)
	}
	return c, &calls
}

func TestREST_SuccessfulSend(t *testing.T) {
	var gotPath, gotAuth, gotContentType string
	var gotBody map[string]any

	c, calls := newTestREST(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":true,"errors":[],"messages":[],"result":{"delivered":["msg-1"],"permanent_bounces":[],"queued":[]}}`)
	}, 0)

	res, err := c.Send(context.Background(), baseMessage())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	wantPath := "/accounts/acct123/email/sending/send"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if gotAuth != "Bearer test-token-abc" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-token-abc")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}

	from := gotBody["from"].(map[string]any)
	if from["address"] != "support@yourdomain.com" || from["name"] != "Support Team" {
		t.Errorf("from = %v, want display name carried through", from)
	}
	to := gotBody["to"].([]any)[0].(map[string]any)
	if to["address"] != "jane@example.com" || to["name"] != "Jane Doe" {
		t.Errorf("to[0] = %v, want display name carried through", to)
	}
	if gotBody["subject"] != "hello" || gotBody["text"] != "plain body" {
		t.Errorf("subject/text = %v/%v, want hello/plain body", gotBody["subject"], gotBody["text"])
	}

	if res.ProviderID != "msg-1" {
		t.Errorf("ProviderID = %q, want msg-1", res.ProviderID)
	}
	if res.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", res.Attempts)
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Errorf("handler calls = %d, want 1", *calls)
	}
}

func TestREST_MultipleRecipients(t *testing.T) {
	var gotBody map[string]any

	c, _ := newTestREST(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":true,"result":{"delivered":["m1"]}}`)
	}, 0)

	msg := baseMessage()
	msg.To = []email.Address{{Address: "to1@example.com"}, {Address: "to2@example.com"}}
	msg.Cc = []email.Address{{Address: "cc1@example.com"}}
	msg.Bcc = []email.Address{{Address: "bcc1@example.com"}}

	if _, err := c.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	to := gotBody["to"].([]any)
	if len(to) != 2 {
		t.Fatalf("to has %d entries, want 2", len(to))
	}
	cc := gotBody["cc"].([]any)
	if len(cc) != 1 || cc[0].(map[string]any)["address"] != "cc1@example.com" {
		t.Errorf("cc = %v, want [cc1@example.com]", cc)
	}
	bcc := gotBody["bcc"].([]any)
	if len(bcc) != 1 || bcc[0].(map[string]any)["address"] != "bcc1@example.com" {
		t.Errorf("bcc = %v, want [bcc1@example.com]", bcc)
	}
}

func TestREST_BccIsolation(t *testing.T) {
	var gotBody map[string]any

	c, _ := newTestREST(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":true,"result":{"delivered":["m1"]}}`)
	}, 0)

	msg := baseMessage()
	msg.To = []email.Address{{Address: "to1@example.com"}}
	msg.Cc = []email.Address{{Address: "cc1@example.com"}}
	msg.Bcc = []email.Address{{Address: "secret@example.com"}}

	if _, err := c.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	for _, key := range []string{"to", "cc"} {
		list := gotBody[key].([]any)
		for _, entry := range list {
			addr := entry.(map[string]any)["address"]
			if addr == "secret@example.com" {
				t.Errorf("bcc address leaked into %q array", key)
			}
		}
	}
	bcc := gotBody["bcc"].([]any)
	if len(bcc) != 1 || bcc[0].(map[string]any)["address"] != "secret@example.com" {
		t.Errorf("bcc = %v, want [secret@example.com]", bcc)
	}
}

func TestREST_HTMLOnly(t *testing.T) {
	var raw string
	c, _ := newTestREST(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":true,"result":{"delivered":["m1"]}}`)
	}, 0)

	msg := baseMessage()
	msg.Text = ""
	msg.HTML = "<p>hi</p>"

	if _, err := c.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !strings.Contains(raw, `"html"`) {
		t.Errorf("body must contain html, got: %s", raw)
	}
	if strings.Contains(raw, `"text"`) {
		t.Errorf("body must omit text, got: %s", raw)
	}
}

func TestREST_TextOnly(t *testing.T) {
	var raw string
	c, _ := newTestREST(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":true,"result":{"delivered":["m1"]}}`)
	}, 0)

	msg := baseMessage()
	msg.Text = "plain only"
	msg.HTML = ""

	if _, err := c.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if !strings.Contains(raw, `"text"`) {
		t.Errorf("body must contain text, got: %s", raw)
	}
	if strings.Contains(raw, `"html"`) {
		t.Errorf("body must omit html, got: %s", raw)
	}
}

func TestREST_HTTP400_NoRetry(t *testing.T) {
	c, calls := newTestREST(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"success":false,"errors":[{"code":10001,"message":"invalid_request_schema"}],"result":null}`)
	}, 3)

	_, err := c.Send(context.Background(), baseMessage())
	de, ok := email.AsDeliveryError(err)
	if !ok {
		t.Fatalf("expected *email.DeliveryError, got %T: %v", err, err)
	}
	if de.Temporary {
		t.Error("Temporary = true, want false")
	}
	if de.SMTPCode != 550 {
		t.Errorf("SMTPCode = %d, want 550", de.SMTPCode)
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Errorf("handler calls = %d, want 1 (no retry)", *calls)
	}
}

func TestREST_HTTP500_RetriesExhausted(t *testing.T) {
	const maxRetries = 2
	c, calls := newTestREST(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"success":false,"errors":[{"code":10002,"message":"internal_error"}],"result":null}`)
	}, maxRetries)

	_, err := c.Send(context.Background(), baseMessage())
	de, ok := email.AsDeliveryError(err)
	if !ok {
		t.Fatalf("expected *email.DeliveryError, got %T: %v", err, err)
	}
	if !de.Temporary {
		t.Error("Temporary = false, want true")
	}
	if de.SMTPCode != 451 {
		t.Errorf("SMTPCode = %d, want 451", de.SMTPCode)
	}
	if de.EnhancedCode != [3]int{4, 3, 0} {
		t.Errorf("EnhancedCode = %v, want {4 3 0}", de.EnhancedCode)
	}
	want := int32(1 + maxRetries)
	if atomic.LoadInt32(calls) != want {
		t.Errorf("handler calls = %d, want %d", *calls, want)
	}
	if de.Attempts != int(want) {
		t.Errorf("Attempts = %d, want %d", de.Attempts, want)
	}
}

func TestREST_HTTP429_HonorsRetryAfter(t *testing.T) {
	var attempt int32
	c, calls := newTestREST(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempt, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"success":false,"errors":[{"code":10004,"message":"throttled"}],"result":null}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":true,"result":{"delivered":["m1"]}}`)
	}, 1)

	res, err := c.Send(context.Background(), baseMessage())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if res.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", res.Attempts)
	}
	if atomic.LoadInt32(calls) != 2 {
		t.Errorf("handler calls = %d, want 2", *calls)
	}
}

func TestREST_HTTP403_Code10105_TemporaryNoRetry(t *testing.T) {
	c, calls := newTestREST(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"success":false,"errors":[{"code":10105,"message":"not entitled"}],"result":null}`)
	}, 3)

	_, err := c.Send(context.Background(), baseMessage())
	de, ok := email.AsDeliveryError(err)
	if !ok {
		t.Fatalf("expected *email.DeliveryError, got %T: %v", err, err)
	}
	if !de.Temporary {
		t.Error("Temporary = false, want true (deliberate choice for 403/entitlement)")
	}
	if de.SMTPCode != 451 {
		t.Errorf("SMTPCode = %d, want 451", de.SMTPCode)
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Errorf("handler calls = %d, want 1 (401/403 must not be internally retried)", *calls)
	}
}

func TestREST_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":true,"result":{"delivered":["m1"]}}`)
	}))
	defer srv.Close()

	c, err := NewREST(RESTConfig{
		BaseURL:    srv.URL,
		AccountID:  "acct123",
		APIToken:   "test-token",
		Timeout:    30 * time.Millisecond,
		MaxRetries: 0,
		Sleep:      noopSleep,
	})
	if err != nil {
		t.Fatalf("NewREST() error = %v", err)
	}

	_, sendErr := c.Send(context.Background(), baseMessage())
	de, ok := email.AsDeliveryError(sendErr)
	if !ok {
		t.Fatalf("expected *email.DeliveryError, got %T: %v", sendErr, sendErr)
	}
	if !de.Temporary {
		t.Error("Temporary = false, want true")
	}
	if de.SMTPCode != 451 {
		t.Errorf("SMTPCode = %d, want 451", de.SMTPCode)
	}
	if de.EnhancedCode != [3]int{4, 4, 1} {
		t.Errorf("EnhancedCode = %v, want {4 4 1}", de.EnhancedCode)
	}
}

func TestREST_HTTP200SuccessFalse(t *testing.T) {
	c, _ := newTestREST(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":false,"errors":[{"code":10001,"message":"invalid_request_schema"}],"result":null}`)
	}, 0)

	_, err := c.Send(context.Background(), baseMessage())
	de, ok := email.AsDeliveryError(err)
	if !ok {
		t.Fatalf("expected *email.DeliveryError, got %T: %v", err, err)
	}
	if de.Temporary {
		t.Error("Temporary = true, want false: HTTP 200 with success:false must not be treated as success")
	}
	if de.SMTPCode != 550 {
		t.Errorf("SMTPCode = %d, want 550", de.SMTPCode)
	}
}

func TestREST_MalformedJSON(t *testing.T) {
	c, _ := newTestREST(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `not-json-at-all{{{`)
	}, 0)

	_, err := c.Send(context.Background(), baseMessage())
	de, ok := email.AsDeliveryError(err)
	if !ok {
		t.Fatalf("expected *email.DeliveryError, got %T: %v", err, err)
	}
	if !de.Temporary {
		t.Error("Temporary = false, want true")
	}
	if de.SMTPCode != 451 {
		t.Errorf("SMTPCode = %d, want 451", de.SMTPCode)
	}
}

func TestREST_SecretNeverLeaks(t *testing.T) {
	const token = "sk-very-secret-token-do-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"success":false,"errors":[{"code":10001,"message":"invalid_request_schema"}],"result":null}`)
	}))
	defer srv.Close()

	c, err := NewREST(RESTConfig{
		BaseURL:    srv.URL,
		AccountID:  "acct123",
		APIToken:   token,
		Timeout:    2 * time.Second,
		MaxRetries: 0,
		Sleep:      noopSleep,
	})
	if err != nil {
		t.Fatalf("NewREST() error = %v", err)
	}

	_, sendErr := c.Send(context.Background(), baseMessage())
	if sendErr == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(sendErr.Error(), token) {
		t.Errorf("error string leaked the API token: %s", sendErr.Error())
	}
	de, ok := email.AsDeliveryError(sendErr)
	if ok && strings.Contains(de.Reason, token) {
		t.Errorf("Reason leaked the API token: %s", de.Reason)
	}
}

func TestREST_RetriedRequestEventuallySucceeds(t *testing.T) {
	var attempt int32
	c, calls := newTestREST(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempt, 1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"success":false,"errors":[{"code":10002,"message":"internal_error"}],"result":null}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":true,"result":{"delivered":["m2"]}}`)
	}, 2)

	res, err := c.Send(context.Background(), baseMessage())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if res.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", res.Attempts)
	}
	if atomic.LoadInt32(calls) != 2 {
		t.Errorf("handler calls = %d, want 2", *calls)
	}
}

func TestNewREST_ValidatesConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  RESTConfig
	}{
		{"empty base URL", RESTConfig{BaseURL: "", AccountID: "a", APIToken: "t"}},
		{"empty account id", RESTConfig{BaseURL: "https://api.example.com", AccountID: "", APIToken: "t"}},
		{"empty api token", RESTConfig{BaseURL: "https://api.example.com", AccountID: "a", APIToken: ""}},
		{"negative max retries", RESTConfig{BaseURL: "https://api.example.com", AccountID: "a", APIToken: "t", MaxRetries: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewREST(tt.cfg); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}
