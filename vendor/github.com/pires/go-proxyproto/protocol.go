package proxyproto

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// readBufferSize is the size used for bufio.Reader's internal buffer.
//
// This is kept low to reduce per-connection memory overhead. If the header is
// larger than readBufferSize, the header will be decoded with multiple Read
// calls. For v1 the header length is at most 108 bytes. For v2 the header
// length is at most 52 bytes plus the length of the TLVs. We use 256 bytes to
// accommodate for the most common cases.
const readBufferSize = 256

var (
	// DefaultReadHeaderTimeout is how long header processing waits for header to
	// be read from the wire, if Listener.ReaderHeaderTimeout is not set.
	// It's kept as a global variable so to make it easier to find and override,
	// e.g. go build -ldflags -X "github.com/pires/go-proxyproto.DefaultReadHeaderTimeout=1s".
	DefaultReadHeaderTimeout = 10 * time.Second

	// ErrInvalidUpstream should be returned (possibly wrapped) by a policy
	// function when an upstream connection address is not trusted or cannot be
	// classified. Listener.Accept closes that connection and keeps listening;
	// a policy error that does not wrap ErrInvalidUpstream is returned by
	// Accept itself, which typically stops the caller's accept loop. All
	// built-in policies wrap address-classification failures in
	// ErrInvalidUpstream so a single unclassifiable peer cannot stop the
	// listener.
	ErrInvalidUpstream = fmt.Errorf("proxyproto: upstream connection address not trusted for PROXY information")
)

// Listener is used to wrap an underlying listener,
// whose connections may be using the HAProxy Proxy Protocol.
// If the connection is using the protocol, the RemoteAddr() will return
// the correct client address. ReadHeaderTimeout will be applied to all
// connections in order to prevent blocking operations. If no ReadHeaderTimeout
// is set, a default of 10s will be used. This can be disabled by setting the
// timeout to < 0.
//
// When neither Policy nor ConnPolicy is set, DefaultPolicy applies: REQUIRE,
// so connections that do not open with a PROXY header fail their first
// Read/Write with ErrNoProxyProtocol. Headers are still honored from ANY peer
// under REQUIRE; a listener reachable by untrusted clients should set a
// trusted-source policy (e.g. TrustProxyHeaderFrom or TrustProxyHeaderFromRanges).
//
// Listener is stream-oriented (TCP, Unix stream): the header is read once at
// the start of the byte stream. It cannot implement the PROXY protocol over
// UDP, where the spec requires a header parsed independently in each datagram;
// use ParseUDPDatagram and Header.FormatUDPDatagram for that.
//
// Note that ReadHeaderTimeout only bounds how long a single slow connection can
// hold a goroutine and file descriptor during header detection; it is not a
// connection count or accept-rate limit. Deployments exposed to untrusted
// clients should keep ReadHeaderTimeout low and enforce connection/rate limits
// upstream (or around Accept).
//
// Only one of Policy or ConnPolicy should be provided. If both are provided then
// a panic would occur during accept.
type Listener struct {
	// Listener is the underlying listener.
	Listener net.Listener
	// Deprecated: use ConnPolicyFunc instead. This will be removed in future release.
	Policy PolicyFunc
	// ConnPolicy is the policy function for accepted connections.
	ConnPolicy ConnPolicyFunc
	// ValidateHeader is the validator function for the proxy header.
	ValidateHeader Validator
	// ReadHeaderTimeout is the timeout for reading the proxy header.
	ReadHeaderTimeout time.Duration
	// ReadBufferSize is the read buffer size for accepted connections. When > 0,
	// each accepted connection uses this size for proxy header detection; 0 means default.
	// See the sizing note on WithBufferSize: values below 107 bytes break v1
	// header parsing.
	ReadBufferSize int
}

// Conn is used to wrap and underlying connection which
// may be speaking the Proxy Protocol. If it is, the RemoteAddr() will
// return the address of the client instead of the proxy address. Each connection
// will have its own readHeaderTimeout and readDeadline set by the Accept() call.
type Conn struct {
	readDeadline atomic.Value // time.Time
	once         sync.Once
	readErr      error
	conn         net.Conn
	bufReader    *bufio.Reader
	// bufferSize is set when the client overrides via WithBufferSize; nil means use default.
	bufferSize        *int
	header            *Header
	ProxyHeaderPolicy Policy
	Validate          Validator
	readHeaderTimeout time.Duration
}

// Validator receives a header and decides whether it is a valid one
// In case the header is not deemed valid it should return an error.
type Validator func(*Header) error

// ValidateHeader adds given validator for proxy headers to a connection when passed as option to NewConn().
func ValidateHeader(v Validator) func(*Conn) {
	return func(c *Conn) {
		if v != nil {
			c.Validate = v
		}
	}
}

// SetReadHeaderTimeout sets the readHeaderTimeout for a connection when passed as option to NewConn().
// A value of 0 disables the header read timeout; negative values are ignored,
// leaving the connection's current timeout (the NewConn default) in place.
func SetReadHeaderTimeout(t time.Duration) func(*Conn) {
	return func(c *Conn) {
		if t >= 0 {
			c.readHeaderTimeout = t
		}
	}
}

// WithBufferSize sets the size of the read buffer used for proxy header detection.
// Values <= 0 are ignored and the default (256 bytes) is used. Values < 16 are
// effectively 16 due to bufio's minimum. The default is tuned for typical proxy
// protocol header lengths.
//
// The buffer must be able to hold an entire v1 header line (up to 107 bytes):
// v1 parsing requires the line to be available without refilling the buffer
// (the slow-loris defense), so a smaller buffer rejects every v1 connection
// with ErrCantReadVersion1Header even from well-behaved senders. v2 parsing
// refills freely and works with any size.
func WithBufferSize(length int) func(*Conn) {
	return func(c *Conn) {
		if length <= 0 {
			return
		}
		p := new(int)
		*p = length
		c.bufferSize = p
		c.bufReader = bufio.NewReaderSize(c.conn, length)
	}
}

// Accept waits for and returns the next valid connection to the listener.
func (p *Listener) Accept() (net.Conn, error) {
	for {
		// Get the underlying connection.
		conn, err := p.Listener.Accept()
		if err != nil {
			return nil, err
		}

		proxyHeaderPolicy := DefaultPolicy
		if p.Policy != nil && p.ConnPolicy != nil {
			panic("only one of policy or connpolicy must be provided.")
		}
		if p.Policy != nil || p.ConnPolicy != nil {
			if p.Policy != nil {
				proxyHeaderPolicy, err = p.Policy(conn.RemoteAddr())
			} else {
				proxyHeaderPolicy, err = p.ConnPolicy(ConnPolicyOptions{
					Upstream:   conn.RemoteAddr(),
					Downstream: conn.LocalAddr(),
				})
			}
			if err != nil {
				// can't decide the policy, we can't accept the connection.
				if closeErr := conn.Close(); closeErr != nil {
					return nil, closeErr
				}

				if errors.Is(err, ErrInvalidUpstream) {
					// keep listening for other connections.
					continue
				}

				return nil, err
			}
			// Handle a connection as a regular one.
			if proxyHeaderPolicy == SKIP {
				return conn, nil
			}
		}

		opts := []func(*Conn){
			WithPolicy(proxyHeaderPolicy),
			ValidateHeader(p.ValidateHeader),
		}
		if p.ReadBufferSize > 0 {
			opts = append(opts, WithBufferSize(p.ReadBufferSize))
		}
		newConn := NewConn(conn, opts...)

		// Set the readHeaderTimeout of the new conn to the value of the listener,
		// falling back to the default when unset. Read into a local rather than
		// writing back to the shared Listener: mutating p here races with
		// concurrent Accept calls and would silently rewrite the caller's struct.
		readHeaderTimeout := p.ReadHeaderTimeout
		if readHeaderTimeout == 0 {
			readHeaderTimeout = DefaultReadHeaderTimeout
		}
		newConn.readHeaderTimeout = readHeaderTimeout

		return newConn, nil
	}
}

// Close closes the underlying listener.
func (p *Listener) Close() error {
	return p.Listener.Close()
}

// Addr returns the underlying listener's network address.
func (p *Listener) Addr() net.Addr {
	return p.Listener.Addr()
}

// NewConn is used to wrap a net.Conn that may be speaking the PROXY protocol
// into a proxyproto.Conn.
//
// Conn is stream-oriented; see the note on Listener about the PROXY protocol
// over UDP datagrams.
//
// By default the returned Conn applies DefaultPolicy (REQUIRE, so the peer
// must open with a PROXY header; override with the WithPolicy option) and
// DefaultReadHeaderTimeout (10s) while detecting the PROXY protocol header, so
// a client that connects but never sends data cannot make header detection
// block forever.
//
// The timeout bounds header detection only, not the first Read end-to-end:
// under a non-REQUIRE policy, when no header is present Read falls through to
// a normal read of the underlying connection, which can still block on a
// silent client (pinning a goroutine and file descriptor). For an end-to-end
// bound, set a read deadline on the connection, or keep the REQUIRE policy,
// which makes the first Read fail when no header arrives within the timeout.
//
// Override the timeout with the SetReadHeaderTimeout option; pass
// SetReadHeaderTimeout(0) to disable it entirely.
//
// NOTE: NewConn may interfere with previously set ReadDeadline on the provided net.Conn,
// because it sets a temporary deadline when detecting and reading the PROXY protocol header.
// If you need to enforce a specific ReadDeadline on the connection, be sure to call Conn.SetReadDeadline
// again after NewConn returns, to restore your desired deadline.
func NewConn(conn net.Conn, opts ...func(*Conn)) *Conn {
	br := bufio.NewReaderSize(conn, readBufferSize)

	pConn := &Conn{
		bufReader:         br,
		conn:              conn,
		ProxyHeaderPolicy: DefaultPolicy,
		readHeaderTimeout: DefaultReadHeaderTimeout,
	}

	for _, opt := range opts {
		opt(pConn)
	}

	return pConn
}

// Read checks for the proxy protocol header on the first call, then reads
// from the connection. If there is an error processing the header, it is
// returned by this and every subsequent call. The connection is NOT closed by
// this package; the caller should close it.
func (p *Conn) Read(b []byte) (int, error) {
	// Ensure header processing runs at most once and surface any errors.
	if err := p.ensureHeaderProcessed(); err != nil {
		return 0, err
	}

	// Drain the buffer if it exists and has data.
	if p.bufReader != nil {
		if p.bufReader.Buffered() > 0 {
			n, err := p.bufReader.Read(b)

			// Did we empty the buffer?
			// Buffering a net.Conn means the buffer doesn't return io.EOF until the connection returns io.EOF.
			// Therefore, we use Buffered() == 0 to detect if we are done with the buffer.
			if p.bufReader.Buffered() == 0 {
				// Garbage collect the buffer.
				p.bufReader = nil
			}

			// Return immediately. Do not touch p.conn.
			// If err is EOF here, it means the connection is actually closed,
			// so we should return that error to the user anyway.
			return n, err
		}
		// If buffer was empty to begin with (shouldn't happen with the >0 check
		// but good for safety), clear it.
		p.bufReader = nil
	}

	// From now on, read directly from the underlying connection.
	return p.conn.Read(b)
}

// Write wraps original conn.Write.
func (p *Conn) Write(b []byte) (int, error) {
	// Ensure header processing has completed before writing.
	if err := p.ensureHeaderProcessed(); err != nil {
		return 0, err
	}
	return p.conn.Write(b)
}

// Close wraps original conn.Close.
func (p *Conn) Close() error {
	return p.conn.Close()
}

// ProxyHeader returns the proxy protocol header, if any. If an error occurs
// while reading the proxy header, nil is returned.
func (p *Conn) ProxyHeader() *Header {
	// Ensure header processing runs at most once.
	_ = p.ensureHeaderProcessed()
	return p.header
}

// LocalAddr returns the address of the server if the proxy
// protocol is being used, otherwise just returns the address of
// the socket server. In case an error happens on reading the
// proxy header the original LocalAddr is returned, not the one
// from the proxy header even if the proxy header itself is
// syntactically correct.
func (p *Conn) LocalAddr() net.Addr {
	// Ensure header processing runs at most once.
	_ = p.ensureHeaderProcessed()
	if p.header == nil || p.header.Command.IsLocal() || p.readErr != nil || p.header.DestinationAddr == nil {
		return p.conn.LocalAddr()
	}

	return p.header.DestinationAddr
}

// RemoteAddr returns the address of the client if the proxy
// protocol is being used, otherwise just returns the address of
// the socket peer. In case an error happens on reading the
// proxy header the original RemoteAddr is returned, not the one
// from the proxy header even if the proxy header itself is
// syntactically correct.
func (p *Conn) RemoteAddr() net.Addr {
	// Ensure header processing runs at most once.
	_ = p.ensureHeaderProcessed()
	if p.header == nil || p.header.Command.IsLocal() || p.readErr != nil || p.header.SourceAddr == nil {
		return p.conn.RemoteAddr()
	}

	return p.header.SourceAddr
}

// Raw returns the underlying connection which can be casted to
// a concrete type, allowing access to specialized functions.
//
// Use this ONLY if you know exactly what you are doing.
func (p *Conn) Raw() net.Conn {
	return p.conn
}

// TCPConn returns the underlying TCP connection,
// allowing access to specialized functions.
//
// Use this ONLY if you know exactly what you are doing.
func (p *Conn) TCPConn() (conn *net.TCPConn, ok bool) {
	conn, ok = p.conn.(*net.TCPConn)
	return
}

// UnixConn returns the underlying Unix socket connection,
// allowing access to specialized functions.
//
// Use this ONLY if you know exactly what you are doing.
func (p *Conn) UnixConn() (conn *net.UnixConn, ok bool) {
	conn, ok = p.conn.(*net.UnixConn)
	return
}

// UDPConn returns the underlying UDP connection,
// allowing access to specialized functions.
//
// Use this ONLY if you know exactly what you are doing.
func (p *Conn) UDPConn() (conn *net.UDPConn, ok bool) {
	conn, ok = p.conn.(*net.UDPConn)
	return
}

// SetDeadline wraps original conn.SetDeadline.
func (p *Conn) SetDeadline(t time.Time) error {
	p.readDeadline.Store(t)
	return p.conn.SetDeadline(t)
}

// SetReadDeadline wraps original conn.SetReadDeadline.
func (p *Conn) SetReadDeadline(t time.Time) error {
	// Set a local var that tells us the desired deadline. This is
	// needed in order to reset the read deadline to the one that is
	// desired by the user, rather than an empty deadline.
	p.readDeadline.Store(t)
	return p.conn.SetReadDeadline(t)
}

// SetWriteDeadline wraps original conn.SetWriteDeadline.
func (p *Conn) SetWriteDeadline(t time.Time) error {
	return p.conn.SetWriteDeadline(t)
}

// readHeader reads the proxy protocol header from the connection.
func (p *Conn) readHeader() error {
	// If the connection's readHeaderTimeout is more than 0,
	// apply a temporary deadline without extending a user-configured
	// deadline. If the user has no deadline, we use now + timeout.
	if p.readHeaderTimeout > 0 {
		var (
			storedDeadline time.Time
			hasDeadline    bool
		)
		if t := p.readDeadline.Load(); t != nil {
			storedDeadline = t.(time.Time)
			hasDeadline = !storedDeadline.IsZero()
		}

		headerDeadline := time.Now().Add(p.readHeaderTimeout)
		if hasDeadline && storedDeadline.Before(headerDeadline) {
			// Clamp to the user's earlier deadline to avoid extending it.
			headerDeadline = storedDeadline
		}

		if err := p.conn.SetReadDeadline(headerDeadline); err != nil {
			return err
		}
	}

	header, err := Read(p.bufReader)

	// If the connection's readHeaderTimeout is more than 0, undo the change to the
	// deadline that we made above. Because we retain the readDeadline as part of our
	// SetReadDeadline override, we can restore the user's deadline (if any).
	// Therefore, we check whether the error is a net.Timeout and if it is, we decide
	// the proxy proto does not exist and set the error accordingly.
	if p.readHeaderTimeout > 0 {
		t := p.readDeadline.Load()
		if t == nil {
			t = time.Time{}
		}
		// Restore the user's deadline on a best-effort basis. This must not
		// discard a header we already parsed: some connections (notably
		// net.Pipe) return an error from SetReadDeadline once the peer has
		// closed, which can happen right after the peer sends the header and
		// closes. Only surface a restore failure when the read produced neither
		// a header nor an error of its own.
		restoreErr := p.conn.SetReadDeadline(t.(time.Time))
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			err = ErrNoProxyProtocol
		} else if err == nil && header == nil {
			err = restoreErr
		}
	}

	// For the purpose of this wrapper shamefully stolen from armon/go-proxyproto
	// let's act as if there was no error when PROXY protocol is not present.
	if err == ErrNoProxyProtocol {
		// but not if it is required that the connection has one
		if p.ProxyHeaderPolicy == REQUIRE {
			return err
		}

		return nil
	}

	// proxy protocol header was found
	if err == nil && header != nil {
		switch p.ProxyHeaderPolicy {
		case REJECT:
			// this connection is not allowed to send one
			return ErrSuperfluousProxyHeader
		case USE, REQUIRE:
			if p.Validate != nil {
				err = p.Validate(header)
				if err != nil {
					return err
				}
			}

			p.header = header
		}
	}

	return err
}

// ensureHeaderProcessed runs header processing once.
func (p *Conn) ensureHeaderProcessed() error {
	p.once.Do(func() {
		p.readErr = p.readHeader()
	})
	if p.readErr != nil {
		return p.readErr
	}
	return nil
}

// ReadFrom implements the io.ReaderFrom ReadFrom method.
func (p *Conn) ReadFrom(r io.Reader) (int64, error) {
	// Ensure header processing has completed before reading/writing.
	if err := p.ensureHeaderProcessed(); err != nil {
		return 0, err
	}
	if rf, ok := p.conn.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(p.conn, r)
}

// WriteTo implements io.WriterTo.
func (p *Conn) WriteTo(w io.Writer) (int64, error) {
	// Ensure header processing has completed before reading/writing.
	if err := p.ensureHeaderProcessed(); err != nil {
		return 0, err
	}

	// If the buffer has been drained (or cleared), copy directly from conn.
	if p.bufReader == nil {
		return io.Copy(w, p.conn)
	}

	b := make([]byte, p.bufReader.Buffered())
	if _, err := p.bufReader.Read(b); err != nil {
		return 0, err // this should never happen as we read buffered data.
	}

	var n int64
	{
		nn, err := w.Write(b)
		n += int64(nn)
		if err != nil {
			return n, err
		}
	}
	{
		nn, err := io.Copy(w, p.conn)
		n += nn
		if err != nil {
			return n, err
		}
	}

	return n, nil
}
