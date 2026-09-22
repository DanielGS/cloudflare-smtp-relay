package smtpserver

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"

	"github.com/dagase/cloudflare-smtp-relay/internal/email"
	"github.com/dagase/cloudflare-smtp-relay/internal/logging"
)

// sendTimeout bounds how long a single Sender.Send call may take. It is
// applied to the context.Background()-derived context built for each
// accepted DATA command, since go-smtp's Session interface carries no
// context of its own.
const sendTimeout = 30 * time.Second

// errAuthRequired is returned by Mail when the client has not yet
// authenticated. 530 with enhanced code {5,7,0} tells the client
// authentication is required before the command can proceed.
var errAuthRequired = &gosmtp.SMTPError{
	Code:         530,
	EnhancedCode: gosmtp.EnhancedCode{5, 7, 0},
	Message:      "Authentication required",
}

// backend adapts Options into a gosmtp.Backend, producing one session per
// connection.
type backend struct {
	opts Options
}

func (b *backend) NewSession(_ *gosmtp.Conn) (gosmtp.Session, error) {
	return &session{opts: &b.opts}, nil
}

// session holds the per-connection SMTP transaction state. A session is
// reused across multiple transactions on the same connection (MAIL FROM /
// RCPT TO / DATA, then possibly another MAIL FROM), so Reset must fully
// clear the envelope to prevent one transaction leaking into the next.
type session struct {
	opts *Options

	authenticated bool
	from          string
	recipients    []string
}

var (
	_ gosmtp.Session     = (*session)(nil)
	_ gosmtp.AuthSession = (*session)(nil)
)

// AuthMechanisms advertises PLAIN and LOGIN. Authentication is mandatory:
// there is no anonymous path into MAIL FROM.
func (s *session) AuthMechanisms() []string {
	return []string{sasl.Plain, sasl.Login}
}

// Auth returns the sasl.Server for the requested mechanism.
func (s *session) Auth(mech string) (sasl.Server, error) {
	switch mech {
	case sasl.Plain:
		return sasl.NewPlainServer(func(_ /* identity */, username, password string) error {
			return s.authenticate(username, password)
		}), nil
	case sasl.Login:
		return newLoginServer(s.authenticate), nil
	default:
		return nil, gosmtp.ErrAuthUnknownMechanism
	}
}

// authenticate checks username and password against the configured
// credentials using a constant-time comparison for both fields, and
// compares both even when the username has already failed, so a client
// cannot use response timing to learn which field was wrong.
func (s *session) authenticate(username, password string) error {
	validUser := subtle.ConstantTimeCompare([]byte(username), []byte(s.opts.Username)) == 1
	validPass := subtle.ConstantTimeCompare([]byte(password), []byte(s.opts.Password)) == 1
	if !validUser || !validPass {
		return gosmtp.ErrAuthFailed
	}
	s.authenticated = true
	return nil
}

// Reset discards the current transaction's envelope state. It is called by
// go-smtp on RSET, after DATA completes, and before a session is reused
// for a new transaction; it never resets s.authenticated, since AUTH is a
// per-connection property, not a per-transaction one.
func (s *session) Reset() {
	s.from = ""
	s.recipients = nil
}

// Logout clears the envelope state and ends the session.
func (s *session) Logout() error {
	s.Reset()
	return nil
}

// Mail starts a new transaction. It rejects an unauthenticated client and
// a sender whose domain is not allowed by the configured policy.
func (s *session) Mail(from string, _ *gosmtp.MailOptions) error {
	if !s.authenticated {
		return errAuthRequired
	}

	addr := email.Address{Address: from}
	if err := s.opts.Policy.Check(addr); err != nil {
		if errors.Is(err, email.ErrSenderNotAllowed) {
			return &gosmtp.SMTPError{
				Code:         550,
				EnhancedCode: gosmtp.EnhancedCode{5, 7, 1},
				Message:      fmt.Sprintf("sender domain %q is not allowed", addr.Domain()),
			}
		}
		return &gosmtp.SMTPError{
			Code:         550,
			EnhancedCode: gosmtp.EnhancedCode{5, 7, 1},
			Message:      "sender rejected",
		}
	}

	s.from = from
	s.recipients = nil
	return nil
}

// Rcpt accumulates a recipient for the current transaction. go-smtp itself
// enforces MaxRecipients before calling this method, so no limit check is
// duplicated here.
func (s *session) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	s.recipients = append(s.recipients, to)
	return nil
}

// Data reads the message body, parses it, assigns its relay-internal ID,
// and hands it to the configured Sender. It emits exactly one
// logging.LogDelivery record per call, regardless of outcome.
func (s *session) Data(r io.Reader) error {
	start := s.opts.Now()

	var (
		result       = "rejected"
		deliveryErr  error
		messageID    string
		rfcMessageID string
		from         string
		to           []string
		subject      string
		httpStatus   int
		providerCode int
		attempts     int
	)

	defer func() {
		logging.LogDelivery(s.opts.Logger, logging.Delivery{
			MessageID:    messageID,
			RFCMessageID: rfcMessageID,
			From:         from,
			To:           to,
			Subject:      subject,
			Result:       result,
			Duration:     s.opts.Now().Sub(start),
			HTTPStatus:   httpStatus,
			ProviderCode: providerCode,
			Attempts:     attempts,
			Err:          deliveryErr,
		})
	}()

	// go-smtp already wraps r in a reader bounded by Server.MaxMessageBytes
	// (see New, which sets it from Options.MaxMessageBytes), so a client
	// that ignores the advertised SIZE cannot force unbounded buffering
	// here: reading past the limit surfaces gosmtp.ErrDataTooLarge.
	raw, err := io.ReadAll(r)
	if err != nil {
		deliveryErr = err
		if errors.Is(err, gosmtp.ErrDataTooLarge) {
			result = "rejected"
			return err
		}
		result = "deferred"
		return &gosmtp.SMTPError{
			Code:         451,
			EnhancedCode: gosmtp.EnhancedCode{4, 3, 0},
			Message:      "error reading message",
		}
	}

	msg, perr := email.Parse(raw, s.from, s.recipients)
	if perr != nil {
		deliveryErr = perr
		result = "rejected"
		return &gosmtp.SMTPError{
			Code:         550,
			EnhancedCode: gosmtp.EnhancedCode{5, 6, 0},
			Message:      "message could not be parsed",
		}
	}
	msg.ID = s.opts.NewID()

	messageID = msg.ID
	rfcMessageID = msg.RFCMessageID
	from = msg.From.Address
	to = msg.AllRecipients()
	subject = msg.Subject

	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()

	res, serr := s.opts.Sender.Send(ctx, msg)
	if serr != nil {
		deliveryErr = serr
		if de, ok := email.AsDeliveryError(serr); ok {
			httpStatus = de.HTTPStatus
			providerCode = de.ProviderCode
			attempts = de.Attempts
			if de.Temporary {
				result = "deferred"
			} else {
				result = "rejected"
			}
			return &gosmtp.SMTPError{
				Code:         de.SMTPCode,
				EnhancedCode: gosmtp.EnhancedCode(de.EnhancedCode),
				Message:      de.Reason,
			}
		}

		result = "deferred"
		return &gosmtp.SMTPError{
			Code:         451,
			EnhancedCode: gosmtp.EnhancedCode{4, 0, 0},
			Message:      "internal error",
		}
	}

	result = "sent"
	if res != nil {
		httpStatus = res.HTTPStatus
		attempts = res.Attempts
	}
	return nil
}
