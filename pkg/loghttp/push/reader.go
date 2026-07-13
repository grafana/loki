package push

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
)

// allocBlockSize is the granularity at which sharedLimitReader reserves bytes
// from the shared budget. Reserving in fixed blocks (rather than per-Read)
// keeps contention on the shared atomic low.
const allocBlockSize = 8 << 10 // 8KB

var (
	// inflightBytesLimit is the configured process-wide inflight-bytes limit. A
	// value > 0 means the limit is enabled; the parsers wrap the request body
	// whenever it is enabled, independent of how much budget currently remains.
	inflightBytesLimit atomic.Int64

	// inflightBytesBudget is the remaining inflight-bytes budget shared by every
	// request being parsed. It counts down from inflightBytesLimit as bytes are
	// reserved and back up as requests finish. It can legitimately sit at 0 (or
	// briefly below, transiently, during a failed reservation) while the limit
	// is still enabled.
	inflightBytesBudget atomic.Int64
)

// SetMaxInflightBytes seeds the process-wide inflight-bytes limit and budget. 0
// (or a negative value) disables the limit, in which case the parsers do not
// wrap the request body at all.
func SetMaxInflightBytes(n int64) {
	inflightBytesLimit.Store(n)
	inflightBytesBudget.Store(n)
}

// inflightLimitEnabled reports whether the process-wide inflight-bytes limit is
// configured. It is deliberately independent of the remaining budget so that
// requests keep being limited even once the budget is fully consumed.
func inflightLimitEnabled() bool {
	return inflightBytesLimit.Load() > 0
}

// observeInflightBytesUsage records the current amount of the process-wide
// budget that is reserved (limit minus remaining) on the high-watermark summary.
func observeInflightBytesUsage() {
	used := inflightBytesLimit.Load() - inflightBytesBudget.Load()
	if used < 0 {
		used = 0
	}
	inflightBytesHighWatermark.Observe(float64(used))
}

// sharedLimitReader is a block-based allocator over a shared budget. As bytes
// are read it reserves budget from the shared atomic in allocBlockSize
// increments, and it caps reads to what it has actually reserved so the shared
// budget can never be driven below zero. When the budget is exhausted Read
// returns io.EOF (terminating parsing) and records that truncation occurred.
//
// The whole reservation is returned to the budget on Close, so callers must
// always Close the reader once parsing is done.
//
// A sharedLimitReader is used by a single request/goroutine; only the shared
// *atomic.Int64 budget is concurrent, so the per-reader fields need no locking.
type sharedLimitReader struct {
	reader    io.Reader
	limit     *atomic.Int64
	reserved  int64 // bytes reserved from the shared budget, not yet returned
	consumed  int64 // bytes actually read so far
	truncated bool  // true if a read was cut short because the budget was exhausted
}

func NewSharedLimitReader(reader io.Reader, limit *atomic.Int64) *sharedLimitReader {
	return &sharedLimitReader{
		reader: reader,
		limit:  limit,
	}
}

func (r *sharedLimitReader) Read(p []byte) (n int, err error) {
	// Reserve enough blocks to cover this read on top of what we have already
	// consumed. Reservation is done in fixed blocks to keep atomic contention low.
	for r.reserved < r.consumed+int64(len(p)) {
		if !r.grab(allocBlockSize) {
			break
		}
	}

	avail := r.reserved - r.consumed
	if avail <= 0 {
		// Budget exhausted: terminate parsing by signalling EOF and record that
		// the body was truncated.
		r.truncated = true
		return 0, io.EOF
	}

	// Never read more than we have reserved.
	if int64(len(p)) > avail {
		p = p[:avail]
	}

	n, err = r.reader.Read(p)
	r.consumed += int64(n)
	return n, err
}

// grab attempts to reserve n bytes from the shared budget. It returns false if
// the budget cannot satisfy the request, restoring any speculative decrement.
func (r *sharedLimitReader) grab(n int64) bool {
	if r.limit.Add(-n) < 0 {
		r.limit.Add(n)
		return false
	}
	r.reserved += n
	return true
}

// Truncated reports whether a read was cut short because the shared budget was
// exhausted.
func (r *sharedLimitReader) Truncated() bool {
	return r.truncated
}

// Close returns the reader's entire reservation to the shared budget.
func (r *sharedLimitReader) Close() error {
	r.limit.Add(r.reserved)
	r.reserved = 0
	return nil
}

type inflightReleaserKey struct{}

// InflightReleaser tracks the inflight-byte reservations made while parsing a
// single request so they can be returned to the shared budget once the response
// has been sent, rather than as soon as parsing finishes. This keeps the bytes
// "allocated" for the whole lifetime of the request (parse, replication to
// ingesters and response), which is what the process-wide inflight limit is
// meant to bound.
type InflightReleaser struct {
	mu      sync.Mutex
	closers []io.Closer
}

// track registers a reservation (a sharedLimitReader) to be released later.
func (ir *InflightReleaser) track(c io.Closer) {
	ir.mu.Lock()
	ir.closers = append(ir.closers, c)
	ir.mu.Unlock()
}

// Release returns all tracked inflight-byte reservations to the shared budget.
// It is safe to call more than once.
func (ir *InflightReleaser) Release() {
	ir.mu.Lock()
	closers := ir.closers
	ir.closers = nil
	ir.mu.Unlock()
	for _, c := range closers {
		_ = c.Close()
	}
}

// WithInflightReleaser returns a context carrying an InflightReleaser that the
// request parsers use to register inflight-byte reservations. The caller (the
// HTTP push handler) must call Release on the returned releaser once the
// response has been sent so the reserved bytes are returned to the shared
// budget.
func WithInflightReleaser(ctx context.Context) (context.Context, *InflightReleaser) {
	ir := &InflightReleaser{}
	return context.WithValue(ctx, inflightReleaserKey{}, ir), ir
}

func inflightReleaserFromContext(ctx context.Context) *InflightReleaser {
	ir, _ := ctx.Value(inflightReleaserKey{}).(*InflightReleaser)
	return ir
}
