package cloudflare

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dagase/cloudflare-smtp-relay/internal/email"
)

func TestBuildPayload_AddressKeyNotEmail(t *testing.T) {
	msg := &email.Message{
		From:    email.Address{Name: "Support Team", Address: "support@yourdomain.com"},
		To:      []email.Address{{Name: "Jane Doe", Address: "jane@example.com"}},
		Subject: "hello",
		Text:    "body",
	}

	p := buildPayload(msg)
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	body := string(raw)

	if strings.Contains(body, `"email"`) {
		t.Errorf("payload must not contain the literal key \"email\", got: %s", body)
	}
	if !strings.Contains(body, `"address"`) {
		t.Errorf("payload must contain the literal key \"address\", got: %s", body)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	from, ok := decoded["from"].(map[string]any)
	if !ok {
		t.Fatalf("from is not an object: %v", decoded["from"])
	}
	if _, ok := from["address"]; !ok {
		t.Error("from object missing \"address\" key")
	}
	if _, ok := from["email"]; ok {
		t.Error("from object must not contain \"email\" key")
	}
}

func TestBuildPayload_NameOmittedWhenEmpty(t *testing.T) {
	msg := &email.Message{
		From:    email.Address{Address: "noreply@example.com"},
		To:      []email.Address{{Address: "jane@example.com"}},
		Subject: "hello",
		Text:    "body",
	}

	raw, err := json.Marshal(buildPayload(msg))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	from := decoded["from"].(map[string]any)
	if _, ok := from["name"]; ok {
		t.Errorf("from object must omit \"name\" when empty, got: %v", from)
	}
}

func TestBuildPayload_OptionalFieldsOmittedWhenEmpty(t *testing.T) {
	msg := &email.Message{
		From:    email.Address{Address: "noreply@example.com"},
		To:      []email.Address{{Address: "jane@example.com"}},
		Subject: "hello",
		Text:    "body only, no html",
	}

	raw, err := json.Marshal(buildPayload(msg))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	body := string(raw)

	for _, key := range []string{`"cc"`, `"bcc"`, `"reply_to"`, `"html"`, `"headers"`} {
		if strings.Contains(body, key) {
			t.Errorf("payload must omit %s when empty, got: %s", key, body)
		}
	}
	if !strings.Contains(body, `"text"`) {
		t.Errorf("payload must include \"text\" when set, got: %s", body)
	}
}

func TestBuildPayload_ReplyToIsSnakeCase(t *testing.T) {
	msg := &email.Message{
		From:    email.Address{Address: "noreply@example.com"},
		To:      []email.Address{{Address: "jane@example.com"}},
		ReplyTo: "support@example.com",
		Subject: "hello",
		Text:    "body",
	}

	raw, err := json.Marshal(buildPayload(msg))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, `"reply_to":"support@example.com"`) {
		t.Errorf("expected snake_case \"reply_to\" key, got: %s", body)
	}
	if strings.Contains(body, `"replyTo"`) {
		t.Errorf("payload must not use camelCase \"replyTo\", got: %s", body)
	}
}
