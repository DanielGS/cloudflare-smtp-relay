package smtpserver

import (
	"github.com/emersion/go-sasl"
)

// loginServer implements the server side of the LOGIN SASL mechanism,
// which github.com/emersion/go-sasl does not provide (it only ships a
// LOGIN client). It follows the classic two-round-trip exchange: a
// "Username:" challenge followed by a "Password:" challenge.
type loginServer struct {
	state        loginState
	username     string
	authenticate func(username, password string) error
}

type loginState int

const (
	loginStateUsername loginState = iota
	loginStatePassword
	loginStateDone
)

// newLoginServer builds a sasl.Server for the LOGIN mechanism that calls
// authenticate once both the username and password have been collected.
func newLoginServer(authenticate func(username, password string) error) sasl.Server {
	return &loginServer{authenticate: authenticate}
}

// Next implements sasl.Server.
func (l *loginServer) Next(response []byte) (challenge []byte, done bool, err error) {
	switch l.state {
	case loginStateUsername:
		if response == nil {
			// No initial response was supplied; prompt for the username.
			return []byte("Username:"), false, nil
		}
		l.username = string(response)
		l.state = loginStatePassword
		return []byte("Password:"), false, nil

	case loginStatePassword:
		password := string(response)
		l.state = loginStateDone
		return nil, true, l.authenticate(l.username, password)

	default:
		return nil, true, sasl.ErrUnexpectedClientResponse
	}
}
