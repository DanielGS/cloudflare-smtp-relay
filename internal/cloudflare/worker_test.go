package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DanielGS/cloudflare-smtp-relay/internal/email"
)

func newTestWorker(t *testing.T, handler http.HandlerFunc, maxRetries int) (*Worker, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	c, err := NewWorker(WorkerConfig{
		URL:        srv.URL + "/send",
		Secret:     "worker-secret-abc",
		Timeout:    2 * time.Second,
		MaxRetries: maxRetries,
		Sleep:      noopSleep,
	})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	return c, &calls
}

func TestWorker_SuccessfulSend(t *testing.T) {
	var gotPath, gotAuth, gotContentType string
	var gotBody map[string]any

	c, calls := newTestWorker(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":true,"messageId":"wm-1"}`)
	}, 0)

	res, err := c.Send(context.Background(), baseMessage())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if gotPath != "/send" {
		t.Errorf("path = %q, want /send", gotPath)
	}
	if gotAuth != "Bearer worker-secret-abc" {
		t.Errorf("Authorization = %q, want Bearer worker-secret-abc", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}

	from := gotBody["from"].(map[string]any)
	if from["address"] != "support@mydomainexample.com" || from["name"] != "Support Team" {
		t.Errorf("from = %v, want display name carried through", from)
	}
	if _, ok := from["email"]; ok {
		t.Error("Worker payload must not use \"email\" key; shared shape uses \"address\"")
	}

	if res.ProviderID != "wm-1" {
		t.Errorf("ProviderID = %q, want wm-1", res.ProviderID)
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Errorf("handler calls = %d, want 1", *calls)
	}
}

func TestWorker_BccIsolation(t *testing.T) {
	var gotBody map[string]any
	c, _ := newTestWorker(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":true,"messageId":"wm-1"}`)
	}, 0)

	msg := baseMessage()
	msg.To = []email.Address{{Address: "to1@example.com"}}
	msg.Cc = []email.Address{{Address: "cc1@example.com"}}
	msg.Bcc = []email.Address{{Address: "secret@example.com"}}

	if _, err := c.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	for _, key := range []string{"to", "cc"} {
		for _, entry := range gotBody[key].([]any) {
			if entry.(map[string]any)["address"] == "secret@example.com" {
				t.Errorf("bcc address leaked into %q array", key)
			}
		}
	}
	bcc := gotBody["bcc"].([]any)
	if len(bcc) != 1 || bcc[0].(map[string]any)["address"] != "secret@example.com" {
		t.Errorf("bcc = %v, want [secret@example.com]", bcc)
	}
}

func TestWorker_HTTP400_NoRetry(t *testing.T) {
	c, calls := newTestWorker(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"success":false,"code":"E_INVALID_PAYLOAD","error":"bad request"}`)
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

func TestWorker_HTTP500_RetriesExhausted(t *testing.T) {
	const maxRetries = 2
	c, calls := newTestWorker(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"success":false,"code":"E_INTERNAL","error":"boom"}`)
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
	want := int32(1 + maxRetries)
	if atomic.LoadInt32(calls) != want {
		t.Errorf("handler calls = %d, want %d", *calls, want)
	}
}

func TestWorker_RateLimitCodes(t *testing.T) {
	tests := []string{"E_RATE_LIMIT_EXCEEDED", "E_DAILY_LIMIT_EXCEEDED"}
	for _, code := range tests {
		t.Run(code, func(t *testing.T) {
			c, calls := newTestWorker(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprintf(w, `{"success":false,"code":%q,"error":"rate limited"}`, code)
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
			if de.EnhancedCode != [3]int{4, 4, 5} {
				t.Errorf("EnhancedCode = %v, want {4 4 5}", de.EnhancedCode)
			}
			if atomic.LoadInt32(calls) != 1 {
				t.Errorf("handler calls = %d, want 1", *calls)
			}
		})
	}
}

func TestWorker_PermanentEntitlementCodes(t *testing.T) {
	tests := []string{"E_SENDER_NOT_VERIFIED", "E_SENDER_DOMAIN_NOT_AVAILABLE", "E_RECIPIENT_NOT_ALLOWED"}
	for _, code := range tests {
		t.Run(code, func(t *testing.T) {
			c, calls := newTestWorker(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(w, `{"success":false,"code":%q,"error":"not allowed"}`, code)
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
			if de.EnhancedCode != [3]int{5, 7, 1} {
				t.Errorf("EnhancedCode = %v, want {5 7 1}", de.EnhancedCode)
			}
			if atomic.LoadInt32(calls) != 1 {
				t.Errorf("handler calls = %d, want 1 (permanent, no retry)", *calls)
			}
		})
	}
}

func TestWorker_UnknownCodeClassifiesFromStatus(t *testing.T) {
	c, _ := newTestWorker(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, `{"success":false,"code":"E_SOMETHING_NEW","error":"unexpected"}`)
	}, 0)

	_, err := c.Send(context.Background(), baseMessage())
	de, ok := email.AsDeliveryError(err)
	if !ok {
		t.Fatalf("expected *email.DeliveryError, got %T: %v", err, err)
	}
	if !de.Temporary {
		t.Error("Temporary = false, want true (503 falls back to status classification)")
	}
	if de.SMTPCode != 451 {
		t.Errorf("SMTPCode = %d, want 451", de.SMTPCode)
	}
}

func TestWorker_MalformedJSON(t *testing.T) {
	c, _ := newTestWorker(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{{{not json`)
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

func TestWorker_SecretNeverLeaks(t *testing.T) {
	const secret = "worker-super-secret-do-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"success":false,"code":"E_INVALID_PAYLOAD","error":"bad"}`)
	}))
	defer srv.Close()

	c, err := NewWorker(WorkerConfig{
		URL:        srv.URL + "/send",
		Secret:     secret,
		Timeout:    2 * time.Second,
		MaxRetries: 0,
		Sleep:      noopSleep,
	})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}

	_, sendErr := c.Send(context.Background(), baseMessage())
	if sendErr == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(sendErr.Error(), secret) {
		t.Errorf("error string leaked the secret: %s", sendErr.Error())
	}
}

func TestWorker_RetriedRequestEventuallySucceeds(t *testing.T) {
	var attempt int32
	c, calls := newTestWorker(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempt, 1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"success":false,"code":"E_INTERNAL","error":"boom"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"success":true,"messageId":"wm-2"}`)
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

func TestNewWorker_ValidatesConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  WorkerConfig
	}{
		{"empty URL", WorkerConfig{URL: "", Secret: "s"}},
		{"empty secret", WorkerConfig{URL: "https://worker.example.com/send", Secret: ""}},
		{"negative max retries", WorkerConfig{URL: "https://worker.example.com/send", Secret: "s", MaxRetries: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewWorker(tt.cfg); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}
