package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNew_ValidLevelsProduceJSONLogger(t *testing.T) {
	tests := []struct {
		level string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"Info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"Error", slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			var buf bytes.Buffer
			logger, err := New(tt.level, &buf)
			if err != nil {
				t.Fatalf("New(%q) returned unexpected error: %v", tt.level, err)
			}
			if logger == nil {
				t.Fatalf("New(%q) returned a nil logger", tt.level)
			}

			logger.Log(context.Background(), tt.want, "probe message")

			if buf.Len() == 0 {
				t.Fatalf("expected a log line to be written for level %q, got none", tt.level)
			}

			var decoded map[string]any
			if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
				t.Fatalf("expected JSON output, got %q: %v", buf.String(), err)
			}
			if decoded["msg"] != "probe message" {
				t.Fatalf("expected msg field %q, got %v", "probe message", decoded["msg"])
			}
		})
	}
}

func TestNew_LevelBelowThresholdIsSuppressed(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("warn", &buf)
	if err != nil {
		t.Fatalf("New returned unexpected error: %v", err)
	}

	logger.Info("this should not appear")

	if buf.Len() != 0 {
		t.Fatalf("expected no output for a suppressed level, got %q", buf.String())
	}
}

func TestNew_UnknownLevelReturnsError(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New("verbose", &buf)
	if err == nil {
		t.Fatalf("expected an error for unknown level %q, got nil", "verbose")
	}
	if logger != nil {
		t.Fatalf("expected a nil logger when New returns an error, got %v", logger)
	}
	if !strings.Contains(err.Error(), "verbose") {
		t.Fatalf("expected error to mention the offending level %q, got: %v", "verbose", err)
	}
}
