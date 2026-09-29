package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/spiridonov/deadhorse"
)

// ErrInvalidKey is wrapped into a RequestEntry's ResponseEntry.Err when its
// key can't be sent over DHP/1 at all (empty, or containing whitespace/'|').
// It's a caller bug, not an operational failure, so it's always reported as
// throttled regardless of the client's fail-open/closed setting.
var ErrInvalidKey = errors.New("deadhorse: invalid key")

// ErrEntryRejected is wrapped into a ResponseEntry.Err when the server sent
// back the bare ERR token for that entry -- it rejected the entry outright
// (a bad field on the wire), so there's no computed decision to trust.
var ErrEntryRejected = errors.New("deadhorse: entry rejected by server")

// ErrCircuitOpen is wrapped into a ResponseEntry.Err when a shard's circuit
// breaker is open: the call was failed immediately, without any network
// activity, because that shard has recently shown enough consecutive
// failures that another attempt right now is far more likely to waste a
// full timeout than to get a real answer. See circuitBreaker.
var ErrCircuitOpen = errors.New("deadhorse: shard circuit open, failing fast without contacting it")

// maxInFlight bounds how many THROTTLE calls a single shardConn will pipeline
// onto its connection at once. A call beyond this simply waits its turn to
// submit -- backpressure against a shard that's accepting writes faster than
// it's answering them, rather than an unbounded, memory-growing queue.
const maxInFlight = 256

// idleReadTimeout bounds how long shardConn's reader will wait for a response
// with nothing at all coming back, so a shard that accepted a connection but
// then went silent forever eventually gets noticed and reconnected instead of
// wedging the pipeline for good. It's deliberately generous and unrelated to
// any per-call timeout: normal gaps between bursts of traffic are expected
// and shouldn't cost a reconnect.
const idleReadTimeout = 30 * time.Second

// shardConn is one persistent, pipelined connection to one shard, reconnected
// lazily on the next call after any error. Multiple Throttle calls may be in
// flight on it concurrently: each submits its request and waits on its own
// channel rather than holding the connection for the whole round trip, so one
// slow call never blocks another's request from going out. This relies on
// the server answering one connection's requests strictly in the order they
// arrived (see TextServer.handleConn), which is what lets responses be
// matched back to calls by plain FIFO order instead of a request ID.
type shardConn struct {
	addr string

	// breaker fails calls to this shard fast, without any network activity,
	// once it's shown enough consecutive shard-attributable failures in a
	// row -- see circuitBreaker's doc comment. It has its own internal
	// synchronization (atomics, not mu) and is read/updated from throttle
	// and exchange without ever touching mu.
	breaker *circuitBreaker

	// mu guards conn, gen, and dialing, and serializes submit's
	// write+enqueue pair (see submit) so that wire order and queue order
	// never diverge. It is deliberately never held across a blocking network
	// call -- neither a read nor, importantly, the dial itself (see
	// ensureConn), nor waiting for room in a full pending queue (see
	// generation): a shard that's slow to connect, or already at
	// maxInFlight, must only stall calls actually waiting on that, not
	// every other call to this shardConn, and must still respect each
	// caller's own ctx/timeout rather than blocking indefinitely.
	mu   sync.Mutex
	conn net.Conn
	gen  *generation

	// dialing is non-nil while one goroutine is in the middle of dialing a
	// fresh connection, and is closed (by that goroutine) once the attempt
	// finishes, successfully or not. Any other goroutine that finds dialing
	// already in progress waits on it instead of piling up behind mu or
	// racing to dial a second, redundant connection.
	dialing chan struct{}

	// closed is set by close() and checked first thing in ensureConn, so
	// that once closed, close is a definitive, permanent shutdown -- like
	// sql.DB or net.Listener -- rather than something a later Throttle call
	// silently undoes by dialing a fresh connection. It also closes a
	// narrower race: without it, a dial already in flight at the time of
	// the call could install its connection after close() returns, since
	// mu is released across the dial itself (see ensureConn), leaking
	// exactly the socket and reader goroutine close() was meant to tear
	// down.
	closed bool
}

// generation groups one physical connection's pending-call queue with the
// semaphore that admits calls into it, so a submit blocked waiting for room
// can wait on sem alone -- without holding shardConn's mu -- while still
// being guaranteed that, once it holds a token, enqueuing into pending will
// never itself block: sem starts pre-loaded with exactly maxInFlight
// tokens, and a token only ever comes back once its call has actually left
// pending (see readLoop and drainPending), so #tokens-held can never exceed
// pending's remaining capacity.
type generation struct {
	pending chan *pendingCall
	sem     chan struct{}
}

func newGeneration() *generation {
	sem := make(chan struct{}, maxInFlight)
	for i := 0; i < maxInFlight; i++ {
		sem <- struct{}{}
	}
	return &generation{pending: make(chan *pendingCall, maxInFlight), sem: sem}
}

func (g *generation) release() {
	g.sem <- struct{}{}
}

// pendingCall is one THROTTLE request waiting for its response. resultCh is
// buffered so the reader loop's delivery never blocks on a caller that has
// already given up (its ctx expired) and stopped listening.
type pendingCall struct {
	entries  []deadhorse.RequestEntry
	resultCh chan pendingResult
}

type pendingResult struct {
	results []deadhorse.ResponseEntry
	err     error
}

// throttle validates keys locally first, so one malformed key doesn't spoil
// the rest of the batch, sends the valid entries over the wire, and always
// returns a fully populated, same-length result slice. The returned error,
// if any, is a join of every distinct problem (validation and/or network)
// encountered -- for the network case specifically, failOpen decides what
// Throttled becomes for the entries that never got a real answer.
func (c *shardConn) throttle(ctx context.Context, entries []deadhorse.RequestEntry, timeout time.Duration, failOpen bool) ([]deadhorse.ResponseEntry, error) {
	results := make([]deadhorse.ResponseEntry, len(entries))
	validEntries := make([]deadhorse.RequestEntry, 0, len(entries))
	validIdx := make([]int, 0, len(entries))
	var errs []error

	for i, e := range entries {
		if err := validateKey(e.Key); err != nil {
			results[i] = deadhorse.ResponseEntry{Key: e.Key, Throttled: true, Err: err}
			errs = append(errs, err)
			continue
		}
		validEntries = append(validEntries, e)
		validIdx = append(validIdx, i)
	}
	if len(validEntries) == 0 {
		return results, errors.Join(errs...)
	}

	if !c.breaker.allow() {
		err := fmt.Errorf("deadhorse: shard %s: %w", c.addr, ErrCircuitOpen)
		for _, idx := range validIdx {
			results[idx] = deadhorse.ResponseEntry{Key: entries[idx].Key, Throttled: !failOpen, Err: err}
		}
		return results, errors.Join(append(errs, err)...)
	}

	shardFault, err := c.exchange(ctx, validEntries, timeout, results, validIdx)
	switch {
	case err != nil && shardFault:
		c.breaker.recordFailure()
	case err == nil:
		c.breaker.recordSuccess()
	}
	if err != nil {
		for _, idx := range validIdx {
			results[idx] = deadhorse.ResponseEntry{Key: entries[idx].Key, Throttled: !failOpen, Err: err}
		}
		errs = append(errs, err)
	}
	return results, errors.Join(errs...)
}

// exchange submits a batch of already-validated entries and waits for its
// own response, without ever holding the connection for the duration of the
// wait -- see submit and shardConn's pipelining doc comment. It returns only
// a network/protocol-level error -- never a per-entry one, since a per-entry
// ERR is not an exchange failure (see decodeResult).
//
// The shardFault return says whether a non-nil err reflects something
// actually wrong with this shard (a dial/write/read failure, a desynced
// connection, or this client's own configured timeout expiring before the
// shard answered) as opposed to the caller's own ctx ending first (its
// deadline passing, or an explicit cancel) -- which says nothing about the
// shard's health and must never be allowed to trip its circuit breaker: a
// caller with a tighter budget than this client's configured timeout must
// not be able to fail every other caller's calls to this shard too, just by
// giving up early itself. The two are told apart by checking ctx -- the
// caller's own context, not the deadline-bound waitCtx derived from it below
// -- immediately after any failure: if ctx is already done, the caller is
// why this call ended; if it isn't, this call's own end (a real network
// failure, or this client's own timeout) is what happened, which is exactly
// the shard-attributable case. That check is only reliable because of how
// waitCtx itself is constructed below: when ctx's own deadline already
// binds at least as tightly as timeout would, waitCtx is ctx itself, not a
// second context.WithDeadline wrapped around it for the very same instant
// -- two independent timers racing to fire at the same instant would leave
// which one's Err() is visible first up to chance, undermining the whole
// distinction.
func (c *shardConn) exchange(ctx context.Context, validEntries []deadhorse.RequestEntry, timeout time.Duration, results []deadhorse.ResponseEntry, validIdx []int) (shardFault bool, err error) {
	deadline := time.Now().Add(timeout)
	waitCtx := ctx
	if d, ok := ctx.Deadline(); !ok || d.After(deadline) {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}

	call := &pendingCall{entries: validEntries, resultCh: make(chan pendingResult, 1)}
	req := encodeThrottle(validEntries)

	if err := c.submit(waitCtx, call, req); err != nil {
		return ctx.Err() == nil, err
	}

	select {
	case res := <-call.resultCh:
		if res.err != nil {
			return true, res.err
		}
		for j, idx := range validIdx {
			results[idx] = res.results[j]
		}
		return false, nil
	case <-waitCtx.Done():
		// call's response, if the shard eventually sends one, is still read
		// by the connection's reader loop and simply dropped into resultCh's
		// one-slot buffer unread -- the connection itself is left alone for
		// every other call still pipelined on it.
		return ctx.Err() == nil, waitCtx.Err()
	}
}

// submit enqueues call and writes req to the wire as a single unit under mu,
// so the order calls are queued in always matches the order their requests
// hit the wire -- required for the reader loop's FIFO response matching to
// stay correct. ctx bounds how long submit will wait for room in a full
// pending queue and, via ensureConn, how long it will wait to connect; it
// does not bound the write itself, which gets its own deadline from ctx
// below.
func (c *shardConn) submit(ctx context.Context, call *pendingCall, req string) error {
	for {
		conn, gen, err := c.ensureConn(ctx)
		if err != nil {
			return err
		}

		// Wait for room in gen's pending queue outside mu: a shard already
		// at maxInFlight must only stall calls actually waiting for a slot,
		// not every other call to this shard (see maxInFlight's doc
		// comment). Once acquired, this token guarantees the send into
		// gen.pending below can never itself block.
		select {
		case <-gen.sem:
		case <-ctx.Done():
			return ctx.Err()
		}

		c.mu.Lock()
		if c.conn != conn {
			// A concurrent failure (or reconnect) raced us between ensureConn
			// returning and us taking mu; this generation is no longer
			// current, so give back the token we're holding for it (it's
			// ours alone -- nothing else will ever use gen again) and retry
			// against whatever the shard's connection is now.
			c.mu.Unlock()
			gen.release()
			continue
		}

		gen.pending <- call

		if d, ok := ctx.Deadline(); ok {
			conn.SetWriteDeadline(d)
		}
		_, err = conn.Write([]byte(req))
		c.mu.Unlock()
		if err != nil {
			c.abort(conn, gen, err)
			return err
		}
		return nil
	}
}

// ensureConn returns the shard's current connection and generation,
// dialing a fresh one if none is established. Unlike dialing under mu, at
// most one goroutine ever has a real net.Dialer.DialContext call in flight
// for this shardConn at a time -- others racing to connect the same shard
// wait on that attempt (bounded by their own ctx) instead of blocking every
// other call to this shard for however long the OS-level connect takes, or
// each independently dialing a redundant connection.
func (c *shardConn) ensureConn(ctx context.Context) (net.Conn, *generation, error) {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, nil, net.ErrClosed
		}
		if c.conn != nil {
			conn, gen := c.conn, c.gen
			c.mu.Unlock()
			return conn, gen, nil
		}
		if dialing := c.dialing; dialing != nil {
			c.mu.Unlock()
			select {
			case <-dialing:
				continue // re-check c.conn now that the other dial finished
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}

		dialing := make(chan struct{})
		c.dialing = dialing
		c.mu.Unlock()

		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", c.addr)

		c.mu.Lock()
		c.dialing = nil
		switch {
		case err != nil:
			// Nothing dialed; nothing to install.
		case c.closed:
			// close() ran while the dial above was in flight -- leave the
			// shardConn's state alone and close the connection we just
			// opened instead of resurrecting it after Close().
			err = net.ErrClosed
		default:
			gen := newGeneration()
			c.conn = conn
			c.gen = gen
			go c.readLoop(conn, gen)
		}
		resultConn, resultGen := c.conn, c.gen
		close(dialing)
		c.mu.Unlock()

		if err != nil {
			if conn != nil {
				conn.Close()
			}
			return nil, nil, err
		}
		return resultConn, resultGen, nil
	}
}

// readLoop owns conn's read side for its entire lifetime: it reads one
// response line at a time and delivers each to the oldest still-pending
// call, relying on the server never answering a connection's requests out of
// order. Any read or decode error desyncs that ordering for good, so it
// takes the whole connection down with it via abort rather than trying to
// recover.
func (c *shardConn) readLoop(conn net.Conn, gen *generation) {
	r := bufio.NewReader(conn)
	for {
		conn.SetReadDeadline(time.Now().Add(idleReadTimeout))
		line, err := r.ReadString('\n')
		if err != nil {
			c.abort(conn, gen, err)
			return
		}

		var call *pendingCall
		select {
		case call = <-gen.pending:
			gen.release()
		default:
			c.abort(conn, gen, fmt.Errorf("deadhorse: response with nothing pending: %q", strings.TrimRight(line, "\r\n")))
			return
		}

		results, err := decodeResult(line, call.entries)
		call.resultCh <- pendingResult{results: results, err: err}
		if err != nil {
			c.abort(conn, gen, err)
			return
		}
	}
}

// abort tears down conn and fails every call still waiting in gen.pending
// with err. It's called from the reader loop itself (outside mu, since it
// must never block a concurrent submit on a network read), and only clears
// shardConn's own conn/gen fields if they still refer to this exact
// generation -- a concurrent submit may already have reconnected.
func (c *shardConn) abort(conn net.Conn, gen *generation, err error) {
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
		c.gen = nil
	}
	c.mu.Unlock()

	conn.Close()
	drainPending(gen, err)
}

func drainPending(gen *generation, err error) {
	for {
		select {
		case call := <-gen.pending:
			gen.release()
			call.resultCh <- pendingResult{err: err}
		default:
			return
		}
	}
}

func (c *shardConn) close() {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.gen = nil
	c.closed = true
	c.mu.Unlock()

	if conn != nil {
		conn.Close()
	}
}

func validateKey(key string) error {
	if key == "" || strings.ContainsFunc(key, isKeyDelimiter) {
		return fmt.Errorf("deadhorse: key %q is empty or contains whitespace/'|', which DHP/1 forbids: %w", key, ErrInvalidKey)
	}
	return nil
}

// isKeyDelimiter reports whether r is a rune the wire protocol's own
// tokenizers -- strings.Fields, used by both the server (textserver.go's
// dispatch/handleThrottle) and this package's own response parser
// (decodeResult) -- treat as a delimiter: any Unicode whitespace, or '|'.
// validateKey must reject exactly this set, not a narrower ASCII-only
// blacklist: a key containing e.g. '\v' would otherwise pass validation
// here only to get split into two wire tokens later, desyncing the whole
// batch (see decodeResult's entry-count check).
func isKeyDelimiter(r rune) bool {
	return unicode.IsSpace(r) || r == '|'
}

// encodeThrottle assumes every entry has already passed validateKey.
func encodeThrottle(entries []deadhorse.RequestEntry) string {
	parts := make([]string, len(entries))
	for i, e := range entries {
		mode := "R"
		if e.Peek {
			mode = "P"
		}
		parts[i] = fmt.Sprintf("%s|%d|%d|%d|%s", e.Key, e.Limit.Capacity, int64(e.Limit.EmissionInterval), deadhorse.EffectiveCost(e.Cost), mode)
	}
	if len(parts) == 0 {
		return "THROTTLE\n"
	}
	return "THROTTLE " + strings.Join(parts, " ") + "\n"
}

func decodeResult(line string, entries []deadhorse.RequestEntry) ([]deadhorse.ResponseEntry, error) {
	line = strings.TrimRight(line, "\r\n")
	tokens := strings.Fields(line)
	if len(tokens) == 0 || tokens[0] != "RESULT" {
		return nil, fmt.Errorf("deadhorse: unexpected response %q", line)
	}
	resultTokens := tokens[1:]
	if len(resultTokens) != len(entries) {
		return nil, fmt.Errorf("deadhorse: response entry count mismatch: got %d, want %d in %q", len(resultTokens), len(entries), line)
	}

	results := make([]deadhorse.ResponseEntry, len(entries))
	for i, tok := range resultTokens {
		if tok == "ERR" {
			results[i] = deadhorse.ResponseEntry{Key: entries[i].Key, Throttled: true, Err: ErrEntryRejected}
			continue
		}

		fields := strings.Split(tok, "|")
		if len(fields) != 4 {
			return nil, fmt.Errorf("deadhorse: malformed result %q", tok)
		}
		remaining, err1 := strconv.ParseInt(fields[2], 10, 64)
		retryAfter, err2 := strconv.ParseInt(fields[3], 10, 64)
		if err1 != nil || err2 != nil || remaining < 0 {
			return nil, fmt.Errorf("deadhorse: malformed result %q", tok)
		}
		results[i] = deadhorse.ResponseEntry{
			Key:        fields[0],
			Throttled:  fields[1] == "1",
			Remaining:  remaining,
			RetryAfter: time.Duration(retryAfter),
		}
	}
	return results, nil
}
