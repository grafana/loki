// Package deadhorse defines the shared request/response vocabulary between
// a DeadHorse server and its clients -- RequestEntry, ResponseEntry, and
// Limit. It has no dependency beyond the standard library and no opinion on
// how a Throttle call is actually carried out -- see the server subpackage
// for the in-memory engine and DHP/1 server (InMemoryThrottler.Throttle,
// and server.Throttler for what it takes to plug something else into
// TextServer), and the client subpackage for the sharded network client
// (ShardedClient.Throttle).
package deadhorse

import (
	"time"
)

// Limit describes a leaky bucket's shape: how large it is and how quickly
// it drains. Callers typically define one Limit per rate-limited operation
// (e.g. "100 writes/sec for this service") and reuse it across every
// RequestEntry for that operation, rather than recomputing it per request.
type Limit struct {
	Capacity int64 // bucket size, in cost units
	Rate     Rate  // how quickly the bucket drains
}

// Rate is a leak rate expressed as a ratio: Units cost units drain every Period.
type Rate struct {
	Units  int64
	Period time.Duration
}

// RequestEntry is one leaky-bucket check: consume (or peek at) Cost units
// from the bucket identified by Key, shaped by Limit.
type RequestEntry struct {
	Key   string
	Limit Limit
	// Cost is how many units this request consumes. Zero means "not
	// specified," which defaults to 1 -- see EffectiveCost.
	Cost int64
	// Peek, if true, reports what would happen without consuming from the
	// bucket. The zero value (false) is the safe default: an entry that
	// forgets to set this still actually enforces its limit, rather than
	// silently never doing anything.
	//
	// A Peek entry is also exempt from the all-or-none transaction one
	// Throttle call forms for its other entries (see
	// server.InMemoryThrottler.Throttle): it's evaluated and reported
	// entirely on its own, and neither gates nor is gated by whatever
	// non-Peek entries share its call.
	Peek bool
}

// ResponseEntry is the outcome of one RequestEntry, always returned in the
// same order and at the same index as its request.
type ResponseEntry struct {
	Key string
	// Throttled reports whether this request was denied. Its own meaning is
	// unchanged by the all-or-none transaction one Throttle call forms for
	// its non-Peek entries (see server.InMemoryThrottler.Throttle): it
	// still just says whether this request was admitted. What can change
	// is the reason -- a non-Peek entry can come back Throttled even
	// though its own bucket had room, if another non-Peek entry sharing
	// its call was denied; nothing about this field's shape or the wire
	// format changes to reflect that.
	Throttled bool
	// Remaining is the bucket's headroom in cost units as of just before
	// this request, clamped to [0, Limit.Capacity] -- not affected by this
	// request's own Cost or Throttled outcome. A request that itself gets
	// admitted (or throttled) still reports the same Remaining a Peek at
	// that same instant would have; it does not shrink by Cost just because
	// this request consumed from the bucket.
	Remaining int64
	// RetryAfter is how long until this exact request would have fit. Zero
	// when not throttled, or when this exact request could never fit under
	// this Limit at all (e.g. Cost exceeds Limit.Capacity) -- in that case
	// retrying, however long you wait, will not help.
	RetryAfter time.Duration
	// Err is non-nil if this entry couldn't be evaluated normally -- a
	// validation problem (e.g. a key a wire client can't encode) or an
	// operational failure (e.g. the shard that owns this key was
	// unreachable). Throttled still holds a sensible value even when Err is
	// set -- whatever the caller's fail-open/fail-closed policy decided --
	// so code that only reads Throttled works the same whether or not it
	// checks Err.
	Err error
}

// EffectiveCost normalizes a request's cost: zero/negative means "not
// specified," which defaults to 1. Both InMemoryThrottler and the DHP/1
// wire client apply this so an omitted cost means the same thing
// everywhere.
func EffectiveCost(cost int64) int64 {
	if cost <= 0 {
		return 1
	}
	return cost
}

// EffectiveUnits normalizes a Rate's Units the same way EffectiveCost
// normalizes a request's cost: zero/negative means "not specified," which
// defaults to 1 (a plain "one unit per Period" rate). Both InMemoryThrottler
// and the DHP/1 wire client apply this so an omitted Units means the same
// thing everywhere.
func EffectiveUnits(units int64) int64 {
	if units <= 0 {
		return 1
	}
	return units
}
