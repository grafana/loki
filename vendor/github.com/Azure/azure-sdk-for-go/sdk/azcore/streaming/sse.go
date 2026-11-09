// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package streaming

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/log"
	"github.com/Azure/azure-sdk-for-go/sdk/internal/errorinfo"
)

// SSEFrame is a single Server-Sent Event frame parsed from a text/event-stream
// body. It exposes only the wire-level SSE envelope; the strongly-typed payload
// is produced by a generated per-stream decoder that consumes a SSEFrame.
type SSEFrame struct {
	// Type is the value of the SSE "event" field. An empty value means the
	// default "message" event type.
	Type string

	// Data is the concatenated "data" field(s) for the event with the single
	// trailing newline removed. For JSON events this is the raw JSON document;
	// for @data/text events it is the raw text payload.
	Data []byte

	// ID is the explicitly supplied SSE "id" field, or nil when absent.
	// An empty value resets the reader's resumption cursor. Use LastEventID on
	// the reader to get the current cursor, including inherited IDs.
	ID *string

	// Retry is the reconnection delay in milliseconds from the SSE "retry"
	// field, or nil when absent. Zero requests immediate reconnection.
	Retry *int
}

// SSEReader provides typed, forward-only iteration over a Server-Sent Events
// response body. T is the generated event union for the operation.
//
// The stream is bound to the operation context passed to NewSSEReader.
// Call Close to release the stream early. Close may be called concurrently with Next; other methods
// that advance the stream must not be called concurrently.
//
// The zero value is a valid, already-exhausted stream: iteration yields no
// events and Close is a no-op.
type SSEReader[T any] struct {
	mu        sync.Mutex // protects body, done, and lastID from concurrent Close
	body      io.ReadCloser
	scanner   *sseScanner
	decode    func(SSEFrame) (T, bool, error)
	connect   SSEConnector
	ctx       context.Context
	cancel    context.CancelFunc
	reconnect bool
	done      bool
	lastID    string
	retry     int
	delay     time.Duration
}

// SSEConnector opens the initial SSE connection and reopens it when
// reconnection is enabled. lastEventID is the initial checkpoint or the latest
// committed event ID, or an empty string when no checkpoint is available.
//
// The connector must honor ctx, validate the response status and content type,
// and use the client's pipeline for request retries. It returns the response
// body on success, or io.EOF when the service indicates completion (e.g. HTTP 204).
// The reader does not retry failed connector calls and closes any body returned
// with an error. All returned bodies become the reader's responsibility, even
// if construction fails. Bodies must support concurrent Read and Close, with
// Close unblocking a pending Read, as an HTTP response body does.
type SSEConnector func(ctx context.Context, lastEventID string) (io.ReadCloser, error)

// SSEHandler supplies the typed decoder and reconnection behavior.
type SSEHandler[T any] struct {
	// Decode maps a wire-level SSEFrame to the typed union value; it returns
	// terminal=true to deliver value as the final event. Subsequent reads return
	// io.EOF without reconnecting. To stop without delivering a value (for example,
	// for a literal sentinel such as "[DONE]"), return io.EOF as err instead.
	Decode func(frame SSEFrame) (value T, terminal bool, err error)

	// Reconnect enables a continuous stream to reconnect after EOF or a
	// recoverable transport read error. It does not require an event ID or a
	// previously delivered event. Enable it only when reopening the operation
	// is supported by the service. It must remain false for finite operations
	// that signal completion with EOF. The default is false.
	Reconnect bool
}

// SSEReaderOptions contains the optional values when constructing an SSEReader.
type SSEReaderOptions struct {
	// LastEventID seeds the reader's resumption cursor and is passed to the
	// connector when opening the initial connection. The default empty string
	// means no checkpoint; the service determines the starting position.
	// It does not necessarily mean the beginning of the event history.
	LastEventID string

	// ReconnectDelay is the delay before reopening a disconnected stream when
	// the server has not supplied a valid SSE retry field. The default is three
	// seconds. A server-supplied retry field takes precedence, including zero.
	// It remains in effect across connections until another valid retry field
	// replaces it.
	// Negative values are invalid. Pipeline request retry delays are separate.
	ReconnectDelay time.Duration
}

// NewSSEReader opens an SSE connection through connect and returns a typed
// reader. It validates the configuration before invoking connect and reports
// initial connection errors before returning. If connect returns io.EOF, it
// returns a valid, exhausted reader.
//
// ctx must be the original operation context, not an individual pipeline attempt
// context. The reader passes its own cancellable child context to connect for
// every connection. ctx does not itself interrupt a pending Read on an arbitrary
// body; the body must honor cancellation to provide that behavior.
func NewSSEReader[T any](ctx context.Context, connect SSEConnector, handler SSEHandler[T], options *SSEReaderOptions) (*SSEReader[T], error) {
	if ctx == nil {
		return nil, errors.New("streaming: context must not be nil")
	}
	if connect == nil {
		return nil, errors.New("streaming: Connect must not be nil")
	}
	if handler.Decode == nil {
		return nil, errors.New("streaming: Decode must not be nil")
	}
	lastID := ""
	delay := 3 * time.Second
	if options != nil {
		lastID = options.LastEventID
		if options.ReconnectDelay < 0 {
			return nil, errors.New("streaming: ReconnectDelay must not be negative")
		}
		if options.ReconnectDelay > 0 {
			delay = options.ReconnectDelay
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	reader := &SSEReader[T]{
		decode:    handler.Decode,
		connect:   connect,
		ctx:       ctx,
		cancel:    cancel,
		reconnect: handler.Reconnect,
		retry:     -1,
		lastID:    lastID,
		delay:     delay,
	}
	if err := reader.open(); err != nil {
		reader.stop()
		return nil, err
	}
	return reader, nil
}

func newSSEScanner(body io.ReadCloser, lastID string) *sseScanner {
	sc := bufio.NewScanner(body)
	sse := &sseScanner{scanner: sc, lastID: lastID, retry: -1, firstLine: true}
	sc.Split(sse.scanSSELines)
	// SSE payloads can carry large JSON documents; grow the buffer accordingly.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return sse
}

// Next returns the next typed event in the stream. It returns io.EOF when the
// stream is complete. A decoded terminal event is returned once before io.EOF;
// a decoder io.EOF ends the stream without delivering a value. When the
// SSEReader was created with reconnect support, EOF or a recoverable transport
// read error triggers a reconnect, honoring the reconnection delay and stream
// context. Failed connection attempts and decoding errors end iteration.
//
// Next blocks while waiting for a complete data event. An open stream can block
// indefinitely if no event arrives, even if comments or metadata continue to
// arrive. There is no built-in idle timeout. Call Close to interrupt the wait.
// For HTTP response bodies opened with the original operation context, canceling
// that context or reaching its deadline also interrupts a pending read.
func (e *SSEReader[T]) Next() (T, error) {
	var zero T
	// a nil scanner is an empty stream: nothing to consume.
	for {
		if err := e.nextError(); err != nil {
			return zero, err
		}
		frame, err := e.scanner.next()
		e.mu.Lock()
		e.lastID = e.scanner.lastID
		e.mu.Unlock()
		if e.scanner.retry >= 0 {
			e.retry = e.scanner.retry
		}
		if err != nil {
			if stateErr := e.nextError(); stateErr != nil {
				return zero, stateErr
			}
			if e.reconnect && isSSEReconnectError(err) {
				if rerr := e.reopen(); rerr != nil {
					e.stop()
					return zero, rerr
				}
				continue
			}
			e.stop()
			return zero, err
		}
		if err := e.nextError(); err != nil {
			return zero, err
		}
		if frame.Data == nil {
			continue
		}
		value, terminal, derr := e.decode(frame)
		if derr != nil {
			e.stop()
			return zero, derr
		}
		if err := e.nextError(); err != nil {
			return zero, err
		}
		if terminal {
			e.stop()
		}
		return value, nil
	}
}

func (e *SSEReader[T]) nextError() error {
	if e.scanner == nil {
		return io.EOF
	}
	return e.stateError()
}

func (e *SSEReader[T]) stateError() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.done {
		return io.EOF
	}
	if err := e.ctx.Err(); err != nil {
		e.done = true
		e.cancel()
		return err
	}
	return nil
}

func (e *SSEReader[T]) stop() {
	e.mu.Lock()
	e.done = true
	e.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
	}
}

func isSSEReconnectError(err error) bool {
	var nonRetriable errorinfo.NonRetriable
	if errors.As(err, &nonRetriable) {
		return false
	}
	var netErr net.Error
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &netErr)
}

// reopen waits the server-suggested retry delay, then reconnects via the
// factory and swaps in the new segment's body, forwarding the last event id.
// It is bound by the stream's context.
func (e *SSEReader[T]) reopen() error {
	e.mu.Lock()
	body := e.body
	e.body = nil
	e.mu.Unlock()
	if body != nil {
		if err := body.Close(); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}
	delay := e.delay
	if e.retry >= 0 {
		// An SSE retry value can exceed the range of time.Duration.
		const maxDelay = time.Duration(1<<63 - 1)
		if int64(e.retry) > int64(maxDelay/time.Millisecond) {
			delay = maxDelay
		} else {
			delay = time.Duration(e.retry) * time.Millisecond
		}
	}
	log.Writef(log.EventRetryPolicy, "SSE stream reconnecting after %s", delay)
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-e.ctx.Done():
			return e.nextError()
		case <-timer.C:
		}
	}
	return e.open()
}

func (e *SSEReader[T]) open() error {
	if err := e.stateError(); err != nil {
		return err
	}
	nextBody, err := e.connect(e.ctx, e.LastEventID())
	if err != nil {
		complete := errors.Is(err, io.EOF)
		if nextBody != nil {
			if closeErr := nextBody.Close(); closeErr != nil {
				if complete {
					return closeErr
				}
				// A cleanup EOF must not make a connection failure look like completion.
				if !errors.Is(closeErr, io.EOF) {
					err = errors.Join(err, closeErr)
				}
			}
		}
		if stateErr := e.stateError(); stateErr != nil {
			return stateErr
		}
		if complete {
			e.stop()
			return nil
		}
		return err
	}
	if nextBody == nil {
		return errors.New("streaming: Connect returned a nil body")
	}
	e.mu.Lock()
	e.body = nextBody
	if !e.done {
		e.scanner = newSSEScanner(nextBody, e.lastID)
	}
	e.mu.Unlock()
	if stateErr := e.stateError(); stateErr != nil {
		closeErr := e.Close()
		if closeErr != nil && !errors.Is(closeErr, io.EOF) {
			return closeErr
		}
		return stateErr
	}
	return nil
}

// LastEventID returns the current resumption cursor, initially seeded by
// SSEReaderOptions.LastEventID and updated by complete SSE blocks with an id
// field. This value is passed to the connector when reopening the stream.
func (e *SSEReader[T]) LastEventID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastID
}

// Close stops iteration and reconnection, cancels an in-progress reconnect, and
// closes the underlying response body. Subsequent calls to Next return io.EOF.
// Calling Close more than once is safe.
func (e *SSEReader[T]) Close() error {
	e.mu.Lock()
	e.done = true
	body := e.body
	e.body = nil
	e.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
	}
	if body == nil {
		return nil
	}
	return body.Close()
}

// sseScanner also returns metadata-only blocks, which update reader state without
// being passed to the event decoder.
type sseScanner struct {
	scanner   *bufio.Scanner
	lastID    string
	retry     int
	firstLine bool
	skipLF    bool
}

func (e *sseScanner) next() (SSEFrame, error) {
	var (
		frame    SSEFrame
		dataBuf  bytes.Buffer
		haveData bool
	)
	for e.scanner.Scan() {
		line := e.scanner.Text()
		if e.firstLine {
			line = strings.TrimPrefix(line, "\uFEFF")
			e.firstLine = false
		}
		if line == "" {
			if frame.ID != nil {
				e.lastID = *frame.ID
			}
			return finishFrame(&frame, &dataBuf, haveData), nil
		}
		if strings.HasPrefix(line, ":") {
			continue // comment line
		}
		field, value := splitSSEField(line)
		switch field {
		case "event":
			frame.Type = value
		case "data":
			dataBuf.WriteString(value)
			dataBuf.WriteByte('\n')
			haveData = true
		case "id":
			if !strings.ContainsRune(value, 0) { // ignore ids containing U+0000 NULL
				frame.ID = &value
			}
		case "retry":
			if isASCIIDigits(value) {
				if n, err := strconv.Atoi(value); err == nil {
					frame.Retry = &n
					e.retry = n
				}
			}
		}
	}
	if err := e.scanner.Err(); err != nil {
		return SSEFrame{}, err
	}
	return SSEFrame{}, io.EOF
}

func finishFrame(frame *SSEFrame, dataBuf *bytes.Buffer, haveData bool) SSEFrame {
	if haveData {
		d := dataBuf.Bytes()
		if n := len(d); n > 0 && d[n-1] == '\n' {
			d = d[:n-1] // strip the single trailing newline added during accumulation
		}
		frame.Data = make([]byte, len(d))
		copy(frame.Data, d)
	}
	return *frame
}

func splitSSEField(line string) (field, value string) {
	if i := strings.IndexByte(line, ':'); i >= 0 {
		field = line[:i]
		value = strings.TrimPrefix(line[i+1:], " ")
		return field, value
	}
	return line, ""
}

func isASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// scanSSELines is a bufio.SplitFunc that splits on the SSE line terminators
// CRLF, LF, and a lone CR, none of which are included in the returned token.
func (e *sseScanner) scanSSELines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	start := 0
	if e.skipLF && len(data) > 0 {
		e.skipLF = false
		if data[0] == '\n' {
			start = 1
		}
	}
	for i := start; i < len(data); i++ {
		switch data[i] {
		case '\n':
			return i + 1, data[start:i], nil
		case '\r':
			// CR ends the line immediately; swallow a following LF on the next call.
			e.skipLF = true
			return i + 1, data[start:i], nil
		}
	}
	if atEOF && start < len(data) {
		return len(data), data[start:], nil
	}
	return start, nil, nil // request more data
}
