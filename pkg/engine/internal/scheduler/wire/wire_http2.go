package wire

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"golang.org/x/net/http2"
)

// peerAddressHeader is the header used to advertise the address to connect back
// to a client.
const peerAddressHeader = "Loki-Peer-Address"

// HTTP2Listener implements Listener for HTTP/2-based connections.
type HTTP2Listener struct {
	logger log.Logger
	addr   net.Addr

	connCh    chan *incomingHTTP2Conn
	closeOnce sync.Once
	closed    chan struct{}
	codec     *protobufCodec
}

type incomingHTTP2Conn struct {
	conn *http2Conn
	w    http.ResponseWriter
}

var (
	_ Listener     = (*HTTP2Listener)(nil)
	_ http.Handler = (*HTTP2Listener)(nil)
)

type http2ListenerOpts struct {
	// MaxPendingConns defines the maximum number of pending connections (which are not Accepted yet).
	MaxPendingConns uint

	// Logger is used for logging.
	Logger log.Logger
}

type HTTP2ListenerOptFunc func(*http2ListenerOpts)

func WithHTTP2ListenerMaxPendingConns(maxPendingConns uint) HTTP2ListenerOptFunc {
	return func(o *http2ListenerOpts) {
		o.MaxPendingConns = maxPendingConns
	}
}

func WithHTTP2ListenerLogger(logger log.Logger) HTTP2ListenerOptFunc {
	return func(o *http2ListenerOpts) {
		o.Logger = logger
	}
}

// NewHTTP2Listener creates a new HTTP/2 listener on the specified address.
func NewHTTP2Listener(addr net.Addr, optFuncs ...HTTP2ListenerOptFunc) *HTTP2Listener {
	opts := http2ListenerOpts{
		MaxPendingConns: 10,
		Logger:          log.NewNopLogger(),
	}
	for _, optFunc := range optFuncs {
		optFunc(&opts)
	}

	l := &HTTP2Listener{
		addr:   addr,
		logger: opts.Logger,

		connCh: make(chan *incomingHTTP2Conn, opts.MaxPendingConns),
		closed: make(chan struct{}),
		codec:  defaultFrameCodec,
	}

	return l
}

// ServeHTTP handles incoming connections.
func (l *HTTP2Listener) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.ProtoMajor != 2 {
		http.Error(w, "codec not supported", http.StatusHTTPVersionNotSupported)
		return
	}

	peerAddress := r.Header.Get(peerAddressHeader)
	if peerAddress == "" {
		http.Error(w, "missing required Loki-Peer-Address header", http.StatusBadRequest)
		return
	}

	remoteAddr, err := addrPortStrToAddr(peerAddress)
	if err != nil {
		http.Error(w, "invalid remote addr", http.StatusBadRequest)
		return
	}

	rc := http.NewResponseController(w)
	err = rc.SetWriteDeadline(time.Time{})
	if err != nil {
		http.Error(w, "failed to set write deadline", http.StatusInternalServerError)
		return
	}

	err = rc.SetReadDeadline(time.Time{})
	if err != nil {
		http.Error(w, "failed to set read deadline", http.StatusInternalServerError)
		return
	}

	conn := newHTTP2Conn(l.Addr(), remoteAddr, r.Body, w, rc, l.codec)
	defer conn.Close()

	incomingConn := &incomingHTTP2Conn{conn: conn, w: w}

	// Try to enqueue the connection without blocking indefinitely
	select {
	case <-l.closed:
		err := conn.Close()
		if err != nil {
			level.Error(l.logger).Log("msg", "failed to close connection on listener close", "err", err.Error())
			http.Error(w, "failed to close connection", http.StatusInternalServerError)
			return
		}

		http.Error(w, "listener closed", http.StatusServiceUnavailable)
	case l.connCh <- incomingConn:
		// read loop exits if a connection is closed or the context is canceled
		conn.readLoop(r.Context())
	}
}

// Accept waits for and returns the next connection to the listener.
func (l *HTTP2Listener) Accept(ctx context.Context) (Conn, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.closed:
		return nil, net.ErrClosed
	case incomingConn := <-l.connCh:
		incomingConn.w.WriteHeader(http.StatusOK)
		err := incomingConn.conn.responseController.Flush()
		if err != nil {
			return nil, fmt.Errorf("flush response: %w", err)
		}
		return incomingConn.conn, nil
	}
}

// Close closes the listener.
func (l *HTTP2Listener) Close(_ context.Context) error {
	l.closeOnce.Do(func() {
		close(l.closed)
	})
	return nil
}

// Addr returns the listener's network address.
func (l *HTTP2Listener) Addr() net.Addr {
	return l.addr
}

// http2Conn implements Conn for HTTP/2-based connections.
type http2Conn struct {
	localAddr  net.Addr
	remoteAddr net.Addr

	codec              *protobufCodec
	reader             io.ReadCloser
	writer             io.Writer
	responseController *http.ResponseController
	cleanup            func() // Optional cleanup function

	writeMu   sync.Mutex
	closeOnce sync.Once
	closed    chan struct{}

	incomingCh chan incomingFrame
}

type incomingFrame struct {
	frame Frame
	err   error
}

var _ Conn = (*http2Conn)(nil)

// newHTTP2Conn creates a new HTTP/2 connection.
func newHTTP2Conn(
	localAddr net.Addr,
	remoteAddr net.Addr,
	reader io.ReadCloser,
	writer io.Writer,
	responseController *http.ResponseController,
	codec *protobufCodec,
) *http2Conn {
	c := &http2Conn{
		localAddr:          localAddr,
		remoteAddr:         remoteAddr,
		codec:              codec,
		reader:             reader,
		writer:             writer,
		responseController: responseController,
		closed:             make(chan struct{}),
		incomingCh:         make(chan incomingFrame),
	}
	return c
}

func (c *http2Conn) readLoop(ctx context.Context) {
	for {
		frame, err := c.codec.DecodeFrom(c.reader)
		incoming := incomingFrame{frame: frame, err: err}
		select {
		case <-ctx.Done():
			return
		case <-c.closed:
			return
		case c.incomingCh <- incoming:
		}
	}
}

// Send sends a frame over the connection.
func (c *http2Conn) Send(ctx context.Context, frame Frame) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return ErrConnClosed
	default:
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.codec.EncodeTo(c.writer, frame); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}

	// Flush after each frame to ensure immediate delivery
	if c.responseController != nil {
		err := c.responseController.Flush()
		if err != nil {
			return fmt.Errorf("flush response: %w", err)
		}
	}

	return nil
}

// Recv receives a frame from the connection.
func (c *http2Conn) Recv(ctx context.Context) (Frame, error) {
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
func (c *http2Conn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		err = c.reader.Close()
		if c.cleanup != nil {
			c.cleanup()
		}
	})
	return err
}

// LocalAddr returns the local network address.
func (c *http2Conn) LocalAddr() net.Addr {
	return c.localAddr
}

// RemoteAddr returns the remote network address.
func (c *http2Conn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

// HTTP2Dialer holds an http client to pool the connections.
type HTTP2Dialer struct {
	client *http.Client
	codec  *protobufCodec
	path   string
}

var _ Dialer = (*HTTP2Dialer)(nil)

// NewHTTP2Dialer creates a [Dialer] that can open HTTP/2 connections to the
// specified address.
func NewHTTP2Dialer(path string) *HTTP2Dialer {
	return &HTTP2Dialer{
		client: &http.Client{
			Transport: &http2.Transport{
				// No TLS
				AllowHTTP: true,
				DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, addr)
				},
			},
			// Context is used for cancellation, no timeout
			Timeout: 0,
		},
		codec: defaultFrameCodec,
		path:  path,
	}
}

// Dial establishes an HTTP/2 connection to the specified address.
func (d *HTTP2Dialer) Dial(ctx context.Context, from, to net.Addr) (Conn, error) {
	pr, pw := io.Pipe()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://%s%s", to.String(), d.path), pr)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set(peerAddressHeader, from.String())

	resp, err := d.client.Do(req)
	if err != nil {
		_ = pw.Close()
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		// Drain and close response body to allow connection reuse
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		_ = pw.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Create connection
	conn := newHTTP2Conn(
		from,
		to,
		resp.Body,
		pw,
		nil, // client doesn't need responseController, it's handled by the pipe writer
		d.codec,
	)

	readLoopWg := sync.WaitGroup{}
	readLoopWg.Add(1)
	go func() {
		defer readLoopWg.Done()
		conn.readLoop(ctx)
	}()

	// when the connection is closed, close the pipe writer and wait until the reader loop exits
	conn.cleanup = func() {
		_ = pw.Close()
		readLoopWg.Wait()
	}

	return conn, nil
}
