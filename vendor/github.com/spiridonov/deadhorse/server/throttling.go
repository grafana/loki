// Package server is the DeadHorse server side: the in-memory GCRA engine
// and the DHP/1 TextServer built on top of it. A pure client of a remote
// DeadHorse fleet never needs to import this package -- see the client
// subpackage instead.
package server

import (
	"context"
	"sort"
	"time"

	"github.com/spiridonov/deadhorse"
)

// InMemoryThrottler is a leaky-bucket (GCRA) rate limiter over a striped,
// self-expiring in-memory store. It holds no configuration of its own --
// every call carries its own capacity and rate -- and no key survives longer
// than about 1-2 GC intervals past its last touch. The GCRA check itself
// can't fail (a nonsensical limit just fails closed, see gcraCheck); the
// only way Throttle returns a non-nil error is ctx already being done when
// the call starts, in which case every entry is reported as throttled with
// Err set to ctx.Err() -- see Throttler's result-slice contract, which this
// always honors.
//
// One Throttle call is one transaction for its non-Peek entries: every one
// of them is checked before any of them is written, and either all of them
// commit or none do (see evaluateRealGroup). What each entry *reports*,
// though, is always just its own individual check -- ResponseEntry.Throttled
// never reflects a sibling's outcome, only its own -- so a caller that wants
// to know whether the transaction as a whole committed ORs Throttled across
// the entries it sent together: if any one of them is true, none of them
// were actually written. Peek entries are unaffected either way -- each is
// still evaluated and reported entirely on its own.
//
// Unlike client.ShardedClient.Throttle, there is no shard key here:
// InMemoryThrottler never shards -- there is exactly one transaction
// domain, itself -- so it has nothing to route.
type InMemoryThrottler struct {
	store *store
}

var _ Throttler = &InMemoryThrottler{}

// NewInMemoryThrottler starts an InMemoryThrottler with the given number of
// concurrency stripes and GC interval; zero/negative values fall back to
// DefaultStripes/DefaultGCInterval. Call Close when done to stop its GC
// goroutine.
func NewInMemoryThrottler(numStripes int, gcInterval time.Duration) *InMemoryThrottler {
	return &InMemoryThrottler{store: newStore(numStripes, gcInterval)}
}

func (t *InMemoryThrottler) Throttle(ctx context.Context, entries []deadhorse.RequestEntry) ([]deadhorse.ResponseEntry, error) {
	if err := ctx.Err(); err != nil {
		// Not evaluated at all -- fail closed (Throttled: true), the safer
		// default when there's no real answer to report, and still a
		// same-length, same-order result slice per Throttler's contract
		// rather than a bare nil.
		result := make([]deadhorse.ResponseEntry, len(entries))
		for i, e := range entries {
			result[i] = deadhorse.ResponseEntry{Key: e.Key, Throttled: true, Err: err}
		}
		return result, err
	}

	now := time.Now().UnixNano()
	result := make([]deadhorse.ResponseEntry, len(entries))

	var realIdx []int
	for i, e := range entries {
		result[i].Key = e.Key
		if !e.Peek {
			realIdx = append(realIdx, i)
			continue
		}

		// Peek entries are independent of everything else in the call --
		// see evaluateRealGroup's doc comment -- so each is just evaluated
		// and immediately released, exactly as before this all-or-none
		// grouping existed.
		b := t.store.stripeFor(e.Key).getOrCreate(e.Key)
		cost := deadhorse.EffectiveCost(e.Cost)

		b.mu.Lock()
		throttled, remaining, retryAfter, _ := gcraCheck(b.tat, now, e.Limit.Capacity, deadhorse.EffectiveUnits(e.Limit.Rate.Units), e.Limit.Rate.Period, cost)
		b.mu.Unlock()

		result[i].Throttled = throttled
		result[i].Remaining = remaining
		result[i].RetryAfter = retryAfter
	}

	if len(realIdx) > 0 {
		t.evaluateRealGroup(entries, realIdx, now, result)
	}
	return result, nil
}

// evaluateRealGroup evaluates every non-Peek ("Real") entry in one Throttle
// call as a single all-or-none transaction: every entry's GCRA check is run
// first, against a private, in-memory working copy of each touched key's
// TAT, without writing anything back to the store. Only if every one of
// them individually admits does the combined effect of all of them actually
// get written back; otherwise none of it does. Each entry still *reports*
// its own individual check, though, regardless of what the transaction as a
// whole decided -- see Throttler and ResponseEntry.Throttled; a caller that
// wants the transaction's outcome ORs Throttled across the entries it sent
// together. Peek entries never reach here at all: they're handled
// entirely separately, in Throttle above, and never interact with this
// group in either direction.
//
// Locking is a purely local, in-process problem, not a distributed one:
// every key this group touches has its bucketState.mu acquired up front, in
// a fixed (sorted) order, so that two concurrent calls touching an
// overlapping set of keys in different orders can never deadlock on each
// other. Holding every touched lock for the whole evaluation is what makes
// "check everything, then write everything" atomic: no other call touching
// one of these same keys can interleave a write in between.
func (t *InMemoryThrottler) evaluateRealGroup(entries []deadhorse.RequestEntry, realIdx []int, now int64, result []deadhorse.ResponseEntry) {
	buckets := make(map[string]*bucketState, len(realIdx))
	for _, i := range realIdx {
		key := entries[i].Key
		if _, ok := buckets[key]; !ok {
			buckets[key] = t.store.stripeFor(key).getOrCreate(key)
		}
	}

	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		buckets[k].mu.Lock()
	}
	defer func() {
		for _, k := range keys {
			buckets[k].mu.Unlock()
		}
	}()

	// workingTAT is each touched key's TAT as this group would leave it if
	// every entry admitted so far keeps admitting -- seeded from the real,
	// currently-committed value and advanced in place as entries within
	// this same group pass, so two Real entries sharing a key in one line
	// still chain exactly as sequential calls would. Nothing here is
	// written back to buckets until admitAll is known.
	workingTAT := make(map[string]int64, len(buckets))
	for k, b := range buckets {
		workingTAT[k] = b.tat
	}

	admitAll := true
	for _, i := range realIdx {
		e := entries[i]
		cost := deadhorse.EffectiveCost(e.Cost)
		throttled, remaining, retryAfter, admittedTAT := gcraCheck(workingTAT[e.Key], now, e.Limit.Capacity, deadhorse.EffectiveUnits(e.Limit.Rate.Units), e.Limit.Rate.Period, cost)
		if throttled {
			admitAll = false
		} else {
			workingTAT[e.Key] = admittedTAT
		}
		// Throttled is this entry's own check, full stop -- not the group's
		// decision (see ResponseEntry.Throttled). A sibling failing elsewhere
		// in this same group can still mean nothing gets committed below,
		// even for an entry that reports Throttled=false here.
		result[i].Throttled = throttled
		result[i].Remaining = remaining
		result[i].RetryAfter = retryAfter
	}

	if admitAll {
		for k, tat := range workingTAT {
			buckets[k].tat = tat
		}
	}
}

// Close stops the background GC goroutine. It does not otherwise release
// memory held by the store -- the process is expected to exit shortly after.
func (t *InMemoryThrottler) Close() {
	t.store.close()
}

func (t *InMemoryThrottler) keyCountEstimate() int {
	return t.store.keyCountEstimate()
}
