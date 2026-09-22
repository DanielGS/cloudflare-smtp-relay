package health_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dagase/cloudflare-smtp-relay/internal/health"
)

// TestHandlerHealthOK verifies that GET /health returns 200, the exact JSON
// body and a JSON content type, without exposing any configuration.
func TestHandlerHealthOK(t *testing.T) {
	srv := httptest.NewServer(health.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body %q: %v", body, err)
	}
	if len(payload) != 1 || payload["status"] != "ok" {
		t.Fatalf("body = %q, want exactly {\"status\":\"ok\"}", body)
	}
}

// TestHandlerWrongPath verifies that any path other than /health returns 404.
func TestHandlerWrongPath(t *testing.T) {
	srv := httptest.NewServer(health.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/does-not-exist")
	if err != nil {
		t.Fatalf("GET /does-not-exist: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// TestHandlerWrongMethod verifies that a non-GET method on /health returns 405.
func TestHandlerWrongMethod(t *testing.T) {
	srv := httptest.NewServer(health.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/health", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("POST /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

// TestServerServeAndAddr verifies the Server type binds to a loopback
// listener, serves the health handler, exposes its bound address through
// Addr, and shuts down cleanly.
func TestServerServeAndAddr(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := health.New("127.0.0.1:0", logger)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(l)
	}()

	// Wait until Addr() reflects the bound listener.
	deadline := time.Now().Add(2 * time.Second)
	for srv.Addr() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	addr := srv.Addr()
	if addr == "" {
		t.Fatal("Addr() is empty after Serve")
	}

	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("GET health: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if err := <-errCh; err != nil && err != http.ErrServerClosed {
		t.Fatalf("Serve returned unexpected error: %v", err)
	}
}

// TestCheckSuccess verifies that Check succeeds against a live server
// exposing the health handler.
func TestCheckSuccess(t *testing.T) {
	srv := httptest.NewServer(health.Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := health.Check(ctx, srv.URL+"/health"); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
}

// TestCheckFailureOnServerError verifies that Check fails against a server
// returning a 500 status.
func TestCheckFailureOnServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := health.Check(ctx, srv.URL+"/health"); err == nil {
		t.Fatal("Check() = nil, want an error for a 500 response")
	}
}
