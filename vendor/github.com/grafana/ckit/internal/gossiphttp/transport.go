// Package gossiphttp implements an HTTP/2 transport for gossip.
//
// # Protocol
//
// Peers send two types of messages to each other:
//
//  1. /api/v1/ckit/transport/message sends a stream of messages to a peer. The
//     receiver does not respond with any messages.
//
//  2. /api/v1/ckit/transport/stream opens a bidirectional communication
//     channel to a peer, where both peers may send messages to each other. Once
//     either peer closes the connection, the stream is terminated.
//
// Both requests expect the Content-Type header to be set to
// application/x.ckit.
//
// Requests MUST be delivered over HTTP/2. HTTP/1.X requests will be rejected
// with HTTP 505 HTTP Version Not Supported.
//
// # Message Format
//
// All messages sent between peers have the same format:
//
//	+------------------------+
//	| Magic = 0xCC <1  byte> |
//	|------------------------|
//	| Data length  <2 bytes> |
//	|------------------------|
//	| Data         <n bytes> |
//	+------------------------+
//
// It is recommended that the message data size kept within the UDP MTU size,
// normally 1400 bytes. The message data size must not exeed 65,535 bytes.
package gossiphttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/ckit/internal/queue"
	"github.com/hashicorp/memberlist"
	"github.com/prometheus/client_golang/prometheus"
)

// packetBufferSize is the maximum amount of packets that can be held in
// memory. Packets buffers are an LRU cache, so the oldest non-dequeued packet
// is discarded after a bufer is full.
const packetBufferSize = 1000

const (
	contentTypeHeader = "Content-Type"
	// ckitContentType is the value of the Content Type header that gossiphttp
	// messages must use.
	ckitContentType = "application/x.ckit"
)

// API endpoints used for messaging.
var (
	baseRoute = "/api/v1/ckit/transport/"

	// messageEndpoint is used to send one or more messages to a peer.
	messageEndpoint = baseRoute + "message"

	// streamEndpoint is used to open a communication stream to a peer where they
	// can exchange larger amounts of information.
	streamEndpoint = baseRoute + "stream"
)

// Options controls the gossiphttp transport.
type Options struct {
	// Optional logger to use.
	Log log.Logger

	// Client to use for communicating to peers. Required. The Transport used
	// by the client must be able to handle HTTP/2 requests for any peer.
	//
	// Note that TLS is not required for communication between peers. The
	// Client.Transport should be able to fall back to h2c for HTTP/2 traffic
	// when connections over HTTPS are not used.
	Client *http.Client

	// Timeout to use when sending a packet.
	PacketTimeout time.Duration

	// UseHTTPS defines whether TLS should be used for communication between
	// peers.
	UseHTTPS bool
}

// Transport is an HTTP/2 implementation of memberlist.Transport. Call
// NewTransport to create one.
type Transport struct {
	log     log.Logger
	opts    Options
	metrics *metrics

	// memberlist is designed for UDP, which is nearly non-blocking for writes.
	// We need to be able to emulate the same performance of passing messages, so
	// we write messages to buffered queues which are processed in the
	// background.
	inPacketQueue, outPacketQueue *queue.Queue

	inPacketCh chan *memberlist.Packet
	streamCh   chan net.Conn

	// Incoming packets and streams should be rejected when the transport is
	// closed.
	closedMut sync.RWMutex
	exited    chan struct{}
	cancel    context.CancelFunc
	hasExited atomic.Bool

	// Generated after calling FinalAdvertiseAddr
	localAddr net.Addr
}

var (
	_ memberlist.Transport          = (*Transport)(nil)
	_ memberlist.NodeAwareTransport = (*Transport)(nil)
)

// http2Stream implements the streamClient interface and allows for sending and
// receiving messages in a bidirectional streaming connection.
type http2Stream struct {
	r io.ReadCloser
	w io.Writer
}

// Send sends b over an http2Stream connection.
func (sc *http2Stream) Send(b []byte) error {
	return writeMessage(sc.w, b)
}

// Recv reads from an http2Stream connection.
func (sc *http2Stream) Recv() ([]byte, error) {
	return readMessage(sc.r)
}

var _ io.WriteCloser = (*flushWriter)(nil)

// flushWriter wraps an io.Writer with an http.Flusher to flush buffered data
// to a streaming HTTP/2 connection's request body.
type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func (w *flushWriter) Write(data []byte) (int, error) {
	n, err := w.w.Write(data)
	w.f.Flush()
	return n, err
}

func (w *flushWriter) Close() error { return nil }

// NewTransport returns a new Transport. Transports must be attached to an HTTP
// server so their endpoints are invoked. See [Handler] for more information.
func NewTransport(opts Options) (*Transport, prometheus.Collector, error) {
	if opts.Client == nil {
		return nil, nil, fmt.Errorf("HTTP client must be provided")
	}

	l := opts.Log
	if l == nil {
		l = log.NewNopLogger()
	}

	ctx, cancel := context.WithCancel(context.Background())

	t := &Transport{
		log:     l,
		opts:    opts,
		metrics: newMetrics(),

		inPacketQueue:  queue.New(packetBufferSize),
		outPacketQueue: queue.New(packetBufferSize),

		inPacketCh: make(chan *memberlist.Packet),
		streamCh:   make(chan net.Conn),

		exited: make(chan struct{}),
		cancel: cancel,
	}

	t.metrics.Add(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "cluster_transport_rx_packet_queue_length",
			Help: "Current number of unprocessed incoming packets",
		},
		func() float64 { return float64(t.inPacketQueue.Size()) },
	))
	t.metrics.Add(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "cluster_transport_tx_packet_queue_length",
			Help: "Current number of unprocessed outgoing packets",
		},
		func() float64 { return float64(t.outPacketQueue.Size()) },
	))

	go t.run(ctx)

	return t, t.metrics, nil
}

func (t *Transport) run(ctx context.Context) {
	defer close(t.exited)

	var wg sync.WaitGroup
	wg.Add(2)
	defer wg.Wait()

	// Close our queues before shutting down. This must be done before calling
	// wg.Wait as it will cause the goroutines to exit.
	defer func() { _ = t.inPacketQueue.Close() }()
	defer func() { _ = t.outPacketQueue.Close() }()

	// Process queue of incoming packets
	go func() {
		defer wg.Done()

		for {
			v, err := t.inPacketQueue.Dequeue(ctx)
			if err != nil {
				return
			}

			pkt := v.(*memberlist.Packet)
			t.metrics.packetRxTotal.Inc()
			t.metrics.packetRxBytesTotal.Add(float64(len(pkt.Buf)))

			select {
			case <-ctx.Done():
			case t.inPacketCh <- pkt:
			}
		}
	}()

	// Process queue of outgoing packets
	go func() {
		defer wg.Done()

		for {
			v, err := t.outPacketQueue.Dequeue(ctx)
			if err != nil {
				return
			}

			pkt := v.(*outPacket)
			t.metrics.packetTxTotal.Inc()
			t.metrics.packetTxBytesTotal.Add(float64(len(pkt.Data)))
			t.writeToSync(pkt.Data, pkt.Addr)
		}
	}()

	<-ctx.Done()
}

// Handler returns the base HTTP route and handler for the Transport.
func (t *Transport) Handler() (route string, handler http.Handler) {
	mux := http.NewServeMux()
	mux.Handle(messageEndpoint, http.HandlerFunc(t.handleMessage))
	mux.Handle(streamEndpoint, http.HandlerFunc(t.handleStream))
	return baseRoute, mux
}

// handleMessage is used for single-packet communication.
func (t *Transport) handleMessage(w http.ResponseWriter, r *http.Request) {
	if t.hasExited.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if r.ProtoMajor != 2 {
		w.WriteHeader(http.StatusHTTPVersionNotSupported)
		return
	}
	if r.Header.Get(contentTypeHeader) != ckitContentType {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var (
		recvTime   = time.Now()
		remoteAddr = parseRemoteAddr(r.RemoteAddr)
	)

	// Read each message until the request body has been fully consumed. Each
	// message is converted into a single packet.
	for {
		msg, err := readMessage(r.Body)
		if errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			level.Warn(t.log).Log("msg", "error reading packet from peer", "err", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		t.metrics.packetRxTotal.Inc()
		t.metrics.packetRxBytesTotal.Add(float64(len(msg)))

		// Enqueue the packet to be processed in the background. This allows
		// HTTP calls to have as low of a latency as possible to help keep
		// things moving along.
		t.inPacketQueue.Enqueue(&memberlist.Packet{
			Buf:       msg,
			From:      remoteAddr,
			Timestamp: recvTime,
		})
	}

	w.WriteHeader(http.StatusOK)
}

// parseRemoteAddr parses a ip:port string into a net.Addr. If the addr cannot
// be parsed, a default implementation for an "unknown" net.Addr is returned.
func parseRemoteAddr(addr string) net.Addr {
	remoteHost, remoteService, err := net.SplitHostPort(addr)
	if err != nil {
		return unknownAddr{}
	}

	remoteIP := net.ParseIP(remoteHost)
	if remoteIP == nil {
		return unknownAddr{}
	}

	remotePort, err := net.LookupPort("tcp", remoteService)
	if err != nil {
		return unknownAddr{}
	}

	return &net.TCPAddr{
		IP:   remoteIP,
		Port: remotePort,
	}
}

// handleStream is used for streaming bidirectional communication.
func (t *Transport) handleStream(w http.ResponseWriter, r *http.Request) {
	if t.hasExited.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if r.ProtoMajor != 2 {
		w.WriteHeader(http.StatusHTTPVersionNotSupported)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusHTTPVersionNotSupported)
		return
	}

	if r.Header.Get(contentTypeHeader) != ckitContentType {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	waitClosed := make(chan struct{})

	var readMut sync.Mutex
	readCnd := sync.NewCond(&readMut)

	t.metrics.openStreams.Inc()
	defer t.metrics.openStreams.Dec()

	packetsClient := &http2Stream{
		r: r.Body,
		w: &flushWriter{w: w, f: flusher},
	}

	conn := &packetsClientConn{
		cli:     packetsClient,
		onClose: func() { close(waitClosed) },
		closed:  make(chan struct{}),
		metrics: t.metrics,

		localAddr:  t.localAddr,
		remoteAddr: parseRemoteAddr(r.RemoteAddr),

		readCnd:      readCnd,
		readMessages: make(chan readResult),
	}

	t.streamCh <- conn
	<-waitClosed
}

// WriteToAddress implements NodeAwareTransport.
func (t *Transport) WriteToAddress(b []byte, addr memberlist.Address) (time.Time, error) {
	return t.WriteTo(b, addr.Addr)
}

// DialAddressTimeout implements NodeAwareTransport.
func (t *Transport) DialAddressTimeout(addr memberlist.Address, timeout time.Duration) (net.Conn, error) {
	return t.DialTimeout(addr.Addr, timeout)
}

// FinalAdvertiseAddr returns the address this peer uses to advertise its
// connections.
func (t *Transport) FinalAdvertiseAddr(ip string, port int) (net.IP, int, error) {
	if ip == "" {
		return nil, 0, fmt.Errorf("no configured advertise address")
	} else if port == 0 {
		return nil, 0, fmt.Errorf("missing real listen port")
	}

	advertiseIP := net.ParseIP(ip)
	if advertiseIP == nil {
		return nil, 0, fmt.Errorf("failed to parse advertise ip %q", ip)
	}

	// Convert to IPv4 if possible.
	if ip4 := advertiseIP.To4(); ip4 != nil {
		advertiseIP = ip4
	}

	t.localAddr = &net.TCPAddr{IP: advertiseIP, Port: port}
	return advertiseIP, port, nil
}

type outPacket struct {
	Data []byte
	Addr string
}

// WriteTo enqueues a message b to be sent to the peer specified by addr. The
// message is delivered in the background asynchronously by the transport.
func (t *Transport) WriteTo(b []byte, addr string) (time.Time, error) {
	t.outPacketQueue.Enqueue(&outPacket{Data: b, Addr: addr})
	return time.Now(), nil
}

// PacketCh returns a channel of packets received from remote peers.
func (t *Transport) PacketCh() <-chan *memberlist.Packet {
	return t.inPacketCh
}

// DialTimeout opens a bidirectional communication channel to the specified
// peer address.
func (t *Transport) DialTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}

	scheme := "http"
	if t.opts.UseHTTPS {
		scheme = "https"
	}

	var readMut sync.Mutex
	readCnd := sync.NewCond(&readMut)

	pr, pw := io.Pipe()

	req := &http.Request{
		Method: http.MethodPost,
		URL: &url.URL{
			Scheme: scheme,
			Host:   addr,
			Path:   streamEndpoint,
		},
		Header:     http.Header{},
		Proto:      "HTTP/2",
		ProtoMajor: 2,
		ProtoMinor: 0,
		Body:       pr,
	}
	req.Header.Set(contentTypeHeader, ckitContentType)
	req = req.WithContext(ctx)

	resp, err := t.opts.Client.Do(req)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, err
	}

	t.metrics.openStreams.Inc()

	packetsClient := &http2Stream{
		r: resp.Body,
		w: pw,
	}

	return &packetsClientConn{
		cli: packetsClient,

		onClose: func() {
			t.metrics.openStreams.Dec()
			if cancel != nil {
				cancel()
			}
		},
		closed:  make(chan struct{}),
		metrics: t.metrics,

		localAddr:  t.localAddr,
		remoteAddr: parseRemoteAddr(addr),

		readCnd:      readCnd,
		readMessages: make(chan readResult),
	}, nil
}

// StreamCh returns a channel of bidirectional communication channels opened by
// remote peers.
func (t *Transport) StreamCh() <-chan net.Conn {
	return t.streamCh
}

// Shutdown terminates the transport.
func (t *Transport) Shutdown() error {
	t.closedMut.Lock()
	defer t.closedMut.Unlock()
	t.cancel()
	t.hasExited.Store(true)
	<-t.exited
	return nil
}

func (t *Transport) writeToSync(b []byte, addr string) {
	ctx := context.Background()
	if t.opts.PacketTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.opts.PacketTimeout)
		defer cancel()
	}

	scheme := "http"
	if t.opts.UseHTTPS {
		scheme = "https"
	}

	bb := bytes.NewBuffer(nil)
	err := writeMessage(bb, b)
	if err != nil {
		level.Debug(t.log).Log("msg", "failed to encode message", "err", err)
		t.metrics.packetTxFailedTotal.Inc()
		return
	}

	req := &http.Request{
		Method: http.MethodPost,
		URL: &url.URL{
			Scheme: scheme,
			Host:   addr,
			Path:   messageEndpoint,
		},
		Header:     http.Header{},
		Proto:      "HTTP/2",
		ProtoMajor: 2,
		ProtoMinor: 0,
		Body:       io.NopCloser(bb),
	}
	req.Header.Set(contentTypeHeader, ckitContentType)
	req = req.WithContext(ctx)

	req.Header.Set("Content-Type", ckitContentType)
	resp, err := t.opts.Client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		level.Debug(t.log).Log("msg", "failed to send message", "err", err)
		t.metrics.packetTxFailedTotal.Inc()
	}
}
