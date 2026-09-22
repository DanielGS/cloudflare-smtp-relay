package logging

import (
	"strings"
	"testing"
)

func TestTruncateSubject_ShortStringIsUnchanged(t *testing.T) {
	got := TruncateSubject("hello world", 120)
	if got != "hello world" {
		t.Fatalf("expected unchanged short subject, got %q", got)
	}
}

func TestTruncateSubject_ExactlyAtLimitIsUnchanged(t *testing.T) {
	s := strings.Repeat("a", 120)
	got := TruncateSubject(s, 120)
	if got != s {
		t.Fatalf("expected a 120-rune subject to be left untouched, got %q (len %d)", got, len([]rune(got)))
	}
}

func TestTruncateSubject_OverLimitIsTruncatedWithEllipsis(t *testing.T) {
	s := strings.Repeat("a", 130)
	got := TruncateSubject(s, 120)

	runes := []rune(got)
	if len(runes) != 121 {
		t.Fatalf("expected truncated subject to have 121 runes (120 + ellipsis), got %d: %q", len(runes), got)
	}
	if runes[120] != '…' {
		t.Fatalf("expected truncated subject to end with '…', got %q", got)
	}
	if string(runes[:120]) != strings.Repeat("a", 120) {
		t.Fatalf("expected the first 120 runes to be preserved, got %q", string(runes[:120]))
	}
}

func TestTruncateSubject_DoesNotSplitMultiByteRune(t *testing.T) {
	// "日" is a 3-byte UTF-8 rune. A byte-based truncation at 120 bytes would
	// slice through the middle of one of these runes and corrupt the string.
	s := strings.Repeat("日", 130)
	got := TruncateSubject(s, 120)

	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected truncated subject to end with '…', got %q", got)
	}

	// The result must be valid UTF-8: no replacement characters from a split rune.
	if strings.ContainsRune(got, '�') {
		t.Fatalf("truncated subject contains a UTF-8 replacement character, rune was split: %q", got)
	}

	runes := []rune(got)
	if len(runes) != 121 {
		t.Fatalf("expected 121 runes (120 + ellipsis), got %d: %q", len(runes), got)
	}
	if string(runes[:120]) != strings.Repeat("日", 120) {
		t.Fatalf("expected the first 120 runes to be preserved intact, got %q", string(runes[:120]))
	}
}

func TestTruncateSubject_EmptyString(t *testing.T) {
	got := TruncateSubject("", 120)
	if got != "" {
		t.Fatalf("expected empty subject to remain empty, got %q", got)
	}
}
