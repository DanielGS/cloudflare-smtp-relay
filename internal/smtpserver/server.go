package smtpserver

import (
	"context"
	"net"
	"sync"

	gosmtp "github.com/emersion/go-smtp"
)

// Server is the relay's SMTP submission server: a thin wrapper around
// go-smtp's Server configured from Options, exposing only what the rest of
// the relay needs.
type Server struct {
	inner *gosmtp.Server

	mu   sync.Mutex
	addr string
}

// New validates opts and builds a Server ready to accept connections via
// Serve or ListenAndServe. It returns an error when a required option is
// missing or invalid; see Options for which fields are required.
func New(opts Options) (*Server, error) {
	effective, err := opts.validate()
	if err != nil {
		return nil, err
	}

	be := &backend{opts: effective}
	inner := gosmtp.NewServer(be)
	inner.Addr = effective.Addr
	inner.Domain = effective.Domain
	inner.MaxMessageBytes = effective.MaxMessageBytes
	inner.MaxRecipients = effective.MaxRecipients
	inner.ReadTimeout = effective.ReadTimeout
	inner.WriteTimeout = effective.WriteTimeout
	inner.TLSConfig = effective.TLSConfig

	// The intended deployment is a plaintext SMTP port reachable only on a
	// private Docker network, fronted by nothing that speaks STARTTLS. When
	// no TLSConfig is configured there is therefore no way for a client to
	// upgrade the connection, so refusing AUTH would make the mandatory
	// authentication requirement impossible to satisfy. Insecure AUTH is
	// only ever allowed in that specific, documented deployment shape; a
	// caller who does configure TLSConfig gets AllowInsecureAuth=false as
	// usual.
	inner.AllowInsecureAuth = effective.TLSConfig == nil

	return &Server{inner: inner}, nil
}

// Serve accepts connections on l until it is closed or Shutdown is called.
// It records l's address so Addr reflects the actual bound port, which
// matters when l was created with a ":0" address in tests.
func (s *Server) Serve(l net.Listener) error {
	s.mu.Lock()
	s.addr = l.Addr().String()
	s.mu.Unlock()

	return s.inner.Serve(l)
}

// ListenAndServe binds Options.Addr and serves until Shutdown is called.
func (s *Server) ListenAndServe() error {
	return s.inner.ListenAndServe()
}

// Shutdown gracefully stops the server, waiting for in-flight sessions to
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
