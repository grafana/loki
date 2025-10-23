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

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// HTTP2Listener implements Listener for HTTP/2-based connections.
type HTTP2Listener struct {
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

// NewHTTP2Listener creates a new HTTP/2 listener on the specified address.
// TODO(ivkalita): opt funcs for configuration
func NewHTTP2Listener(
	addr string,
	protocolFactory func() FrameProtocol,
	connAcceptTimeout time.Duration,
	serverShutdownTimeout time.Duration,
	maxPendingConns int,
	readHeaderTimeout time.Duration,
) (*HTTP2Listener, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	l := &HTTP2Listener{
		listener:              listener,
		connCh:                make(chan Conn, maxPendingConns),
		closed:                make(chan struct{}),
		protocolFactory:       protocolFactory,
		connAcceptTimeout:     connAcceptTimeout,
		serverShutdownTimeout: serverShutdownTimeout,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/wire", l.handleWire)

	l.server = &http.Server{
		Handler:           h2c.NewHandler(mux, &http2.Server{}),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	l.wgServe.Add(1)
	go func() {
		defer l.wgServe.Done()
		// TODO(ivkalita): log error properly
		_ = l.server.Serve(listener)
	}()

	return l, nil
}

// handleWire handles incoming wire protocol connections.
func (l *HTTP2Listener) handleWire(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send success headers immediately
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Create a new protocol instance for this connection (protocols are stateful)
	conn := newHTTP2Conn(l.listener.Addr(), r.RemoteAddr, r.Body, w, flusher, l.protocolFactory())

	// Try to enqueue the connection without blocking indefinitely
	// TODO(ivkalita): properly handle and log errors
	select {
	case <-l.closed:
		_ = conn.Close()
		return
	case l.connCh <- conn:
		// Connection accepted, wait for it to close
		<-conn.done
	case <-time.After(l.connAcceptTimeout):
		// If Accept() is too slow, close the connection
		_ = conn.Close()
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
func newHTTP2Conn(localAddr net.Addr, remoteAddr string, reader io.ReadCloser, writer io.Writer, flusher http.Flusher, protocol FrameProtocol) *HTTP2Conn {
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
// Note: Uses a goroutine to make blocking Decode() interruptible via context.
// The buffered channel prevents goroutine leaks even if context is cancelled.
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
		// Buffered channel ensures this never blocks, even if receiver moved on
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
	c.closeOnce.Do(func() {
		close(c.closed)
		if c.reader != nil {
			c.reader.Close()
		}
		if c.cleanup != nil {
			c.cleanup()
		}
		close(c.done)
	})
	return nil
}

// LocalAddr returns the local network address.
func (c *HTTP2Conn) LocalAddr() net.Addr {
	return c.localAddr
}

// RemoteAddr returns the remote network address.
func (c *HTTP2Conn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

// DialHTTP2 establishes an HTTP/2 connection to the specified address.
func DialHTTP2(ctx context.Context, addr string, protocolFactory func() FrameProtocol) (Conn, error) {
	// Create a pipe for bidirectional communication
	pr, pw := io.Pipe()

	// Build the request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://%s/wire", addr), pr)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Create HTTP/2 client with reasonable timeouts
	client := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		},
		Timeout: 0, // No timeout, use context
	}

	// Make the request in a goroutine
	respCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)

	go func() {
		resp, err := client.Do(req)
		if err != nil {
			errCh <- err
			pw.Close()
			return
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			pw.Close()
			errCh <- fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			return
		}
		respCh <- resp
	}()

	// Wait for response headers
	var resp *http.Response
	select {
	case <-ctx.Done():
		pw.Close()
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case resp = <-respCh:
	}

	conn := newHTTP2Conn(
		&tcpAddr{addr: "client"},
		addr,
		resp.Body,
		pw,
		nil, // Client doesn't need flusher,
		protocolFactory(),
	)

	// Set cleanup function to close pipe writer
	conn.cleanup = func() {
		pw.Close()
	}

	return conn, nil
}

// tcpAddr implements net.Addr for HTTP/2 connections.
type tcpAddr struct {
	addr string
}

func (a *tcpAddr) Network() string { return "tcp" }
func (a *tcpAddr) String() string  { return a.addr }
