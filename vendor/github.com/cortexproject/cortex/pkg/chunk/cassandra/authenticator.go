package cassandra

import (
	"fmt"

	"github.com/gocql/gocql"
)

// CustomPasswordAuthenticator provides the default behaviour for Username/Password authentication with
// Cassandra while allowing users to specify a non-default Authenticator to accept.
type CustomPasswordAuthenticator struct {
	ApprovedAuthenticators []string
	Username               string
	Password               string
}

func (p CustomPasswordAuthenticator) approve(authenticator string) bool {
	for _, s := range p.ApprovedAuthenticators {
		if authenticator == s {
			return true
		}
	}
	return false
}

// Challenge verifies the name of the authenticator and formats the provided username and password
// into a response
func (p CustomPasswordAuthenticator) Challenge(req []byte) ([]byte, gocql.Authenticator, error) {
	if !p.approve(string(req)) {
		return nil, nil, fmt.Errorf("unexpected authenticator %q", req)
	}
	resp := make([]byte, 2+len(p.Username)+len(p.Password))
	resp[0] = 0
	copy(resp[1:], p.Username)
	resp[len(p.Username)+1] = 0
	copy(resp[2+len(p.Username):], p.Password)
	return resp, nil, nil
}

// Success returns nil by default, identical to the default PasswordAuthenticator
func (p CustomPasswordAuthenticator) Success(data []byte) error {
	return nil
}
