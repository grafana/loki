// Package wire provides the wire protocol for how peers scheduler peers
// communicate.
package wire

import (
	"context"
	"errors"
	"net"
)

// ErrConnClosed indicates a closed connection between peers.
var ErrConnClosed = errors.New("connection closed")

// Listener waits for incoming connections from scheduler peers.
type Listener interface {
	// Accept waits for and returns the next connection to the listener. Accept
	// returns an error if the context is canceled or if the listener is closed.
	Accept(ctx context.Context) (Conn, error)

	// Close closes the listener. Any blocked Accept operations will be
	// unblocked and return errors.
	Close(ctx context.Context) error

	// Addr returns the listener's advertised network address. Peers use this
	// address to connect to the listener.
	Addr() net.Addr
}

// A Dialer establishes connections to scheduler peers.
type Dialer interface {
	// Dial connects to the scheduler peer at the provided "to" address. The
	// "from" address is used to establish the address that can be used to
	// connect back to the caller. Dial returns an error if the context is
	// canceled or if the connection cannot be established.
	Dial(ctx context.Context, from, to net.Addr) (Conn, error)
}

// Conn is a communication stream between two peers.
type Conn interface {
	// Send sends the provided Frame to the peer. Send blocks until the Frame
	// has been sent to the peer, but does not wait for the peer to acknowledge
	// receipt of the Frame.
	//
	// Send returns an error if the context is canceled or if the connection is
	// closed.
	Send(context.Context, Frame) error

	// Recv receives the next Frame from the peer. Recv blocks until a Frame is
	// available. Recv returns an error if the context is canceled or if the
	// connection is closed.
	//
	// Callers should take care to avoid long periods of where there is not an
	// active call to Recv to avoid blocking the peer's Send call.
	Recv(context.Context) (Frame, error)

	// Close closes the Conn. Close may be called by either side of the
	// connection. After the connection has been closed, calls to Send or Recv
	// return [ErrConnClosed].
	Close() error

	// LocalAddr returns the address of the local side of the connection.
	LocalAddr() net.Addr

	// RemoteAddr returns the address of the remote side of the connection.
	RemoteAddr() net.Addr
}
