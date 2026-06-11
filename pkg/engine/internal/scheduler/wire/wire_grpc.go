package wire

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/grafana/loki/v3/pkg/engine/internal/proto/wirepb"
)

// wirePeerAddrMetadataKey is the gRPC metadata key used to advertise the
// caller's address to the remote peer, replacing the old HTTP header
// "Loki-Peer-Address".
const wirePeerAddrMetadataKey = "wire-peer-addr"

// wireMaxGRPCMessageBytes is the maximum gRPC message size for wire
// transport calls.  StreamDataMessage carries serialized Arrow record
// batches that can exceed gRPC's 4 MB default.
//
// FIXME(ops): This is a blunt-instrument workaround.  The gRPC server
// (dskit) applies a SINGLE server-wide message-size limit; raising it for
// WireService also raises it for every other gRPC service on the same
// server (ingester, query-scheduler, etc.), widening the blast radius for
// malformed or malicious requests.  There is no per-service limit in
// gRPC-Go.  Operators MUST also set:
//
//	server.grpc_server_max_recv_msg_size: 268435456  # 256 MiB
//	server.grpc_server_max_send_msg_size: 268435456  # 256 MiB
//
// in their Loki configuration when running distributed query execution or
// dataobj compaction workers.  This should be revisited when dskit adds
// per-service limits or when the batching parameters are better understood.
const wireMaxGRPCMessageBytes = 256 * 1024 * 1024 // 256 MiB

// incomingFrame holds a decoded frame (or an error) pushed by readLoop into
// grpcConn.incomingCh so that Recv can be implemented without holding a mutex.
type incomingFrame struct {
	frame Frame
	err   error
}

// wireStream is the minimal interface shared by WireService_PipeServer and
// WireService_PipeClient.  Both sides expose Send, Recv, and a Context method,
// allowing grpcConn to be used for both server-side and client-side streams.
type wireStream interface {
	Send(*wirepb.Frame) error
	Recv() (*wirepb.Frame, error)
	Context() context.Context
}

// grpcConn implements Conn over a gRPC bidi-streaming call.
// It is used on both sides of the connection (server via GRPCListener.Pipe,
// client via GRPCDialer.Dial).
type grpcConn struct {
	localAddr  net.Addr
	remoteAddr net.Addr
	stream     wireStream

	codec     *protobufCodec
	writeMu   sync.Mutex
	closeOnce sync.Once
	closed    chan struct{}
	closeFunc func() // optional cleanup called once on Close (client-side teardown)

	incomingCh chan incomingFrame
}

var _ Conn = (*grpcConn)(nil)

func newGRPCConn(localAddr, remoteAddr net.Addr, stream wireStream, closeFunc func()) *grpcConn {
	return &grpcConn{
		localAddr:  localAddr,
		remoteAddr: remoteAddr,
		stream:     stream,
		codec:      DefaultFrameCodec,
		closed:     make(chan struct{}),
		closeFunc:  closeFunc,
		incomingCh: make(chan incomingFrame, 1),
	}
}

// readLoop runs in a goroutine and pumps frames from the gRPC stream into
// incomingCh.  It exits when the stream returns an error or when the
// connection is closed.
func (c *grpcConn) readLoop() {
	for {
		pbFrame, err := c.stream.Recv()
		if err != nil {
			if isGRPCConnError(err) {
				err = ErrConnClosed
			}
			select {
			case <-c.closed:
			case c.incomingCh <- incomingFrame{frame: nil, err: err}:
			}
			return
		}
		frame, convErr := c.codec.frameFromPbFrame(pbFrame)
		if convErr != nil {
			select {
			case <-c.closed:
			case c.incomingCh <- incomingFrame{frame: nil, err: convErr}:
			}
			return
		}
		select {
		case <-c.closed:
			return
		case c.incomingCh <- incomingFrame{frame: frame, err: nil}:
		}
	}
}

// Send encodes and sends a frame over the gRPC stream.
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

	pbFrame, err := c.codec.frameToPbFrame(frame)
	if err != nil {
		return fmt.Errorf("encode frame: %w", err)
	}
	err = c.stream.Send(pbFrame)
	if err != nil {
		if isGRPCConnError(err) {
			return ErrConnClosed
		}
		return fmt.Errorf("send frame: %w", err)
	}
	return nil
}

// Recv waits for and returns the next frame from the gRPC stream.
func (c *grpcConn) Recv(ctx context.Context) (Frame, error) {
	select {
	case <-c.closed:
		return nil, ErrConnClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	case f := <-c.incomingCh:
		return f.frame, f.err
	}
}

// Close closes the connection.  Concurrent or subsequent calls to Close are
// no-ops.  After Close returns, Send and Recv return ErrConnClosed.
//
// writeMu is held while calling closeFunc to prevent stream.CloseSend()
// from racing with a concurrent stream.Send() in Send().  This mirrors the
// http2Conn.Close() pattern.
func (c *grpcConn) Close() error {
	c.closeOnce.Do(func() {
		// Mark the connection closed first so in-progress Send() calls
		// bail out quickly once they finish their current stream.Send()
		// and any new Send() calls return ErrConnClosed immediately.
		close(c.closed)
		// Hold writeMu while invoking closeFunc so that stream.CloseSend()
		// cannot race with a concurrent c.stream.Send() call in Send().
		c.writeMu.Lock()
		if c.closeFunc != nil {
			c.closeFunc()
		}
		c.writeMu.Unlock()
	})
	return nil
}

// LocalAddr returns the local address of the connection.
func (c *grpcConn) LocalAddr() net.Addr { return c.localAddr }

// RemoteAddr returns the remote address of the connection.
func (c *grpcConn) RemoteAddr() net.Addr { return c.remoteAddr }

// isGRPCConnError returns true for errors that indicate the stream or
// connection is broken and should be mapped to ErrConnClosed.
func isGRPCConnError(err error) bool {
	if err == nil {
		return false
	}
	if err == io.EOF {
		return true
	}
	switch status.Code(err) {
	case codes.Canceled, codes.DeadlineExceeded, codes.Unavailable, codes.Internal, codes.ResourceExhausted:
		return true
	}
	return false
}

// --- GRPCListener ---

// grpcListenerOpts holds optional configuration for GRPCListener.
type grpcListenerOpts struct {
	Logger log.Logger
}

// GRPCListenerOptFunc is a functional option for NewGRPCListener.
type GRPCListenerOptFunc func(*grpcListenerOpts)

// WithGRPCListenerLogger sets the logger for a GRPCListener.
func WithGRPCListenerLogger(l log.Logger) GRPCListenerOptFunc {
	return func(o *grpcListenerOpts) {
		o.Logger = l
	}
}

// GRPCListener implements Listener and wirepb.WireServiceServer.  It accepts
// incoming bidi-streaming Pipe calls and converts them into wire.Conn
// instances that can be retrieved by the scheduler or worker via Accept.
type GRPCListener struct {
	logger    log.Logger
	addr      net.Addr
	incoming  chan *grpcConn
	closed    chan struct{}
	closeOnce sync.Once
}

var (
	_ Listener                 = (*GRPCListener)(nil)
	_ wirepb.WireServiceServer = (*GRPCListener)(nil)
)

// NewGRPCListener creates a GRPCListener that advertises the given addr.
// The addr is used as the LocalAddr for all accepted connections.
// The listener must be registered on a *grpc.Server via
// wirepb.RegisterWireServiceServer before it can accept connections.
func NewGRPCListener(addr net.Addr, opts ...GRPCListenerOptFunc) *GRPCListener {
	o := &grpcListenerOpts{Logger: log.NewNopLogger()}
	for _, fn := range opts {
		fn(o)
	}
	return &GRPCListener{
		logger:   o.Logger,
		addr:     addr,
		incoming: make(chan *grpcConn),
		closed:   make(chan struct{}),
	}
}

// Pipe is the WireService gRPC handler.  It is called by the gRPC server
// for each incoming bidi stream, validates the "wire-peer-addr" metadata,
// creates a grpcConn, hands it to Accept via the incoming channel, then
// blocks until the connection is closed, keeping the stream alive.
func (l *GRPCListener) Pipe(stream wirepb.WireService_PipeServer) error {
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return status.Error(codes.InvalidArgument, "missing gRPC metadata")
	}
	vals := md.Get(wirePeerAddrMetadataKey)
	if len(vals) == 0 {
		return status.Errorf(codes.InvalidArgument, "missing %q metadata key", wirePeerAddrMetadataKey)
	}
	remoteAddr, err := addrPortStrToAddr(vals[0])
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid %q value %q: %v", wirePeerAddrMetadataKey, vals[0], err)
	}

	conn := newGRPCConn(l.addr, remoteAddr, stream, nil)
	defer conn.Close()

	// Hand the conn to Accept.  Block until Accept consumes it, or until
	// the listener/stream is closed.
	select {
	case <-l.closed:
		return status.Error(codes.Unavailable, "listener closed")
	case <-stream.Context().Done():
		return stream.Context().Err()
	case l.incoming <- conn:
		// connection accepted by Accept()
		level.Debug(l.logger).Log("msg", "accepted wire gRPC connection", "remote_addr", remoteAddr.String())
	}

	// Start the read loop now that the conn has been accepted.
	go conn.readLoop()

	// Block until the conn is closed.  Returning from Pipe closes the
	// gRPC stream, which causes the remote side to see an error on Recv.
	select {
	case <-conn.closed:
		return nil
	case <-stream.Context().Done():
		conn.Close()
		return nil
	}
}

// Accept waits for and returns the next accepted connection.
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

// Close closes the listener.  Any blocked Accept operations will return
// net.ErrClosed.  In-flight connections are unaffected.
func (l *GRPCListener) Close(_ context.Context) error {
	l.closeOnce.Do(func() {
		level.Debug(l.logger).Log("msg", "closing wire gRPC listener", "addr", l.addr.String())
		close(l.closed)
	})
	return nil
}

// Addr returns the listener's advertised network address.
func (l *GRPCListener) Addr() net.Addr { return l.addr }

// --- GRPCDialer ---

// GRPCDialer implements Dialer for gRPC-based connections.
type GRPCDialer struct {
	codec *protobufCodec
}

var _ Dialer = (*GRPCDialer)(nil)

// NewGRPCDialer creates a GRPCDialer.
func NewGRPCDialer() *GRPCDialer {
	return &GRPCDialer{codec: DefaultFrameCodec}
}

// Dial opens a gRPC bidi-streaming connection to the peer at to.
// The from address is sent to the server as the "wire-peer-addr" metadata
// value so the server can identify the caller's advertised address.
//
// Each call to Dial creates a new *grpc.ClientConn that uses a lazy
// connection (grpc.NewClient default).  Errors surface on the first
// Send/Recv rather than at Dial time; the caller's backoff-retry loop
// handles transient failures.  The conn is closed when the returned Conn
// is closed.
//
// The connection uses insecure transport (no TLS), matching the existing
// HTTP/2 wire behaviour.
func (d *GRPCDialer) Dial(ctx context.Context, from, to net.Addr) (Conn, error) {
	cc, err := grpc.NewClient(
		to.String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", to, err)
	}

	// Attach the caller's address as metadata so the server knows the
	// remote address (replaces the old Loki-Peer-Address HTTP header).
	// The context is also used to bound the lifetime of the stream.
	streamCtx := metadata.NewOutgoingContext(ctx,
		metadata.Pairs(wirePeerAddrMetadataKey, from.String()),
	)

	stream, err := wirepb.NewWireServiceClient(cc).Pipe(
		streamCtx,
		grpc.MaxCallRecvMsgSize(wireMaxGRPCMessageBytes),
		grpc.MaxCallSendMsgSize(wireMaxGRPCMessageBytes),
	)
	if err != nil {
		_ = cc.Close()
		return nil, fmt.Errorf("open pipe to %s: %w", to, err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	conn := newGRPCConn(from, to, stream, func() {
		_ = stream.CloseSend()
		_ = cc.Close()
		wg.Wait()
	})

	go func() {
		defer wg.Done()
		conn.readLoop()
	}()

	return conn, nil
}
