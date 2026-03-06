// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"

	internaljson "github.com/modelcontextprotocol/go-sdk/internal/json"
	"github.com/modelcontextprotocol/go-sdk/internal/jsonrpc2"
	"github.com/modelcontextprotocol/go-sdk/internal/xcontext"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// ErrConnectionClosed is returned when sending a message to a connection that
// is closed or in the process of closing.
var ErrConnectionClosed = errors.New("connection closed")

// A Transport is used to create a bidirectional connection between MCP client
// and server.
//
// Transports should be used for at most one call to [Server.Connect] or
// [Client.Connect].
type Transport interface {
	// Connect returns the logical JSON-RPC connection..
	//
	// It is called exactly once by [Server.Connect] or [Client.Connect].
	Connect(ctx context.Context) (Connection, error)
}

// A Connection is a logical bidirectional JSON-RPC connection.
type Connection interface {
	// Read reads the next message to process off the connection.
	//
	// Connections must allow Read to be called concurrently with Close. In
	// particular, calling Close should unblock a Read waiting for input.
	Read(context.Context) (jsonrpc.Message, error)

	// Write writes a new message to the connection.
	//
	// Write may be called concurrently, as calls or responses may occur
	// concurrently in user code.
	Write(context.Context, jsonrpc.Message) error

	// Close closes the connection. It is implicitly called whenever a Read or
	// Write fails.
	//
	// Close may be called multiple times, potentially concurrently.
	Close() error

	// TODO(#148): remove SessionID from this interface.
	SessionID() string
}

// A ClientConnection is a [Connection] that is specific to the MCP client.
//
// If client connections implement this interface, they may receive information
// about changes to the client session.
//
// TODO: should this interface be exported?
type clientConnection interface {
	Connection

	// sessionUpdated is called whenever the client session state changes.
	sessionUpdated(clientSessionState)
}

// A serverConnection is a Connection that is specific to the MCP server.
//
// If server connections implement this interface, they receive information
// about changes to the server session.
//
// TODO: should this interface be exported?
type serverConnection interface {
	Connection
	sessionUpdated(ServerSessionState)
}

// A StdioTransport is a [Transport] that communicates over stdin/stdout using
// newline-delimited JSON.
type StdioTransport struct{}

// Connect implements the [Transport] interface.
func (*StdioTransport) Connect(context.Context) (Connection, error) {
	return newIOConn(rwc{os.Stdin, nopCloserWriter{os.Stdout}}), nil
}

// nopCloserWriter is an io.WriteCloser with a trivial Close method.
type nopCloserWriter struct {
	io.Writer
}

func (nopCloserWriter) Close() error { return nil }

// An IOTransport is a [Transport] that communicates over separate
// io.ReadCloser and io.WriteCloser using newline-delimited JSON.
type IOTransport struct {
	Reader io.ReadCloser
	Writer io.WriteCloser
}

// Connect implements the [Transport] interface.
func (t *IOTransport) Connect(context.Context) (Connection, error) {
	return newIOConn(rwc{t.Reader, t.Writer}), nil
}

// An InMemoryTransport is a [Transport] that communicates over an in-memory
// network connection, using newline-delimited JSON.
//
// InMemoryTransports should be constructed using [NewInMemoryTransports],
// which returns two transports connected to each other.
type InMemoryTransport struct {
	rwc io.ReadWriteCloser
}

// Connect implements the [Transport] interface.
func (t *InMemoryTransport) Connect(context.Context) (Connection, error) {
	return newIOConn(t.rwc), nil
}

// NewInMemoryTransports returns two [InMemoryTransport] objects that connect
// to each other.
//
// The resulting transports are symmetrical: use either to connect to a server,
// and then the other to connect to a client. Servers must be connected before
// clients, as the client initializes the MCP session during connection.
func NewInMemoryTransports() (*InMemoryTransport, *InMemoryTransport) {
	c1, c2 := net.Pipe()
	return &InMemoryTransport{c1}, &InMemoryTransport{c2}
}

type binder[T handler, State any] interface {
	// TODO(rfindley): the bind API has gotten too complicated. Simplify.
	bind(Connection, *jsonrpc2.Connection, State, func()) T
	disconnect(T)
}

type handler interface {
	handle(ctx context.Context, req *jsonrpc.Request) (any, error)
}

func connect[H handler, State any](ctx context.Context, t Transport, b binder[H, State], s State, onClose func()) (H, error) {
	var zero H
	mcpConn, err := t.Connect(ctx)
	if err != nil {
		return zero, err
	}
	// If logging is configured, write message logs.
	reader, writer := jsonrpc2.Reader(mcpConn), jsonrpc2.Writer(mcpConn)
	var (
		h         H
		preempter canceller
	)
	bind := func(conn *jsonrpc2.Connection) jsonrpc2.Handler {
		h = b.bind(mcpConn, conn, s, onClose)
		preempter.conn = conn
		return jsonrpc2.HandlerFunc(h.handle)
	}
	_ = jsonrpc2.NewConnection(ctx, jsonrpc2.ConnectionConfig{
		Reader:    reader,
		Writer:    writer,
		Closer:    mcpConn,
		Bind:      bind,
		Preempter: &preempter,
		OnDone: func() {
			b.disconnect(h)
		},
		OnInternalError: func(err error) { log.Printf("jsonrpc2 error: %v", err) },
	})
	assert(preempter.conn != nil, "unbound preempter")
	return h, nil
}

// A canceller is a jsonrpc2.Preempter that cancels in-flight requests on MCP
// cancelled notifications.
type canceller struct {
	conn *jsonrpc2.Connection
}

// Preempt implements [jsonrpc2.Preempter].
func (c *canceller) Preempt(ctx context.Context, req *jsonrpc.Request) (result any, err error) {
	if req.Method == notificationCancelled {
		var params CancelledParams
		if err := internaljson.Unmarshal(req.Params, &params); err != nil {
			return nil, err
		}
		id, err := jsonrpc2.MakeID(params.RequestID)
		if err != nil {
			return nil, err
		}
		go c.conn.Cancel(id)
	}
	return nil, jsonrpc2.ErrNotHandled
}

// call executes and awaits a jsonrpc2 call on the given connection,
// translating errors into the mcp domain.
func call(ctx context.Context, conn *jsonrpc2.Connection, method string, params Params, result Result) error {
	// The "%w"s in this function expose jsonrpc.Error as part of the API.
	call := conn.Call(ctx, method, params)
	err := call.Await(ctx, result)
	switch {
	case errors.Is(err, jsonrpc2.ErrClientClosing), errors.Is(err, jsonrpc2.ErrServerClosing):
		return fmt.Errorf("%w: calling %q: %v", ErrConnectionClosed, method, err)
	case ctx.Err() != nil:
		// Notify the peer of cancellation.
		err := conn.Notify(xcontext.Detach(ctx), notificationCancelled, &CancelledParams{
			Reason:    ctx.Err().Error(),
			RequestID: call.ID().Raw(),
		})
		// By default, the jsonrpc2 library waits for graceful shutdown when the
		// connection is closed, meaning it expects all outgoing and incoming
		// requests to complete. However, for MCP this expectation is unrealistic,
		// and can lead to hanging shutdown. For example, if a streamable client is
		// killed, the server will not be able to detect this event, except via
		// keepalive pings (if they are configured), and so outgoing calls may hang
		// indefinitely.
		//
		// Therefore, we choose to eagerly retire calls, removing them from the
		// outgoingCalls map, when the caller context is cancelled: if the caller
		// will never receive the response, there's no need to track it.
		conn.Retire(call, ctx.Err())
		return errors.Join(ctx.Err(), err)
	case err != nil:
		return fmt.Errorf("calling %q: %w", method, err)
	}
	return nil
}

// A LoggingTransport is a [Transport] that delegates to another transport,
// writing RPC logs to an io.Writer.
type LoggingTransport struct {
	Transport Transport
	Writer    io.Writer
}

// Connect connects the underlying transport, returning a [Connection] that writes
// logs to the configured destination.
func (t *LoggingTransport) Connect(ctx context.Context) (Connection, error) {
	delegate, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &loggingConn{delegate: delegate, w: t.Writer}, nil
}

type loggingConn struct {
	delegate Connection

	mu sync.Mutex
	w  io.Writer
}

func (c *loggingConn) SessionID() string { return c.delegate.SessionID() }

// Read is a stream middleware that logs incoming messages.
func (s *loggingConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := s.delegate.Read(ctx)

	if err != nil {
		s.mu.Lock()
		fmt.Fprintf(s.w, "read error: %v\n", err)
		s.mu.Unlock()
	} else {
		data, err := jsonrpc2.EncodeMessage(msg)
		s.mu.Lock()
		if err != nil {
			fmt.Fprintf(s.w, "LoggingTransport: failed to marshal: %v", err)
		}
		fmt.Fprintf(s.w, "read: %s\n", string(data))
		s.mu.Unlock()
	}

	return msg, err
}

// Write is a stream middleware that logs outgoing messages.
func (s *loggingConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	err := s.delegate.Write(ctx, msg)
	if err != nil {
		s.mu.Lock()
		fmt.Fprintf(s.w, "write error: %v\n", err)
		s.mu.Unlock()
	} else {
		data, err := jsonrpc2.EncodeMessage(msg)
		s.mu.Lock()
		if err != nil {
			fmt.Fprintf(s.w, "LoggingTransport: failed to marshal: %v", err)
		}
		fmt.Fprintf(s.w, "write: %s\n", string(data))
		s.mu.Unlock()
	}
	return err
}

func (s *loggingConn) Close() error {
	return s.delegate.Close()
}

// A rwc binds an io.ReadCloser and io.WriteCloser together to create an
// io.ReadWriteCloser.
type rwc struct {
	rc io.ReadCloser
	wc io.WriteCloser
}

func (r rwc) Read(p []byte) (n int, err error) {
	return r.rc.Read(p)
}

func (r rwc) Write(p []byte) (n int, err error) {
	return r.wc.Write(p)
}

func (r rwc) Close() error {
	rcErr := r.rc.Close()

	var wcErr error
	if r.wc != nil { // we only allow a nil writer in unit tests
		wcErr = r.wc.Close()
	}

	return errors.Join(rcErr, wcErr)
}

// An ioConn is a transport that delimits messages with newlines across
// a bidirectional stream, and supports jsonrpc.2 message batching.
//
// See https://github.com/ndjson/ndjson-spec for discussion of newline
// delimited JSON.
//
// See [msgBatch] for more discussion of message batching.
type ioConn struct {
	protocolVersion string // negotiated version, set during session initialization.

	writeMu sync.Mutex         // guards Write, which must be concurrency safe.
	rwc     io.ReadWriteCloser // the underlying stream

	// incoming receives messages from the read loop started in [newIOConn].
	incoming <-chan msgOrErr

	// If outgoiBatch has a positive capacity, it will be used to batch requests
	// and notifications before sending.
	outgoingBatch []jsonrpc.Message

	// Unread messages in the last batch. Since reads are serialized, there is no
	// need to guard here.
	queue []jsonrpc.Message

	// batches correlate incoming requests to the batch in which they arrived.
	// Since writes may be concurrent to reads, we need to guard this with a mutex.
	batchMu sync.Mutex
	batches map[jsonrpc2.ID]*msgBatch // lazily allocated

	closeOnce sync.Once
	closed    chan struct{}
	closeErr  error
}

type msgOrErr struct {
	msg json.RawMessage
	err error
}

func newIOConn(rwc io.ReadWriteCloser) *ioConn {
	var (
		incoming = make(chan msgOrErr)
		closed   = make(chan struct{})
	)
	// Start a goroutine for reads, so that we can select on the incoming channel
	// in [ioConn.Read] and unblock the read as soon as Close is called (see #224).
	//
	// This leaks a goroutine if rwc.Read does not unblock after it is closed,
	// but that is unavoidable since AFAIK there is no (easy and portable) way to
	// guarantee that reads of stdin are unblocked when closed.
	go func() {
		dec := json.NewDecoder(rwc)
		for {
			var raw json.RawMessage
			err := dec.Decode(&raw)
			// If decoding was successful, check for trailing data at the end of the stream.
			if err == nil {
				// Read the next byte to check if there is trailing data.
				var tr [1]byte
				if n, readErr := dec.Buffered().Read(tr[:]); n > 0 {
					// If read byte is not a newline, it is an error.
					// Support both Unix (\n) and Windows (\r\n) line endings.
					if tr[0] != '\n' && tr[0] != '\r' {
						err = fmt.Errorf("invalid trailing data at the end of stream")
					}
				} else if readErr != nil && readErr != io.EOF {
					err = readErr
				}
			}
			select {
			case incoming <- msgOrErr{msg: raw, err: err}:
			case <-closed:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return &ioConn{
		rwc:      rwc,
		incoming: incoming,
		closed:   closed,
	}
}

func (c *ioConn) SessionID() string { return "" }

func (c *ioConn) sessionUpdated(state ServerSessionState) {
	protocolVersion := ""
	if state.InitializeParams != nil {
		protocolVersion = state.InitializeParams.ProtocolVersion
	}
	if protocolVersion == "" {
		protocolVersion = protocolVersion20250326
	}
	c.protocolVersion = negotiatedVersion(protocolVersion)
}

// addBatch records a msgBatch for an incoming batch payload.
// It returns an error if batch is malformed, containing previously seen IDs.
//
// See [msgBatch] for more.
func (t *ioConn) addBatch(batch *msgBatch) error {
	t.batchMu.Lock()
	defer t.batchMu.Unlock()
	for id := range batch.unresolved {
		if _, ok := t.batches[id]; ok {
			return fmt.Errorf("%w: batch contains previously seen request %v", jsonrpc2.ErrInvalidRequest, id.Raw())
		}
	}
	for id := range batch.unresolved {
		if t.batches == nil {
			t.batches = make(map[jsonrpc2.ID]*msgBatch)
		}
		t.batches[id] = batch
	}
	return nil
}

// updateBatch records a response in the message batch tracking the
// corresponding incoming call, if any.
//
// The second result reports whether resp was part of a batch. If this is true,
// the first result is nil if the batch is still incomplete, or the full set of
// batch responses if resp completed the batch.
func (t *ioConn) updateBatch(resp *jsonrpc.Response) ([]*jsonrpc.Response, bool) {
	t.batchMu.Lock()
	defer t.batchMu.Unlock()

	if batch, ok := t.batches[resp.ID]; ok {
		idx, ok := batch.unresolved[resp.ID]
		if !ok {
			panic("internal error: inconsistent batches")
		}
		batch.responses[idx] = resp
		delete(batch.unresolved, resp.ID)
		delete(t.batches, resp.ID)
		if len(batch.unresolved) == 0 {
			return batch.responses, true
		}
		return nil, true
	}
	return nil, false
}

// A msgBatch records information about an incoming batch of jsonrpc.2 calls.
//
// The jsonrpc.2 spec (https://www.jsonrpc.org/specification#batch) says:
//
// "The Server should respond with an Array containing the corresponding
// Response objects, after all of the batch Request objects have been
// processed. A Response object SHOULD exist for each Request object, except
// that there SHOULD NOT be any Response objects for notifications. The Server
// MAY process a batch rpc call as a set of concurrent tasks, processing them
// in any order and with any width of parallelism."
//
// Therefore, a msgBatch keeps track of outstanding calls and their responses.
// When there are no unresolved calls, the response payload is sent.
type msgBatch struct {
	unresolved map[jsonrpc2.ID]int
	responses  []*jsonrpc.Response
}

func (t *ioConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	// As a matter of principle, enforce that reads on a closed context return an
	// error.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if len(t.queue) > 0 {
		next := t.queue[0]
		t.queue = t.queue[1:]
		return next, nil
	}

	var raw json.RawMessage
	select {
	case <-ctx.Done():
		return nil, ctx.Err()

	case v := <-t.incoming:
		if v.err != nil {
			return nil, v.err
		}
		raw = v.msg

	case <-t.closed:
		return nil, io.EOF
	}

	msgs, batch, err := readBatch(raw)
	if err != nil {
		return nil, err
	}
	if batch && t.protocolVersion >= protocolVersion20250618 {
		return nil, fmt.Errorf("JSON-RPC batching is not supported in %s and later (request version: %s)", protocolVersion20250618, t.protocolVersion)
	}

	t.queue = msgs[1:]

	if batch {
		var respBatch *msgBatch // track incoming requests in the batch
		for _, msg := range msgs {
			if req, ok := msg.(*jsonrpc.Request); ok {
				if respBatch == nil {
					respBatch = &msgBatch{
						unresolved: make(map[jsonrpc2.ID]int),
					}
				}
				if _, ok := respBatch.unresolved[req.ID]; ok {
					return nil, fmt.Errorf("duplicate message ID %q", req.ID)
				}
				respBatch.unresolved[req.ID] = len(respBatch.responses)
				respBatch.responses = append(respBatch.responses, nil)
			}
		}
		if respBatch != nil {
			// The batch contains one or more incoming requests to track.
			if err := t.addBatch(respBatch); err != nil {
				return nil, err
			}
		}
	}
	return msgs[0], err
}

// readBatch reads batch data, which may be either a single JSON-RPC message,
// or an array of JSON-RPC messages.
func readBatch(data []byte) (msgs []jsonrpc.Message, isBatch bool, _ error) {
	// Try to read an array of messages first.
	var rawBatch []json.RawMessage
	if err := internaljson.Unmarshal(data, &rawBatch); err == nil {
		if len(rawBatch) == 0 {
			return nil, true, fmt.Errorf("empty batch")
		}
		for _, raw := range rawBatch {
			msg, err := jsonrpc2.DecodeMessage(raw)
			if err != nil {
				return nil, true, err
			}
			msgs = append(msgs, msg)
		}
		return msgs, true, nil
	}
	// Try again with a single message.
	msg, err := jsonrpc2.DecodeMessage(data)
	return []jsonrpc.Message{msg}, false, err
}

func (t *ioConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	// As in [ioConn.Read], enforce that Writes on a closed context are an error.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	// Batching support: if msg is a Response, it may have completed a batch, so
	// check that first. Otherwise, it is a request or notification, and we may
	// want to collect it into a batch before sending, if we're configured to use
	// outgoing batches.
	if resp, ok := msg.(*jsonrpc.Response); ok {
		if batch, ok := t.updateBatch(resp); ok {
			if len(batch) > 0 {
				data, err := marshalMessages(batch)
				if err != nil {
					return err
				}
				data = append(data, '\n')
				_, err = t.rwc.Write(data)
				return err
			}
			return nil
		}
	} else if len(t.outgoingBatch) < cap(t.outgoingBatch) {
		t.outgoingBatch = append(t.outgoingBatch, msg)
		if len(t.outgoingBatch) == cap(t.outgoingBatch) {
			data, err := marshalMessages(t.outgoingBatch)
			t.outgoingBatch = t.outgoingBatch[:0]
			if err != nil {
				return err
			}
			data = append(data, '\n')
			_, err = t.rwc.Write(data)
			return err
		}
		return nil
	}
	data, err := jsonrpc2.EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %v", err)
	}
	data = append(data, '\n') // newline delimited
	_, err = t.rwc.Write(data)
	return err
}

func (t *ioConn) Close() error {
	t.closeOnce.Do(func() {
		t.closeErr = t.rwc.Close()
		close(t.closed)
	})
	return t.closeErr
}

func marshalMessages[T jsonrpc.Message](msgs []T) ([]byte, error) {
	var rawMsgs []json.RawMessage
	for _, msg := range msgs {
		raw, err := jsonrpc2.EncodeMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("encoding batch message: %w", err)
		}
		rawMsgs = append(rawMsgs, raw)
	}
	return json.Marshal(rawMsgs)
}
