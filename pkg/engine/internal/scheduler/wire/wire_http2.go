package wire

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// HTTP2Listener implements Listener for HTTP/2-based connections.
type HTTP2Listener struct {
	logger log.Logger

	server                *http.Server
	listener              net.Listener
	connCh                chan Conn
	closeOnce             sync.Once
	closed                chan struct{}
	protocolFactory       func() FrameProtocol
	wgServe               sync.WaitGroup
	connAcceptTimeout     time.Duration
	serverShutdownTimeout time.Duration
}

var _ Listener = (*HTTP2Listener)(nil)

type http2ListenerOpts struct {
	// ConnAcceptTimeout defines how long to wait for a connection to be accepted via .Accept().
	// The connection is closed if Accept() is too slow.
	ConnAcceptTimeout time.Duration

	// ServerShutdownTimeout defines how long to wait for the server to gracefully shutdown.
	ServerShutdownTimeout time.Duration

	// MaxPendingConns defines the maximum number of pending connections (which are not Accepted yet).
	MaxPendingConns uint

	// ReadHeaderTimeout defines how long to wait for the server to read the request headers for the first call
	// to establish a connection.
	ReadHeaderTimeout time.Duration

	// Logger is used for logging.
	Logger log.Logger
}

type HTTP2ListenerOptFunc func(*http2ListenerOpts)

func WithHTTP2ListenerConnAcceptTimeout(timeout time.Duration) HTTP2ListenerOptFunc {
	return func(o *http2ListenerOpts) {
		o.ConnAcceptTimeout = timeout
	}
}

func WithHTTP2ListenerServerShutdownTimeout(timeout time.Duration) HTTP2ListenerOptFunc {
	return func(o *http2ListenerOpts) {
		o.ServerShutdownTimeout = timeout
	}
}

func WithHTTP2ListenerMaxPendingConns(maxPendingConns uint) HTTP2ListenerOptFunc {
	return func(o *http2ListenerOpts) {
		o.MaxPendingConns = maxPendingConns
	}
}

func WithHTTP2ListenerReadHeaderTimeout(timeout time.Duration) HTTP2ListenerOptFunc {
	return func(o *http2ListenerOpts) {
		o.ReadHeaderTimeout = timeout
	}
}

func WithHTTP2ListenerLogger(logger log.Logger) HTTP2ListenerOptFunc {
	return func(o *http2ListenerOpts) {
		o.Logger = logger
	}
}

// NewHTTP2Listener creates a new HTTP/2 listener on the specified address.
func NewHTTP2Listener(
	addr string,
	protocolFactory func() FrameProtocol,
	optFuncs ...HTTP2ListenerOptFunc,
) (*HTTP2Listener, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	opts := http2ListenerOpts{
		ConnAcceptTimeout:     1 * time.Second,
		ServerShutdownTimeout: 1 * time.Second,
		MaxPendingConns:       10,
		ReadHeaderTimeout:     1 * time.Second,
		Logger:                log.NewNopLogger(),
	}
	for _, optFunc := range optFuncs {
		optFunc(&opts)
	}

	l := &HTTP2Listener{
		logger: opts.Logger,

		listener:              listener,
		connCh:                make(chan Conn, opts.MaxPendingConns),
		closed:                make(chan struct{}),
		protocolFactory:       protocolFactory,
		connAcceptTimeout:     opts.ConnAcceptTimeout,
		serverShutdownTimeout: opts.ServerShutdownTimeout,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/stream", l.handleStream)

	l.server = &http.Server{
		Handler:           h2c.NewHandler(mux, &http2.Server{}),
		ReadHeaderTimeout: opts.ReadHeaderTimeout,
	}

	l.wgServe.Add(1)
	go func() {
		defer l.wgServe.Done()
		err := l.server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = level.Error(l.logger).Log("msg", "http2 server error", "err", err.Error())
		}
	}()

	return l, nil
}

// handleStream handles incoming stream connections.
func (l *HTTP2Listener) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	if r.ProtoMajor != 2 {
		http.Error(w, "protocol not supported", http.StatusHTTPVersionNotSupported)
		return
	}

	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Create a new protocol instance for this connection (protocols are stateful)
	conn := newHTTP2Conn(l.listener.Addr(), r.RemoteAddr, r.Body, w, flusher, l.protocolFactory())

	// Try to enqueue the connection without blocking indefinitely
	select {
	case <-l.closed:
		err := conn.Close()
		if err != nil {
			level.Error(l.logger).Log("msg", "failed to close connection on listener close", "err", err.Error())
		}
		return
	case l.connCh <- conn:
		// Connection accepted, wait for it to close
		<-conn.done
	case <-time.After(l.connAcceptTimeout):
		// If Accept() is too slow, close the connection
		err := conn.Close()
		if err != nil {
			level.Error(l.logger).Log("msg", "failed to close not accepted connection", "err", err.Error())
		}

		http.Error(w, "connection not accepted", http.StatusServiceUnavailable)
	}
}

// Accept waits for and returns the next connection to the listener.
func (l *HTTP2Listener) Accept(ctx context.Context) (Conn, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.closed:
		return nil, net.ErrClosed
	case conn := <-l.connCh:
		return conn, nil
	}
}

// Close closes the listener.
func (l *HTTP2Listener) Close(ctx context.Context) error {
	var err error
	l.closeOnce.Do(func() {
		close(l.closed)

		// Graceful shutdown with timeout
		ctx, cancel := context.WithTimeout(ctx, l.serverShutdownTimeout)
		defer cancel()

		if shutdownErr := l.server.Shutdown(ctx); shutdownErr != nil {
			err = l.server.Close()
		}

		l.wgServe.Wait()
	})
	return err
}

// Addr returns the listener's network address.
func (l *HTTP2Listener) Addr() net.Addr {
	return l.listener.Addr()
}

// HTTP2Conn implements Conn for HTTP/2-based connections.
type HTTP2Conn struct {
	localAddr  net.Addr
	remoteAddr net.Addr

	protocol FrameProtocol
	reader   io.ReadCloser
	writer   io.Writer
	flusher  http.Flusher
	cleanup  func() // Optional cleanup function

	writeMu   sync.Mutex
	closeOnce sync.Once
	closed    chan struct{}
	done      chan struct{} // Signals when the connection is fully closed
}

var _ Conn = (*HTTP2Conn)(nil)

// newHTTP2Conn creates a new HTTP/2 connection.
func newHTTP2Conn(
	localAddr net.Addr,
	remoteAddr string,
	reader io.ReadCloser,
	writer io.Writer,
	flusher http.Flusher,
	protocol FrameProtocol,
) *HTTP2Conn {
	// Bind protocol to reader and writer
	protocol.BindReader(reader)
	protocol.BindWriter(writer)

	c := &HTTP2Conn{
		localAddr:  localAddr,
		remoteAddr: &tcpAddr{addr: remoteAddr},
		protocol:   protocol,
		reader:     reader,
		writer:     writer,
		flusher:    flusher,
		closed:     make(chan struct{}),
		done:       make(chan struct{}),
	}
	return c
}

// Send sends a frame over the connection.
func (c *HTTP2Conn) Send(ctx context.Context, frame Frame) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return ErrConnClosed
	default:
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.protocol.WriteFrame(frame); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}

	// Flush after each frame to ensure immediate delivery
	if c.flusher != nil {
		c.flusher.Flush()
	}

	return nil
}

// Recv receives a frame from the connection.
func (c *HTTP2Conn) Recv(ctx context.Context) (Frame, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, ErrConnClosed
	default:
	}

	type result struct {
		frame Frame
		err   error
	}
	resultCh := make(chan result, 1)

	go func() {
		frame, err := c.protocol.ReadFrame()
		resultCh <- result{frame: frame, err: err}
	}()

	select {
	case <-ctx.Done():
		// Close reader to unblock the decoder goroutine
		c.reader.Close()
		return nil, ctx.Err()
	case <-c.closed:
		return nil, ErrConnClosed
	case res := <-resultCh:
		if res.err != nil {
			if errors.Is(res.err, io.EOF) {
				c.Close()
				return nil, ErrConnClosed
			}
			return nil, fmt.Errorf("decode frame: %w", res.err)
		}
		return res.frame, nil
	}
}

// Close closes the connection.
func (c *HTTP2Conn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.reader != nil {
			err = c.reader.Close()
		}
		if c.cleanup != nil {
			c.cleanup()
		}
		close(c.done)
	})
	return err
}

// LocalAddr returns the local network address.
func (c *HTTP2Conn) LocalAddr() net.Addr {
	return c.localAddr
}

// RemoteAddr returns the remote network address.
func (c *HTTP2Conn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

// HTTP2Dialer holds an http client to pool the connections.
type HTTP2Dialer struct {
	client *http.Client
}

// NewHTTP2Dialer creates a new HTTP/2 dialer that can open HTTP/2 connections to the specified address.
func NewHTTP2Dialer() *HTTP2Dialer {
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
	}
}

// Dial establishes an HTTP/2 connection to the specified address.
func (d *HTTP2Dialer) Dial(ctx context.Context, addr string, protocolFactory func() FrameProtocol) (Conn, error) {
	pr, pw := io.Pipe()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://%s/stream", addr), pr)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

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
		&tcpAddr{addr: "client"},
		addr,
		resp.Body,
		pw,
		nil, // client doesn't need flusher, it's handled by the pipe writer
		protocolFactory(),
	)

	// when connection is closed, close the pipe writer
	conn.cleanup = func() {
		_ = pw.Close()
	}

	return conn, nil
}

type tcpAddr struct {
	addr string
}

func (a *tcpAddr) Network() string { return "tcp" }
func (a *tcpAddr) String() string  { return a.addr }
