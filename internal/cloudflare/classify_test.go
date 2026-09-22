package cloudflare

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestClassifyHTTP_StatusOnly(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		wantTemp     bool
		wantSMTP     int
		wantEnhanced [3]int
	}{
		{"429 throttled", 429, true, 451, [3]int{4, 4, 5}},
		{"500 server error", 500, true, 451, [3]int{4, 3, 0}},
		{"502 server error", 502, true, 451, [3]int{4, 3, 0}},
		{"503 server error", 503, true, 451, [3]int{4, 3, 0}},
		{"504 server error", 504, true, 451, [3]int{4, 3, 0}},
		{"401 unauthorized is temporary", 401, true, 451, [3]int{4, 7, 0}},
		{"403 forbidden is temporary", 403, true, 451, [3]int{4, 7, 0}},
		{"400 invalid request", 400, false, 550, [3]int{5, 6, 0}},
		{"404 not found", 404, false, 550, [3]int{5, 1, 2}},
		{"other 4xx is permanent", 418, false, 550, [3]int{5, 6, 0}},
		{"other 5xx is temporary", 599, true, 451, [3]int{4, 3, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			de := classifyHTTP(tt.status, 0, "", 1)
			if de == nil {
				t.Fatalf("classifyHTTP(%d) returned nil", tt.status)
			}
			if de.Temporary != tt.wantTemp {
				t.Errorf("Temporary = %v, want %v", de.Temporary, tt.wantTemp)
			}
			if de.SMTPCode != tt.wantSMTP {
				t.Errorf("SMTPCode = %d, want %d", de.SMTPCode, tt.wantSMTP)
			}
			if de.EnhancedCode != tt.wantEnhanced {
				t.Errorf("EnhancedCode = %v, want %v", de.EnhancedCode, tt.wantEnhanced)
			}
			if de.HTTPStatus != tt.status {
				t.Errorf("HTTPStatus = %d, want %d", de.HTTPStatus, tt.status)
			}
			if de.Reason == "" {
				t.Error("Reason must not be empty")
			}
		})
	}
}

func TestClassifyHTTP_ProviderCode(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		code         int
		wantTemp     bool
		wantSMTP     int
		wantEnhanced [3]int
	}{
		{"10004 throttled", 429, 10004, true, 451, [3]int{4, 4, 5}},
		{"10002 upstream error", 500, 10002, true, 451, [3]int{4, 3, 0}},
		{"10003 upstream error", 502, 10003, true, 451, [3]int{4, 3, 0}},
		{"10100 upstream error", 503, 10100, true, 451, [3]int{4, 3, 0}},
		{"10101 credential rejected", 401, 10101, true, 451, [3]int{4, 7, 0}},
		{"10103 credential rejected", 401, 10103, true, 451, [3]int{4, 7, 0}},
		{"10102 not entitled", 403, 10102, true, 451, [3]int{4, 7, 0}},
		{"10105 not entitled", 403, 10105, true, 451, [3]int{4, 7, 0}},
		{"10203 not entitled", 403, 10203, true, 451, [3]int{4, 7, 0}},
		{"10001 invalid request", 400, 10001, false, 550, [3]int{5, 6, 0}},
		{"10200 invalid request", 400, 10200, false, 550, [3]int{5, 6, 0}},
		{"10201 invalid request", 400, 10201, false, 550, [3]int{5, 6, 0}},
		{"10202 invalid request", 400, 10202, false, 550, [3]int{5, 6, 0}},
		{"10000 not found", 404, 10000, false, 550, [3]int{5, 1, 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			de := classifyHTTP(tt.status, tt.code, "some message", 1)
			if de.Temporary != tt.wantTemp {
				t.Errorf("Temporary = %v, want %v", de.Temporary, tt.wantTemp)
			}
			if de.SMTPCode != tt.wantSMTP {
				t.Errorf("SMTPCode = %d, want %d", de.SMTPCode, tt.wantSMTP)
			}
			if de.EnhancedCode != tt.wantEnhanced {
				t.Errorf("EnhancedCode = %v, want %v", de.EnhancedCode, tt.wantEnhanced)
			}
			if de.ProviderCode != tt.code {
				t.Errorf("ProviderCode = %d, want %d", de.ProviderCode, tt.code)
			}
		})
	}
}

func TestClassifyHTTP_EmbeddedSuccessFalseUnknownCode(t *testing.T) {
	// HTTP 200 but "success": false with an unrecognized embedded error code
	// must classify as a conservative permanent failure.
	de := classifyHTTP(200, 99999, "unknown provider error", 1)
	if de.Temporary {
		t.Error("Temporary = true, want false for unknown embedded code")
	}
	if de.SMTPCode != 550 {
		t.Errorf("SMTPCode = %d, want 550", de.SMTPCode)
	}
	if de.EnhancedCode != [3]int{5, 6, 0} {
		t.Errorf("EnhancedCode = %v, want {5 6 0}", de.EnhancedCode)
	}
}

func TestClassifyTransport(t *testing.T) {
	t.Run("deadline exceeded", func(t *testing.T) {
		de := classifyTransport(context.DeadlineExceeded, 2)
		if !de.Temporary {
			t.Error("Temporary = false, want true")
		}
		if de.SMTPCode != 451 {
			t.Errorf("SMTPCode = %d, want 451", de.SMTPCode)
		}
		if de.EnhancedCode != [3]int{4, 4, 1} {
			t.Errorf("EnhancedCode = %v, want {4 4 1}", de.EnhancedCode)
		}
		if de.Attempts != 2 {
			t.Errorf("Attempts = %d, want 2", de.Attempts)
		}
	})

	t.Run("generic transport error", func(t *testing.T) {
		de := classifyTransport(errors.New("connection refused"), 1)
		if !de.Temporary {
			t.Error("Temporary = false, want true")
		}
		if de.SMTPCode != 451 {
			t.Errorf("SMTPCode = %d, want 451", de.SMTPCode)
		}
		if de.EnhancedCode != [3]int{4, 4, 1} {
			t.Errorf("EnhancedCode = %v, want {4 4 1}", de.EnhancedCode)
		}
	})
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   int
		want   bool
	}{
		{"429 retryable", 429, 0, true},
		{"500 retryable", 500, 0, true},
		{"599 retryable", 599, 0, true},
		{"400 not retryable", 400, 0, false},
		{"401 not retryable", 401, 0, false},
		{"403 not retryable", 403, 0, false},
		{"404 not retryable", 404, 0, false},
		{"other 4xx not retryable", 418, 0, false},
		{"10004 retryable via code", 0, 10004, true},
		{"10101 not retryable via code", 0, 10101, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryable(tt.status, tt.code)
			if got != tt.want {
				t.Errorf("isRetryable(%d, %d) = %v, want %v", tt.status, tt.code, got, tt.want)
			}
		})
	}
}

func TestClassify_NeverLeaksSecret(t *testing.T) {
	const secret = "sk-super-secret-token-xyz"

	de := classifyHTTP(400, 10001, "message mentioning "+secret, 1)
	if strings.Contains(de.Error(), secret) {
		t.Errorf("Error() leaked secret: %s", de.Error())
	}
	if strings.Contains(de.Reason, secret) {
		t.Errorf("Reason leaked secret: %s", de.Reason)
	}

	transportErr := errors.New("dial tcp: connection refused, token=" + secret)
	de2 := classifyTransport(transportErr, 1)
	if strings.Contains(de2.Error(), secret) {
		t.Errorf("Error() leaked secret: %s", de2.Error())
	}
	if strings.Contains(de2.Reason, secret) {
		t.Errorf("Reason leaked secret: %s", de2.Reason)
	}
}
