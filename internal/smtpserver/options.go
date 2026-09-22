// Package smtpserver implements the relay's SMTP submission server: an
// authenticated go-smtp backend that parses each accepted message and hands
// it to an email.Sender. It knows nothing about Cloudflare, HTTP or process
// configuration; those concerns live in cmd/relay and internal/cloudflare.
package smtpserver

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/dagase/cloudflare-smtp-relay/internal/email"
)

// defaultDomain is advertised in the SMTP greeting when Options.Domain is
// left empty.
const defaultDomain = "localhost"

// Options configures a Server. It is an explicit, package-owned struct
// rather than *config.Config so smtpserver stays testable and decoupled
// from the process configuration package; cmd/relay maps config.Config
// into an Options value.
type Options struct {
	// Addr is the address the server listens on when ListenAndServe is
	// used. It is not consulted by Serve, which accepts an existing
	// net.Listener instead.
	Addr string

	// Domain is the EHLO domain advertised to clients. It defaults to
	// "localhost" when empty.
	Domain string

	// Username and Password are the single set of credentials the relay
	// accepts over AUTH PLAIN and AUTH LOGIN. Both are required.
	Username string
	Password string

	// MaxMessageBytes caps the size of an accepted DATA payload. It must
	// be positive.
	MaxMessageBytes int64
	// MaxRecipients caps the number of RCPT TO commands per transaction.
	// It must be positive.
	MaxRecipients int

	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	// TLSConfig is optional. When nil, the server is assumed to run on a
	// plaintext port inside a private network (the intended Docker
	// deployment), so AllowInsecureAuth is enabled to permit AUTH PLAIN
	// and AUTH LOGIN without STARTTLS; see New for that decision.
	TLSConfig *tls.Config

	// Policy decides which envelope senders may use the relay. A nil
	// Policy defaults to an allow-all policy (see email.NewSenderPolicy).
	Policy *email.SenderPolicy

	// Sender is the outbound delivery port. It is required.
	Sender email.Sender

	// Logger receives one structured record per handled message. It
	// defaults to slog.Default() when nil.
	Logger *slog.Logger

	// NewID generates the relay-internal Message.ID for each accepted
	// message. It defaults to a random UUID generator.
	NewID func() string

	// Now is the time source used for timestamps and duration
	// measurement. It defaults to time.Now.
	Now func() time.Time
}

// errors returned by New when a required option is missing or invalid.
var (
	ErrSenderRequired         = errors.New("smtpserver: Sender is required")
	ErrUsernameRequired       = errors.New("smtpserver: Username is required")
	ErrPasswordRequired       = errors.New("smtpserver: Password is required")
	ErrMaxMessageBytesInvalid = errors.New("smtpserver: MaxMessageBytes must be positive")
	ErrMaxRecipientsInvalid   = errors.New("smtpserver: MaxRecipients must be positive")
)

// validate checks the required options and fills in defaults for the
// optional ones, returning the effective Options.
func (o Options) validate() (Options, error) {
	if o.Sender == nil {
		return Options{}, ErrSenderRequired
	}
	if o.Username == "" {
		return Options{}, ErrUsernameRequired
	}
	if o.Password == "" {
		return Options{}, ErrPasswordRequired
	}
	if o.MaxMessageBytes <= 0 {
		return Options{}, ErrMaxMessageBytesInvalid
	}
	if o.MaxRecipients <= 0 {
		return Options{}, ErrMaxRecipientsInvalid
	}

	if o.Domain == "" {
		o.Domain = defaultDomain
	}
	if o.Policy == nil {
		o.Policy = email.NewSenderPolicy(nil)
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.NewID == nil {
		o.NewID = func() string { return uuid.NewString() }
	}
	if o.Now == nil {
		o.Now = time.Now
	}

	return o, nil
}
