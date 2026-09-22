// Package logging provides the relay's structured logging setup: a JSON
// slog.Logger factory and a per-message delivery log helper that never
// leaks message bodies or credentials.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// New returns a JSON-formatted *slog.Logger writing to w, filtered at the
// given level. The level is parsed case-insensitively and must be one of
// "debug", "info", "warn" or "error"; any other value is rejected with an
// error rather than silently defaulting, since callers are expected to have
// already validated the level (see internal/config).
func New(level string, w io.Writer) (*slog.Logger, error) {
	lvl, err := parseLevel(level)
	if err != nil {
		return nil, err
	}

	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler), nil
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("logging: unknown level %q, expected one of debug, info, warn, error", level)
	}
}
