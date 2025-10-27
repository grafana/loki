package wire

import (
	"context"
	"net"
	"sync"
)

var (
	// LocalScheduler is the address of the local scheduler when using the
	// [Local] listener.
	LocalScheduler net.Addr = localAddr("scheduler")

	// LocalWorker is the address of the local worker when using the
	// [Local] listener.
	LocalWorker net.Addr = localAddr("worker")
)

type localAddr string

var _ net.Addr = localAddr("")

func (addr localAddr) Network() string { return "local" }
func (addr localAddr) String() string  { return string(addr) }

// Local is a [Listener] that accepts connections from the local process without
// using the network.
type Local struct {
	// Address to broadcast when connecting to peers. Should be [LocalScheduler]
	// for the scheduler and [LocalWorker] for the worker.
	Address net.Addr

	incoming chan *localConn // Incoming connections to this Local
	alive    context.Context
	close    context.CancelFunc

	initOnce  sync.Once
	closeOnce sync.Once
}

var _ Listener = (*Local)(nil)

// DialFrom dials the local listener, blocking until the connection is accepted
// or the context is canceled. Returns the caller's connection.
func (l *Local) DialFrom(ctx context.Context, from net.Addr) (Conn, error) {
	l.lazyInit()

	localToRemote := make(chan Frame)
	remoteToLocal := make(chan Frame)

	// Generate a context to share between the streams for whether the
	// connection is still open.
	//
	// This is attached to the listener's context which closes all connections
	// when the listener is closed.
	alive, closeConn := context.WithCancel(l.alive)

	localStream := &localConn{
		alive: alive,
		close: closeConn,

		localAddr:  from,
		remoteAddr: l.Address,

		write: localToRemote,
		read:  remoteToLocal,
	}

	remoteStream := &localConn{
		alive: alive,
		close: closeConn,

		localAddr:  l.Address,
		remoteAddr: from,

		write: remoteToLocal,
		read:  localToRemote,
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-alive.Done():
		return nil, net.ErrClosed
	case l.incoming <- remoteStream:
		return localStream, nil
	}
}

func (l *Local) lazyInit() {
	l.initOnce.Do(func() {
		l.incoming = make(chan *localConn)
		l.alive, l.close = context.WithCancel(context.Background())
	})
}

// Accept waits for and returns the next connection to the listener.
func (l *Local) Accept(ctx context.Context) (Conn, error) {
	l.lazyInit()

	select {
	case <-l.alive.Done():
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	case conn := <-l.incoming:
		return conn, nil
	}
}

// Close closes the listener. Any blocked Accept operations will be unblocked and return errors.
func (l *Local) Close(_ context.Context) error {
	l.lazyInit()

	l.closeOnce.Do(func() {
		close(l.incoming)
		l.close()
	})

	return nil
}

// Addr returns the listener's advertised address.
func (l *Local) Addr() net.Addr { return l.Address }

type localConn struct {
	alive context.Context
	close context.CancelFunc

	localAddr, remoteAddr net.Addr

	write chan<- Frame
	read  <-chan Frame
}

var _ Conn = (*localConn)(nil)

func (c *localConn) Send(ctx context.Context, frame Frame) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.alive.Done(): // Conn closed
		return ErrConnClosed
	case c.write <- frame:
		return nil
	}
}

func (c *localConn) Recv(ctx context.Context) (Frame, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.alive.Done(): // Conn closed
		return nil, ErrConnClosed
	case frame, ok := <-c.read:
		if !ok {
			// Close our end, in case it's not already closed.
			_ = c.Close()
			return nil, ErrConnClosed
		}
		return frame, nil
	}
}

func (c *localConn) Close() error {
	c.close()
	return nil
}

// LocalAddr returns the address of the local side of the connection.
func (c *localConn) LocalAddr() net.Addr { return c.localAddr }

// RemoteAddr returns the address of the remote side of the connection.
func (c *localConn) RemoteAddr() net.Addr { return c.remoteAddr }
