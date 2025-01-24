// Package plain provides PLAIN sasl authentication as specified in RFC4616.
package plain

import (
	"context"
	"errors"

	"github.com/twmb/franz-go/pkg/sasl"
)

// Auth contains information for authentication.
type Auth struct {
	// Zid is an optional authorization ID to use in authenticating.
	Zid string

	// User is username to use for authentication.
	User string

	// Pass is the password to use for authentication.
	Pass string

	_ struct{} // require explicit field initialization
}

// AsMechanism returns a sasl mechanism that will use 'a' as credentials for
// all sasl sessions.
//
// This is a shortcut for using the Plain function and is useful when you do
// not need to live-rotate credentials.
func (a Auth) AsMechanism() sasl.Mechanism {
	return Plain(func(context.Context) (Auth, error) {
		return a, nil
	})
}

// Plain returns a sasl mechanism that will call authFn whenever sasl
// authentication is needed. The returned Auth is used for a single session.
func Plain(authFn func(context.Context) (Auth, error)) sasl.Mechanism {
	return plain(authFn)
}

type plain func(context.Context) (Auth, error)

func (plain) Name() string { return "PLAIN" }
func (fn plain) Authenticate(ctx context.Context, _ string) (sasl.Session, []byte, error) {
	auth, err := fn(ctx)
	if err != nil {
		return nil, nil, err
	}
	if auth.User == "" || auth.Pass == "" {
		return nil, nil, errors.New("PLAIN user and pass must be non-empty")
	}
	return session{}, []byte(auth.Zid + "\x00" + auth.User + "\x00" + auth.Pass), nil
}

type session struct{}

func (session) Challenge([]byte) (bool, []byte, error) {
	return true, nil, nil
}
