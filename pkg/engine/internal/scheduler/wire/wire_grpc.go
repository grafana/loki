package wire

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/go-kit/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/grafana/loki/v3/pkg/engine/internal/proto/wirepb"
)

// peerAddressMetadataKey carries the advertised callback address of the dialing
// peer. It is the gRPC-metadata equivalent of [peerAddressHeader] used by the
// HTTP/2 transport.
const peerAddressMetadataKey = "loki-peer-address"

// frameStream is the common subset of the generated
// [wirepb.WireTransport_ConnectClient] and [wirepb.WireTransport_ConnectServer]
// streams used by [grpcConn]. Both send and receive typed [wirepb.Frame]
// messages, so marshaling is handled by the default gRPC codec (which dispatches
// to the message's own gogo Marshal/Unmarshal, preserving customtype fields such
// as the ULID).
type frameStream interface {
	Context() context.Context
	Send(*wirepb.Frame) error
	Recv() (*wirepb.Frame, error)
}

// grpcConn implements [Conn] over a gRPC bidirectional stream.
type grpcConn struct {
	stream frameStream
	codec  *protobufCodec

	localAddr  net.Addr
	remoteAddr net.Addr

	// cancel aborts the stream, unblocking the read loop. It is set for the
	// dialing (client) side; on the server side the stream ends when the
	// handler returns.
	cancel context.CancelFunc

	writeMu   sync.Mutex
	closeOnce sync.Once
	closed    chan struct{}

	incomingCh chan incomingFrame
}

var _ Conn = (*grpcConn)(nil)

func newGRPCConn(stream frameStream, localAddr, remoteAddr net.Addr) *grpcConn {
	return &grpcConn{
		stream:     stream,
		codec:      DefaultFrameCodec,
		localAddr:  localAddr,
		remoteAddr: remoteAddr,
		closed:     make(chan struct{}),
		incomingCh: make(chan incomingFrame),
	}
}

// readLoop decodes frames from the stream into incomingCh until the stream
// errors (including io.EOF) or the connection is closed. It mirrors
// http2Conn.readLoop so that Recv stays context-cancellable.
func (c *grpcConn) readLoop() {
	for {
		pb, err := c.stream.Recv()

		var incoming incomingFrame
		if err != nil {
			incoming = incomingFrame{err: translateGRPCError(err)}
		} else {
			frame, convErr := c.codec.frameFromPbFrame(pb)
			incoming = incomingFrame{frame: frame, err: convErr}
		}

		select {
		case <-c.closed:
			return
		case c.incomingCh <- incoming:
			if err != nil {
				return
			}
		}
	}
}

// Send sends a frame over the connection.
func (c *grpcConn) Send(ctx context.Context, frame Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	select {
	case <-c.closed:
		return ErrConnClosed
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	pb, err := c.codec.frameToPbFrame(frame)
	if err != nil {
		return fmt.Errorf("convert frame: %w", err)
	}

	if err := c.stream.Send(pb); err != nil {
		if translateGRPCError(err) == ErrConnClosed {
			return ErrConnClosed
		}
		return fmt.Errorf("send frame: %w", err)
	}
	return nil
}

// Recv receives the next frame from the connection.
func (c *grpcConn) Recv(ctx context.Context) (Frame, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, ErrConnClosed
	case f := <-c.incomingCh:
		return f.frame, f.err
	}
}

// Close closes the connection.
func (c *grpcConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		if cs, ok := c.stream.(interface{ CloseSend() error }); ok {
			_ = cs.CloseSend()
		}
		if c.cancel != nil {
			c.cancel()
		}
	})
	return nil
}

// LocalAddr returns the local network address.
func (c *grpcConn) LocalAddr() net.Addr { return c.localAddr }

// RemoteAddr returns the remote network address.
func (c *grpcConn) RemoteAddr() net.Addr { return c.remoteAddr }

// translateGRPCError maps stream errors that indicate a broken/closed
// connection to [ErrConnClosed]; other errors are returned unchanged.
func translateGRPCError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return ErrConnClosed
	}
	switch status.Code(err) {
	case codes.Canceled, codes.Unavailable:
		return ErrConnClosed
	}
	return err
}

// GRPCDialer is a [Dialer] that opens gRPC connections to scheduler peers. It
// pools one [grpc.ClientConn] per target address.
type GRPCDialer struct {
	opts []grpc.DialOption

	mu    sync.Mutex
	conns map[string]*grpc.ClientConn
}

var _ Dialer = (*GRPCDialer)(nil)

// NewGRPCDialer creates a [GRPCDialer]. The provided dial options must include
// transport credentials (e.g. insecure.NewCredentials()).
func NewGRPCDialer(opts ...grpc.DialOption) *GRPCDialer {
	return &GRPCDialer{
		opts:  opts,
		conns: make(map[string]*grpc.ClientConn),
	}
}

func (d *GRPCDialer) clientConn(target string) (*grpc.ClientConn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if cc, ok := d.conns[target]; ok {
		return cc, nil
	}
	cc, err := grpc.NewClient(target, d.opts...)
	if err != nil {
		return nil, fmt.Errorf("create client for %s: %w", target, err)
	}
	d.conns[target] = cc
	return cc, nil
}

// Dial establishes a gRPC connection to the peer at "to". The "from" address is
// advertised to the peer via metadata so it can connect back.
func (d *GRPCDialer) Dial(ctx context.Context, from, to net.Addr) (Conn, error) {
	cc, err := d.clientConn(to.String())
	if err != nil {
		return nil, err
	}

	// The stream outlives Dial but is bound to ctx (matching the HTTP/2
	// transport, where the caller passes a long-lived context); cancel is
	// stored so Close can abort the stream.
	streamCtx, cancel := context.WithCancel(ctx)
	streamCtx = metadata.AppendToOutgoingContext(streamCtx, peerAddressMetadataKey, from.String())

	stream, err := wirepb.NewWireTransportClient(cc).Connect(streamCtx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open stream: %w", err)
	}

	conn := newGRPCConn(stream, from, to)
	conn.cancel = cancel
	go conn.readLoop()
	return conn, nil
}

// Close closes all pooled client connections.
func (d *GRPCDialer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var errs []error
	for target, cc := range d.conns {
		if err := cc.Close(); err != nil {
			errs = append(errs, err)
		}
		delete(d.conns, target)
	}
	return errors.Join(errs...)
}

// GRPCListener implements [Listener] for gRPC-based connections. It is
// registered on a shared [grpc.Server] via [GRPCListener.Register] and
// implements the generated [wirepb.WireTransportServer] interface.
type GRPCListener struct {
	logger log.Logger
	addr   net.Addr

	incoming  chan *grpcConn
	closeOnce sync.Once
	closed    chan struct{}
}

var (
	_ Listener                   = (*GRPCListener)(nil)
	_ wirepb.WireTransportServer = (*GRPCListener)(nil)
)

type grpcListenerOpts struct {
	Logger log.Logger
}

// GRPCListenerOptFunc configures a [GRPCListener].
type GRPCListenerOptFunc func(*grpcListenerOpts)

// WithGRPCListenerLogger sets the logger used by the listener.
func WithGRPCListenerLogger(logger log.Logger) GRPCListenerOptFunc {
	return func(o *grpcListenerOpts) { o.Logger = logger }
}

// NewGRPCListener creates a gRPC listener advertising addr. addr is the address
// peers use to connect back; the listener itself serves on the shared
// [grpc.Server] it is registered with.
func NewGRPCListener(addr net.Addr, optFuncs ...GRPCListenerOptFunc) *GRPCListener {
	opts := grpcListenerOpts{Logger: log.NewNopLogger()}
	for _, optFunc := range optFuncs {
		optFunc(&opts)
	}
	return &GRPCListener{
		logger:   opts.Logger,
		addr:     addr,
		incoming: make(chan *grpcConn),
		closed:   make(chan struct{}),
	}
}

// Register installs the WireTransport service on the provided gRPC server.
func (l *GRPCListener) Register(s *grpc.Server) {
	wirepb.RegisterWireTransportServer(s, l)
}

// Connect implements [wirepb.WireTransportServer]. It runs for the lifetime of
// one incoming stream: it hands the connection to Accept and blocks until the
// connection is closed, keeping the stream open.
func (l *GRPCListener) Connect(stream wirepb.WireTransport_ConnectServer) error {
	ctx := stream.Context()

	md, _ := metadata.FromIncomingContext(ctx)
	vals := md.Get(peerAddressMetadataKey)
	if len(vals) == 0 || vals[0] == "" {
		return status.Errorf(codes.InvalidArgument, "missing required %s metadata", peerAddressMetadataKey)
	}
	remoteAddr, err := addrPortStrToAddr(vals[0])
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid peer address %q: %v", vals[0], err)
	}

	conn := newGRPCConn(stream, l.Addr(), remoteAddr)
	defer conn.Close()
	go conn.readLoop()

	select {
	case <-l.closed:
		return status.Error(codes.Unavailable, "listener closed")
	case <-ctx.Done():
		return status.FromContextError(ctx.Err()).Err()
	case l.incoming <- conn:
		// Connection accepted; keep the stream open until it is closed.
		select {
		case <-conn.closed:
			return nil
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		}
	}
}

// Accept waits for and returns the next connection to the listener.
func (l *GRPCListener) Accept(ctx context.Context) (Conn, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.closed:
		return nil, net.ErrClosed
	case conn := <-l.incoming:
		return conn, nil
	}
}

// Close closes the listener. Blocked Accept calls return an error.
func (l *GRPCListener) Close(_ context.Context) error {
	l.closeOnce.Do(func() {
		close(l.closed)
	})
	return nil
}

// Addr returns the listener's advertised network address.
func (l *GRPCListener) Addr() net.Addr { return l.addr }
