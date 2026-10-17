// Package readbudget bounds the compressed bytes that section reads hold in memory at once.
package readbudget

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Observer receives the wait of every reservation that a [Budget] admits or withdraws. The budget
// calls its methods without holding its lock, on the goroutine that called Reserve. An
// implementation must be safe for concurrent use.
type Observer interface {
	// Admitted reports a reservation that Reserve returned, with its bytes and the time it
	// waited.
	Admitted(bytes int64, waited time.Duration)

	// Withdrawn reports a reservation that waited and gave up because its context ended, with the
	// time it waited.
	Withdrawn(waited time.Duration)
}

// Reserver reserves the bytes that one section read is about to download.
type Reserver interface {
	// Reserve blocks until the reserver admits the bytes, and returns a lease that holds them. The
	// caller must close the lease. When ctx ends first, Reserve returns an error that matches the
	// error of ctx, and no lease. It is safe to call from many goroutines.
	Reserve(ctx context.Context, bytes int64) (*Lease, error)
}

// Budget limits the compressed bytes that section reads hold in memory at once.
//
// A section read reserves all the bytes it will download before it downloads them. It returns the
// bytes as its read passes the pages. A reservation that does not fit waits.
//
// The budget always admits a reservation when it holds nothing, whatever its size. This keeps a
// section larger than the limit from waiting for room that never exists. The budget then holds more
// than its limit until that section returns enough bytes.
//
// When bytes free up, the budget admits every waiter that fits. It tries the waiters in order of
// preference: first the objects it saw first, then the sections of one object by index. The order
// is a preference only. The budget skips a waiter that does not fit, so a large section does not
// hold back smaller ones. The cost is that a large section can run late, because smaller sections
// keep taking the bytes that free up.
//
// A Budget is safe for concurrent use. Create it with [New]. The zero value is not usable.
type Budget struct {
	limit    int64
	observer Observer

	mu      sync.Mutex
	held    int64
	nextSeq uint64
	ranks   map[string]int // The order in which the budget first saw each object.
	waiters []*waiter      // Sorted by compareWaiters.
}

// waiter is a reservation that waits for room.
type waiter struct {
	rank    int
	section int
	seq     uint64
	bytes   int64

	// lease is set, under Budget.mu, when the waiter is admitted. ready is closed at that moment.
	lease *Lease
	ready chan struct{}
}

// New returns a Budget with the given limit. The budget exceeds the limit only when it admits a
// reservation larger than the limit into an empty budget.
//
// A limit below one byte becomes one, so a caller must not pass zero to mean no limit. The
// observer may be nil.
func New(limit int64, observer Observer) *Budget {
	return &Budget{
		limit:    max(limit, 1),
		observer: observer,
		ranks:    make(map[string]int),
	}
}

// Section returns the [Reserver] for one section of the object at the path object. The object and
// the section set the order of admission and nothing else. A nil Budget returns a nil Reserver,
// which reserves nothing.
func (b *Budget) Section(object string, section int) Reserver {
	if b == nil {
		return nil
	}
	return sectionReserver{budget: b, object: object, section: section}
}

// Held returns the bytes that the open leases hold now. A nil Budget holds nothing.
func (b *Budget) Held() int64 {
	if b == nil {
		return 0
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	return b.held
}

type sectionReserver struct {
	budget  *Budget
	object  string
	section int
}

func (r sectionReserver) Reserve(ctx context.Context, bytes int64) (*Lease, error) {
	return r.budget.reserve(ctx, r.object, r.section, bytes)
}

func (b *Budget) reserve(ctx context.Context, object string, section int, bytes int64) (*Lease, error) {
	if ctx.Err() != nil {
		return nil, contextError(ctx)
	}

	w := &waiter{section: section, bytes: max(bytes, 0), ready: make(chan struct{})}
	b.enqueue(w, object)

	var waited time.Duration
	select {
	case <-w.ready:
	default:
		start := time.Now()
		select {
		case <-w.ready:
		case <-ctx.Done():
			err := b.withdraw(ctx, w)
			if b.observer != nil {
				b.observer.Withdrawn(time.Since(start))
			}
			return nil, err
		}
		waited = time.Since(start)
	}

	if b.observer != nil {
		b.observer.Admitted(w.bytes, waited)
	}
	return w.lease, nil
}

// enqueue adds the waiter in order of preference, then admits every waiter that fits.
func (b *Budget) enqueue(w *waiter, object string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	rank, ok := b.ranks[object]
	if !ok {
		rank = len(b.ranks)
		b.ranks[object] = rank
	}
	w.rank = rank
	w.seq = b.nextSeq
	b.nextSeq++

	i, _ := slices.BinarySearchFunc(b.waiters, w, compareWaiters)
	b.waiters = slices.Insert(b.waiters, i, w)
	b.admitLocked()
}

// withdraw removes a waiter whose context ended, and returns the error of the context.
func (b *Budget) withdraw(ctx context.Context, w *waiter) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if w.lease != nil {
		// The budget admitted the waiter at the same time. Nobody will use the lease, so return its bytes.
		b.releaseLocked(w.lease, w.lease.held)
	} else if i := slices.Index(b.waiters, w); i >= 0 {
		b.waiters = slices.Delete(b.waiters, i, i+1)
	}
	return contextError(ctx)
}

// contextError returns the error of a context that ended. It keeps the cause in the message, and
// it still matches [context.Canceled] or [context.DeadlineExceeded]. Callers sort a timeout from
// a cancellation by these errors, and a cause alone is often a plain error.
func contextError(ctx context.Context) error {
	err, cause := ctx.Err(), context.Cause(ctx)
	if cause == nil || cause == err {
		return err
	}
	return fmt.Errorf("%w: %w", err, cause)
}

// admitLocked admits every waiter that fits, in order of preference.
func (b *Budget) admitLocked() {
	for i := 0; i < len(b.waiters); {
		w := b.waiters[i]
		if !b.fitsLocked(w.bytes) {
			i++
			continue
		}

		b.held += w.bytes
		w.lease = &Lease{budget: b, held: w.bytes}
		close(w.ready)
		b.waiters = slices.Delete(b.waiters, i, i+1)
	}
}

// fitsLocked reports whether a reservation of the given size can be admitted now. A reservation of
// zero bytes always fits. So does any reservation into an empty budget, which is how a
// reservation larger than the limit runs.
func (b *Budget) fitsLocked(bytes int64) bool {
	return bytes == 0 || b.held == 0 || bytes <= b.limit-b.held
}

// releaseLocked returns up to n bytes of the lease to the budget, and admits the waiters that
// now fit.
func (b *Budget) releaseLocked(l *Lease, n int64) {
	n = min(max(n, 0), l.held)
	if n == 0 {
		return
	}

	l.held -= n
	b.held -= n
	b.admitLocked()
}

// compareWaiters orders waiters by preference: object, then section, then arrival.
func compareWaiters(a, b *waiter) int {
	return cmp.Or(
		cmp.Compare(a.rank, b.rank),
		cmp.Compare(a.section, b.section),
		cmp.Compare(a.seq, b.seq),
	)
}

// noCopy makes go vet report a copy of the value that holds it.
type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// Lease holds the bytes that one reservation reserved.
//
// A Lease is safe for concurrent use. Do not copy it: a copy would return its bytes twice. The
// methods of a nil or zero Lease do nothing, so a reader without a budget can call them without a
// check.
type Lease struct {
	_ noCopy

	budget *Budget
	held   int64 // Guarded by budget.mu.
}

// Release returns n bytes to the budget. It returns no more than the lease still holds, and it
// ignores n below one.
func (l *Lease) Release(n int64) {
	if l == nil || l.budget == nil || n <= 0 {
		return
	}

	l.budget.mu.Lock()
	defer l.budget.mu.Unlock()
	l.budget.releaseLocked(l, n)
}

// Close returns all the bytes the lease still holds. It is safe to call more than once.
func (l *Lease) Close() {
	if l == nil || l.budget == nil {
		return
	}

	l.budget.mu.Lock()
	defer l.budget.mu.Unlock()
	l.budget.releaseLocked(l, l.held)
}
