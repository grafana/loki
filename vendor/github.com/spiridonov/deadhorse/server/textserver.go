package server

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
	"github.com/spiridonov/deadhorse/internal/dhp1"
)

// Throttler is what TextServer needs from whatever evaluates its entries:
// one call, one all-or-none transaction for its non-Peek entries (Peek
// entries are always independent -- see RequestEntry.Peek), full stop --
// though each entry still reports only its own check, not whether the
// transaction as a whole committed (see ResponseEntry.Throttled). How many
// internal steps that takes is entirely up to the implementation.
// InMemoryThrottler is the one built into this package; a custom
// implementation can be plugged into NewTextServer instead, for example in
// tests that want TextServer's line-parsing and dispatch behavior exercised
// without a real rate limiter behind it (see deadhorsetest).
//
// Unlike client.ShardedClient.Throttle, there is deliberately no shard key
// here: TextServer serves a single, flat keyspace with no notion of shards
// at all -- routing a key to one of several DeadHorse processes is entirely
// a client-side concern (see the client package) that's already resolved
// by the time a line reaches a TextServer.
//
// Implementations always return a result slice the same length as entries,
// in the same order, even when the returned error is non-nil -- a
// call-level error means something went wrong for some subset of entries,
// not that nothing can be reported; see ResponseEntry.Err for which ones
// and why.
type Throttler interface {
	Throttle(ctx context.Context, entries []deadhorse.RequestEntry) ([]deadhorse.ResponseEntry, error)
}

// TextServer serves DHP/1, DeadHorse's line-oriented text protocol, over any
// Throttler. Each line is one newline-terminated command; a connection may
// pipeline many requests without waiting for a response to each. TextServer
// is a single, independent process: it has no notion of shards, other
// TextServers, or a cluster -- routing a key to one of several DeadHorse
// processes is entirely a client-side concern (see the client package).
type TextServer struct {
	throttler   Throttler
	maxLineSize int
	startedAt   time.Time

	// listenerMu guards listener and closed, written by ListenAndServe and
	// Close respectively -- two methods meant to be called from different
	// goroutines (Close is how you make a blocking ListenAndServe return).
	// closed closes the TOCTOU gap between them: without it, a Close() that
	// runs before ListenAndServe finishes net.Listen and assigns listener
	// would see a nil listener, no-op, and leave ListenAndServe to bind and
	// accept forever with nothing left able to stop it.
	listenerMu sync.Mutex
	listener   net.Listener
	closed     bool
}

// DefaultMaxLineSize is what NewTextServer falls back to when given
// maxLineSize<=0. It's exported so cmd/deadhorse can use it as its own
// -max-line-size flag default, rather than duplicating this number as a
// second, driftable copy -- see cmd/deadhorse/main.go.
const DefaultMaxLineSize = 64 * 1024

const (
	protocolVersion = "1"

	// idleReadTimeout bounds how long a connection may sit with nothing
	// arriving before it's dropped, so a client that opens a connection and
	// sends nothing (or a line with no trailing '\n') can't hold a
	// goroutine, buffer, and socket open forever -- DHP/1 has no
	// authentication, so this is the server's only defense against that.
	// Deliberately generous and renewed on every line, like the client's
	// own idleReadTimeout: normal gaps between bursts of traffic are
	// expected and shouldn't cost a disconnect.
	idleReadTimeout = 30 * time.Second
)

var errLineTooLong = errors.New("line too long")

// NewTextServer builds a TextServer over throttler. maxLineSize bounds how
// long a single protocol line may be before the connection is dropped (see
// readLine); zero or negative falls back to DefaultMaxLineSize.
func NewTextServer(throttler Throttler, maxLineSize int) *TextServer {
	if maxLineSize <= 0 {
		maxLineSize = DefaultMaxLineSize
	}
	return &TextServer{
		throttler:   throttler,
		maxLineSize: maxLineSize,
		startedAt:   time.Now(),
	}
}

// ListenAndServe accepts connections on addr until the listener is closed
// (via Close), serving each on its own goroutine. It returns nil once that
// shutdown was actually requested via Close, and a non-nil error only for a
// genuine bind/accept failure -- mirroring the err != nil convention callers
// already use to decide whether something actually went wrong, rather than
// making every caller separately check errors.Is(err, net.ErrClosed) to tell
// "you asked me to stop" apart from "something broke."
func (s *TextServer) ListenAndServe(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	s.listenerMu.Lock()
	if s.closed {
		// Close() already ran, before net.Listen above even returned --
		// there's nothing left to bind and accept for, but this is the
		// shutdown that was asked for, not a failure.
		s.listenerMu.Unlock()
		lis.Close()
		return nil
	}
	s.listener = lis
	s.listenerMu.Unlock()

	for {
		conn, err := lis.Accept()
		if err != nil {
			s.listenerMu.Lock()
			closedByUs := s.closed
			s.listenerMu.Unlock()
			if closedByUs {
				return nil
			}
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *TextServer) Close() error {
	s.listenerMu.Lock()
	lis := s.listener
	s.closed = true
	s.listenerMu.Unlock()

	if lis == nil {
		return nil
	}
	return lis.Close()
}

func (s *TextServer) handleConn(conn net.Conn) {
	defer conn.Close()

	connectionsGauge.Inc()
	defer connectionsGauge.Dec()

	r := bufio.NewReaderSize(conn, 4096)
	w := bufio.NewWriter(conn)

	for {
		conn.SetReadDeadline(time.Now().Add(idleReadTimeout))
		line, err := readLine(r, s.maxLineSize)
		if errors.Is(err, errLineTooLong) {
			lineTooLongTotal.Inc()
			w.WriteString("ERROR line too long\n")
			w.Flush()
			return
		}
		if err != nil {
			return
		}
		lineLength.Observe(float64(len(line)))
		if strings.TrimSpace(line) == "" {
			continue
		}

		resp, closeConn := s.dispatch(line)
		if resp != "" {
			w.WriteString(resp)
			w.WriteByte('\n')
		}
		if err := w.Flush(); err != nil {
			return
		}
		if closeConn {
			return
		}
	}
}

// readLine reads one line terminated by '\n' (a trailing '\r' is trimmed),
// bounded to maxSize bytes total so a client that never sends '\n' can't
// grow the server's memory without bound.
func readLine(r *bufio.Reader, maxSize int) (string, error) {
	var buf []byte
	for {
		frag, err := r.ReadSlice('\n')
		buf = append(buf, frag...)
		if len(buf) > maxSize {
			return "", errLineTooLong
		}
		if err == nil {
			break
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return "", err
	}
	line := strings.TrimSuffix(string(buf), "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, nil
}

func (s *TextServer) dispatch(line string) (response string, closeConn bool) {
	cmd, rest := splitCommand(line)

	// Labeled by the normalized command, never the raw one: cmd is whatever
	// token a client sent first, and labeling with it verbatim would let a
	// client hand the metrics registry unbounded label cardinality.
	label := normalizeCommand(cmd)
	requestsTotal.WithLabelValues(label).Inc()
	start := time.Now()
	defer func() {
		requestDuration.WithLabelValues(label).Observe(time.Since(start).Seconds())
	}()

	switch cmd {
	case "HELLO":
		return s.handleHello(rest), false
	case "THROTTLE":
		return s.handleThrottle(rest), false
	case "PING":
		return "PONG", false
	case "STATS":
		return s.handleStats(), false
	case "QUIT":
		return "", true
	default:
		return "ERROR unknown command", false
	}
}

// splitCommand splits line into its first whitespace-delimited token (the
// command word) and everything after that token, using the same definition
// of "whitespace" -- any unicode.IsSpace rune, not just a literal ' ' --
// that every downstream parser already uses (handleHello's and
// handleThrottle's own strings.Fields calls, and the client's decodeResult).
// Splitting on a literal ' ' here while those split on any whitespace rune
// would make a line like "PING\t" (a tab instead of a space) fail to match
// any known command.
func splitCommand(line string) (cmd, rest string) {
	start := strings.IndexFunc(line, func(r rune) bool { return !unicode.IsSpace(r) })
	if start < 0 {
		return "", ""
	}
	line = line[start:]
	if end := strings.IndexFunc(line, unicode.IsSpace); end >= 0 {
		return line[:end], line[end:]
	}
	return line, ""
}

// normalizeCommand maps an arbitrary first token to one of the known DHP/1
// commands, or "unknown" -- see dispatch's requestsTotal/requestDuration.
func normalizeCommand(cmd string) string {
	switch cmd {
	case "HELLO", "THROTTLE", "PING", "STATS", "QUIT":
		return cmd
	default:
		return "unknown"
	}
}

func (s *TextServer) handleHello(rest string) string {
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "ERROR missing version"
	}
	if fields[0] != protocolVersion {
		return "ERROR unsupported version"
	}
	return "OK " + protocolVersion
}

func (s *TextServer) handleStats() string {
	keys := 0
	if kc, ok := s.throttler.(interface{ keyCountEstimate() int }); ok {
		keys = kc.keyCountEstimate()
	}
	uptime := int64(time.Since(s.startedAt).Seconds())
	return fmt.Sprintf("STATS %d %d", uptime, keys)
}

func (s *TextServer) handleThrottle(rest string) string {
	tokens := strings.Fields(rest)
	throttleBatchSize.Observe(float64(len(tokens)))

	entries := make([]deadhorse.RequestEntry, 0, len(tokens))
	entryOK := make([]bool, len(tokens))
	entryIdx := make([]int, 0, len(tokens))
	for i, tok := range tokens {
		e, ok := parseEntry(tok)
		entryOK[i] = ok
		if ok {
			entries = append(entries, e)
			entryIdx = append(entryIdx, i)
		} else {
			throttleEntriesTotal.WithLabelValues("unknown", "err").Inc()
		}
	}

	results := make([]string, len(tokens))
	if len(entries) > 0 {
		responses, err := s.throttler.Throttle(context.Background(), entries)
		switch {
		case err != nil, len(responses) != len(entries):
			// A throttler-level failure -- or a buggy custom Throttler
			// returning a mismatched-length slice -- must not sink the
			// whole line, only the entries actually sent to it (locally
			// malformed entries are still reported as ERR below
			// regardless). DHP/1 has no room for a message here, so ERR is
			// the same "this entry failed" signal a per-entry Err already
			// gets from formatResult.
			for _, idx := range entryIdx {
				results[idx] = "ERR"
			}
			for _, e := range entries {
				throttleEntriesTotal.WithLabelValues(modeLabel(e.Peek), "err").Inc()
			}
		default:
			for j, resp := range responses {
				results[entryIdx[j]] = formatResult(resp)
				throttleEntriesTotal.WithLabelValues(modeLabel(entries[j].Peek), resultLabel(resp)).Inc()
			}
		}
	}
	for i, ok := range entryOK {
		if !ok {
			results[i] = "ERR"
		}
	}

	if len(results) == 0 {
		return "RESULT"
	}
	return "RESULT " + strings.Join(results, " ")
}

func modeLabel(peek bool) string {
	if peek {
		return "peek"
	}
	return "real"
}

func resultLabel(r deadhorse.ResponseEntry) string {
	switch {
	case r.Err != nil:
		return "err"
	case r.Throttled:
		return "throttled"
	default:
		return "admitted"
	}
}

func parseEntry(tok string) (deadhorse.RequestEntry, bool) {
	parts := strings.Split(tok, "|")
	if len(parts) != 6 {
		return deadhorse.RequestEntry{}, false
	}
	key := parts[0]
	if key == "" {
		return deadhorse.RequestEntry{}, false
	}
	capacity, err1 := strconv.ParseInt(parts[1], 10, 64)
	units, err2 := strconv.ParseInt(parts[2], 10, 64)
	period, err3 := strconv.ParseInt(parts[3], 10, 64)
	cost, err4 := strconv.ParseInt(parts[4], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil ||
		capacity < 0 || units < 0 || period < 0 || cost < 0 {
		return deadhorse.RequestEntry{}, false
	}
	var peek bool
	switch parts[5] {
	case dhp1.ModeReal:
		peek = false
	case dhp1.ModePeek:
		peek = true
	default:
		return deadhorse.RequestEntry{}, false
	}
	return deadhorse.RequestEntry{
		Key: key,
		Limit: deadhorse.Limit{
			Capacity: capacity,
			Rate:     deadhorse.Rate{Units: units, Period: time.Duration(period)},
		},
		Cost: cost,
		Peek: peek,
	}, true
}

// formatResult collapses an errored entry to the bare ERR token, same as a
// parse failure -- the built-in InMemoryThrottler never sets Err, but
// TextServer works over any Throttler, and a custom one might.
func formatResult(r deadhorse.ResponseEntry) string {
	if r.Err != nil {
		return "ERR"
	}
	throttled := "0"
	if r.Throttled {
		throttled = "1"
	}
	return fmt.Sprintf("%s|%s|%d|%d", r.Key, throttled, r.Remaining, int64(r.RetryAfter))
}
