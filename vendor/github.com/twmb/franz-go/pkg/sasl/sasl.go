// Package sasl specifies interfaces that any SASL authentication must provide
// to interop with Kafka SASL.
package sasl

import "context"

// Session is an authentication session.
type Session interface {
	// Challenge is called with a server response. This must return
	// if the authentication is done, or, if not, the next message
	// to send. If the authentication is done, this can return an
	// additional last message to be written (for which we will not
	// read a response).
	//
	// Returning an error stops the authentication flow.
	Challenge([]byte) (bool, []byte, error)
}

// Mechanism authenticates with SASL.
type Mechanism interface {
	// Name is the name of this SASL authentication mechanism.
	Name() string

	// Authenticate initializes an authentication session to the provided
	// host:port. If the mechanism is a client-first authentication
	// mechanism, this also returns the first message to write.
	//
	// If initializing a session fails, this can return an error to stop
	// the authentication flow.
	//
	// The provided context can be used through the duration of the session.
	Authenticate(ctx context.Context, host string) (Session, []byte, error)
}

// ClosingMechanism is an optional interface for SASL mechanisms. Implementing
// this interface signals that the mechanism should be closed if it will never
// be used again.
type ClosingMechanism interface {
	// Close permanently closes a mechanism.
	Close()
}
