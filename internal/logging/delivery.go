package logging

import (
	"context"
	"log/slog"
	"time"
)

// maxSubjectRunes is the maximum number of runes kept from a message subject
// before logging it, so a pathologically long subject cannot bloat log
// storage or leak more content than needed for triage.
const maxSubjectRunes = 120

// Delivery is the structured record emitted once per handled message. It
// intentionally carries no message body, credential or token field: only
// envelope- and outcome-level metadata belongs here.
type Delivery struct {
	MessageID    string // relay-internal id
	RFCMessageID string // original Message-ID header, may be empty
	From         string
	To           []string
	Subject      string // already truncated by the helper
	Result       string // "sent", "rejected", "deferred"
	Duration     time.Duration
	HTTPStatus   int // 0 when absent
	ProviderCode int // 0 when absent
	Attempts     int
	Err          error // may be nil
}

// TruncateSubject truncates s to at most max runes, appending a single "…"
// when truncation occurs. It operates on runes rather than bytes so a
// multi-byte UTF-8 rune is never split.
func TruncateSubject(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// levelForResult maps a Delivery.Result to the slog level LogDelivery emits
// at: "deferred" logs at warn, "rejected" logs at error, and everything
// else (including "sent" and any unrecognized value) logs at info.
func levelForResult(result string) slog.Level {
	switch result {
	case "deferred":
		return slog.LevelWarn
	case "rejected":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// LogDelivery emits one structured record for a handled message. The level
// is chosen from d.Result (info for "sent", warn for "deferred", error for
// "rejected"). The subject is truncated to maxSubjectRunes runes, and
// zero-valued optional fields (HTTPStatus, ProviderCode, RFCMessageID, Err)
// are omitted rather than logged as noisy zeros. LogDelivery never logs
// message bodies, credentials or tokens: Delivery carries no such field.
func LogDelivery(logger *slog.Logger, d Delivery) {
	attrs := []any{
		slog.String("message_id", d.MessageID),
		slog.String("from", d.From),
		slog.Any("to", d.To),
		slog.String("subject", TruncateSubject(d.Subject, maxSubjectRunes)),
		slog.String("result", d.Result),
		// Emitted in milliseconds rather than as a raw slog.Duration, which
		// would render as a bare nanosecond integer and read poorly in a log.
		slog.Int64("duration_ms", d.Duration.Milliseconds()),
		slog.Int("attempts", d.Attempts),
	}

	if d.RFCMessageID != "" {
		attrs = append(attrs, slog.String("rfc_message_id", d.RFCMessageID))
	}
	if d.HTTPStatus != 0 {
		attrs = append(attrs, slog.Int("http_status", d.HTTPStatus))
	}
	if d.ProviderCode != 0 {
		attrs = append(attrs, slog.Int("provider_code", d.ProviderCode))
	}
	if d.Err != nil {
		attrs = append(attrs, slog.String("error", d.Err.Error()))
	}

	logger.Log(context.Background(), levelForResult(d.Result), "message delivery handled", attrs...)
}
