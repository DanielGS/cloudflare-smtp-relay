package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func decodeDeliveryLog(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("expected a single JSON log line, got %q: %v", buf.String(), err)
	}
	return decoded
}

func TestLogDelivery_SentEmitsInfoWithRequiredFields(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("debug", &buf)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}

	LogDelivery(logger, Delivery{
		MessageID: "relay-1",
		From:      "sender@example.com",
		To:        []string{"a@example.com", "b@example.com"},
		Subject:   "hello",
		Result:    "sent",
		Duration:  250 * time.Millisecond,
		Attempts:  1,
	})

	decoded := decodeDeliveryLog(t, &buf)

	if decoded["level"] != "INFO" {
		t.Fatalf("expected level INFO for result=sent, got %v", decoded["level"])
	}
	if decoded["message_id"] != "relay-1" {
		t.Fatalf("expected message_id=relay-1, got %v", decoded["message_id"])
	}
	if decoded["from"] != "sender@example.com" {
		t.Fatalf("expected from=sender@example.com, got %v", decoded["from"])
	}
	if decoded["result"] != "sent" {
		t.Fatalf("expected result=sent, got %v", decoded["result"])
	}
	if decoded["attempts"] != float64(1) {
		t.Fatalf("expected attempts=1, got %v", decoded["attempts"])
	}
}

func TestLogDelivery_DeferredEmitsWarn(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("debug", &buf)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}

	LogDelivery(logger, Delivery{MessageID: "relay-2", Result: "deferred"})

	decoded := decodeDeliveryLog(t, &buf)
	if decoded["level"] != "WARN" {
		t.Fatalf("expected level WARN for result=deferred, got %v", decoded["level"])
	}
}

func TestLogDelivery_RejectedEmitsError(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("debug", &buf)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}

	LogDelivery(logger, Delivery{MessageID: "relay-3", Result: "rejected"})

	decoded := decodeDeliveryLog(t, &buf)
	if decoded["level"] != "ERROR" {
		t.Fatalf("expected level ERROR for result=rejected, got %v", decoded["level"])
	}
}

func TestLogDelivery_TruncatesSubject(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("debug", &buf)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}

	longSubject := strings.Repeat("x", 200)
	LogDelivery(logger, Delivery{MessageID: "relay-4", Result: "sent", Subject: longSubject})

	decoded := decodeDeliveryLog(t, &buf)
	subject, ok := decoded["subject"].(string)
	if !ok {
		t.Fatalf("expected subject field to be a string, got %v", decoded["subject"])
	}
	runes := []rune(subject)
	if len(runes) != 121 {
		t.Fatalf("expected subject to be truncated to 121 runes (120 + ellipsis), got %d: %q", len(runes), subject)
	}
	if runes[120] != '…' {
		t.Fatalf("expected truncated subject to end with '…', got %q", subject)
	}
}

func TestLogDelivery_OmitsZeroValuedOptionalFields(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("debug", &buf)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}

	LogDelivery(logger, Delivery{MessageID: "relay-5", Result: "sent"})

	decoded := decodeDeliveryLog(t, &buf)
	for _, field := range []string{"http_status", "provider_code", "rfc_message_id", "error"} {
		if _, present := decoded[field]; present {
			t.Fatalf("expected zero-valued optional field %q to be omitted, but it was present: %v", field, decoded[field])
		}
	}
}

func TestLogDelivery_IncludesNonZeroOptionalFields(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("debug", &buf)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}

	LogDelivery(logger, Delivery{
		MessageID:    "relay-6",
		RFCMessageID: "<abc@originator>",
		Result:       "rejected",
		HTTPStatus:   422,
		ProviderCode: 10001,
		Err:          errors.New("provider rejected message"),
	})

	decoded := decodeDeliveryLog(t, &buf)
	if decoded["http_status"] != float64(422) {
		t.Fatalf("expected http_status=422, got %v", decoded["http_status"])
	}
	if decoded["provider_code"] != float64(10001) {
		t.Fatalf("expected provider_code=10001, got %v", decoded["provider_code"])
	}
	if decoded["rfc_message_id"] != "<abc@originator>" {
		t.Fatalf("expected rfc_message_id to be present, got %v", decoded["rfc_message_id"])
	}
	if decoded["error"] != "provider rejected message" {
		t.Fatalf("expected error message to be present, got %v", decoded["error"])
	}
}

func TestLogDelivery_NeverLogsCredentialLikeContent(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("debug", &buf)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}

	// Delivery has no body/credential fields at all; this test guards against a
	// future field addition silently leaking sensitive content through LogDelivery.
	LogDelivery(logger, Delivery{
		MessageID: "relay-7",
		From:      "sender@example.com",
		To:        []string{"rcpt@example.com"},
		Subject:   "invoice",
		Result:    "sent",
	})

	output := buf.String()
	for _, forbidden := range []string{"password", "secret", "token", "Authorization"} {
		if strings.Contains(strings.ToLower(output), strings.ToLower(forbidden)) {
			t.Fatalf("delivery log unexpectedly contains %q: %s", forbidden, output)
		}
	}
}

func TestDelivery_LevelForUnknownResultDefaultsToInfo(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("debug", &buf)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}

	LogDelivery(logger, Delivery{MessageID: "relay-8", Result: "something-else"})

	decoded := decodeDeliveryLog(t, &buf)
	if decoded["level"] != slog.LevelInfo.String() {
		t.Fatalf("expected an unrecognized result to default to INFO, got %v", decoded["level"])
	}
}
