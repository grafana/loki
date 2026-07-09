package wire

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/atomic"
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

	localToRemote := make(chan Frame, 128)
	remoteToLocal := make(chan Frame, 128)

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

	metrics atomic.Pointer[Metrics]
}

var _ Conn = (*localConn)(nil)

func (c *localConn) setMetrics(metrics *Metrics) {
	c.metrics.Store(metrics)
}

func (c *localConn) transport() transport { return transportLocal }

func (c *localConn) Send(ctx context.Context, frame Frame) error {
	return c.sendFrame(ctx, frame, sendModeInternal)
}

func (c *localConn) sendFrame(ctx context.Context, frame Frame, sendMode sendMode) error {
	metrics := c.metrics.Load()
	timer := metrics.startFrameSend(transportLocal, frame, sendMode)
	defer timer.Done(outcomeNone)

	return c.send(ctx, frame)
}

func (c *localConn) send(ctx context.Context, frame Frame) error {
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
	start := time.Now()

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
		if metrics := c.metrics.Load(); metrics != nil {
			metrics.observeFrameReceive(phaseLocalChannelReceive, transportLocal, frame, sendModeInternal, time.Since(start))
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

// LocalDialer is a [Dialer] that can open local connections to one or more
// [Local] listeners.
type LocalDialer struct {
	listeners map[net.Addr]*Local
}

var _ Dialer = (*LocalDialer)(nil)

// NewLocalDialer creates a new [LocalDialer] that can open connections to the
// provided listeners.
func NewLocalDialer(listeners ...*Local) *LocalDialer {
	listenerMap := make(map[net.Addr]*Local, len(listeners))
	for _, listener := range listeners {
		listenerMap[listener.Addr()] = listener
	}
	return &LocalDialer{listeners: listenerMap}
}

// Dial opens a connection to the provided address. Returns an error if the
// address was not registered.
func (d *LocalDialer) Dial(ctx context.Context, from, to net.Addr) (Conn, error) {
	listener, ok := d.listeners[to]
	if !ok {
		return nil, fmt.Errorf("address %s not found", to)
	}
	return listener.DialFrom(ctx, from)
}

// LocalNetwork is a shared in-process routing table that lets multiple [Local]
// listeners discover and dial each other without a real network stack.
//
// Use [NewLocalNetwork] to create a network, [LocalNetwork.Register] to add an
// existing listener (e.g. a scheduler's), and [LocalNetwork.NewListener] to
// allocate uniquely-addressed listeners for workers. All peers share the
// [Dialer] returned by [LocalNetwork.Dialer] and can therefore reach one
// another, enabling multi-worker in-process deployments.
type LocalNetwork struct {
	mu      sync.RWMutex
	peers   map[net.Addr]*Local
	counter atomic.Int64
}

// NewLocalNetwork returns an empty [LocalNetwork].
func NewLocalNetwork() *LocalNetwork {
	return &LocalNetwork{peers: make(map[net.Addr]*Local)}
}

// NewListener allocates a new [Local] listener with a unique address derived
// from prefix (e.g. "worker" yields addresses "worker-1", "worker-2", …) and
// registers it with the network.
func (n *LocalNetwork) NewListener(prefix string) *Local {
	id := n.counter.Add(1)
	l := &Local{Address: localAddr(fmt.Sprintf("%s-%d", prefix, id))}
	n.mu.Lock()
	n.peers[l.Address] = l
	n.mu.Unlock()
	return l
}

// Register adds l to the network. Panics if a listener with the same address
// is already registered.
func (n *LocalNetwork) Register(l *Local) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, exists := n.peers[l.Address]; exists {
		panic(fmt.Sprintf("wire: LocalNetwork already has a listener for address %s", l.Address))
	}
	n.peers[l.Address] = l
}

// Dialer returns a [Dialer] that performs live lookups against the network and
// can reach any listener registered at the time [Dialer.Dial] is called.
func (n *LocalNetwork) Dialer() Dialer {
	return &networkLocalDialer{n: n}
}

type networkLocalDialer struct{ n *LocalNetwork }

func (d *networkLocalDialer) Dial(ctx context.Context, from, to net.Addr) (Conn, error) {
	d.n.mu.RLock()
	l, ok := d.n.peers[to]
	d.n.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("wire: no local peer at address %s", to)
	}
	return l.DialFrom(ctx, from)
}
