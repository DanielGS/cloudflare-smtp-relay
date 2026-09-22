// Package health exposes the relay's HTTP health endpoint and the probe
// used to check it from the outside, such as a container HEALTHCHECK.
//
// The endpoint intentionally carries no configuration, credential or
// internal-state detail: it only answers whether the process is alive.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// readHeaderTimeout bounds how long the server waits to read request
// headers, so a slow or malicious client cannot hold a connection open
// indefinitely. gosec flags an http.Server with no such timeout set.
const readHeaderTimeout = 5 * time.Second

// healthBody is the exact JSON body returned by a healthy /health response.
const healthBody = `{"status":"ok"}`

// statusPayload is the shape Check decodes the response body into.
type statusPayload struct {
	Status string `json:"status"`
}

// Handler returns the HTTP handler serving the health endpoint. It is
// exported so it can be exercised in tests without binding a real port.
//
// GET /health replies 200 with a JSON body of {"status":"ok"}. Any other
// path replies 404, and any other method on /health replies 405. No
// configuration or credential is ever exposed in the response.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(healthBody))
	})
	return mux
}

// Server serves the health endpoint over HTTP.
type Server struct {
	inner  *http.Server
	logger *slog.Logger

	mu   sync.Mutex
	addr string
}

// New builds a health Server listening on addr once Serve or
// ListenAndServe is called. logger is currently unused by the handler
// itself (the endpoint never logs request details that could leak
// configuration) but is kept so future operational logging has a seam.
func New(addr string, logger *slog.Logger) *Server {
	return &Server{
		inner: &http.Server{
			Addr:              addr,
			Handler:           Handler(),
			ReadHeaderTimeout: readHeaderTimeout,
		},
		logger: logger,
	}
}

// Serve accepts connections on l and serves the health endpoint until the
// listener is closed or Shutdown is called. It records l's address so
// Addr reports the actual bound address, which matters when addr was
// ":0" or "127.0.0.1:0".
func (s *Server) Serve(l net.Listener) error {
	s.mu.Lock()
	s.addr = l.Addr().String()
	s.mu.Unlock()

	return s.inner.Serve(l)
}

// ListenAndServe binds the configured address and serves the health
// endpoint until Shutdown is called.
func (s *Server) ListenAndServe() error {
	return s.inner.ListenAndServe()
}

// Shutdown gracefully stops the server, waiting for in-flight requests to
// finish or ctx to be done, whichever comes first.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.inner.Shutdown(ctx)
}

// Addr returns the address the server is bound to, once Serve or
// ListenAndServe has started listening. It is empty before that.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

// Check performs the probe used by the container HEALTHCHECK: it issues a
// GET against url and returns nil only when the response is a 200 whose
// body decodes to {"status":"ok"}.
func Check(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("health: build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("health: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health: unexpected status %d", resp.StatusCode)
	}

	var payload statusPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("health: decode response: %w", err)
	}
	if payload.Status != "ok" {
		return fmt.Errorf("health: unexpected status field %q", payload.Status)
	}
	return nil
}
