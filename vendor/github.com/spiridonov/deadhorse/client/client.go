// Package client is a DHP/1 client for DeadHorse.
//
// A ShardedClient holds a static list of shard addresses and, for every
// Throttle call, picks exactly one shard by hashing that call's shardKey --
// no discovery, no rebalancing, no coordination with the servers at all.
package client

import (
	"context"
	"time"

	"github.com/spiridonov/deadhorse"
)

const defaultTimeout = 10 * time.Millisecond

// ShardedClient routes every Throttle call, as a whole, to shard
// hash(shardKey) % len(addrs) -- a static list configured at construction
// time. This is deliberately plain modulo hashing rather than consistent
// hashing: remapping a key when the shard count changes just resets that
// key's bucket on its new shard (one extra burst of unthrottled traffic,
// once), which is cheap enough here that the simpler scheme is the right
// default. Switch to a consistent-hashing variant instead if the shard
// count changes often enough that even brief under-enforcement during a
// resize is a problem.
//
// One call is one shard, and therefore one DHP/1 line: every entry passed
// to a single Throttle call is sent together, and the server commits every
// non-Peek one of them as a single all-or-none group (see
// server.InMemoryThrottler.Throttle for exactly how). This is exactly what
// makes shardKey the caller's tool for controlling that grouping: giving
// two calls' worth of entries the same shardKey and sending them as one
// Throttle call forces them onto the same shard and the same transaction;
// entries that don't need to be decided together belong in separate calls
// (each shardKey can simply be that entry's own Key, matching plain
// per-key routing). Two concurrent Throttle calls never share a line just
// because they hash to the same shard -- each becomes its own line,
// pipelined independently over that shard's connection.
//
// Each shard also carries its own circuit breaker (see circuitBreaker):
// since a shardConn has no memory of a shard's health between calls, a
// shard that's dead or black-holing traffic would otherwise cost every
// caller its full configured timeout, on every single call, forever. Once
// a shard has failed enough consecutive calls in a row, its breaker opens
// and further calls to it fail immediately instead of paying that cost,
// until a periodic trial call finds it healthy again. Tune with
// WithBreaker, or disable it entirely if that's not the right tradeoff for
// a given deployment.
type ShardedClient struct {
	shards   []*shardConn
	timeout  time.Duration
	failOpen bool

	breakerFailureThreshold int64
	breakerOpenDuration     time.Duration
}

type Option func(*ShardedClient)

// WithTimeout overrides the per-call network timeout (default 10ms -- a
// starting point for a same-rack/same-DC deployment; tune from observed
// p99.9 in practice). A context deadline passed to Throttle, if earlier,
// still takes precedence.
func WithTimeout(d time.Duration) Option {
	return func(c *ShardedClient) { c.timeout = d }
}

// WithFailClosed makes a shard that's unreachable or times out count as
// throttled instead of the default fail-open (not throttled): a rate limiter
// should not be the reason the service it protects goes down. Reach for
// fail-closed only for limits that are also a security/quota control, not
// just protective. This never applies to entries rejected by local
// validation (e.g. a malformed key) -- those are always reported as
// throttled, since a caller bug is not something fail-open is meant to
// paper over.
func WithFailClosed() Option {
	return func(c *ShardedClient) { c.failOpen = false }
}

// WithBreaker overrides a shard's circuit breaker defaults (see
// circuitBreaker): after failureThreshold consecutive failures
// attributable to that shard -- a dial/write/read failure, or this
// client's own configured timeout expiring before the shard answered, but
// never a failure that was actually the caller's own ctx giving up first --
// the shard's breaker opens and every call to it fails immediately, with no
// network activity at all, for openDuration. After that, one call is let
// through as a trial: success closes the breaker again; failure reopens it
// and restarts the cooldown.
//
// Pass failureThreshold <= 0 to disable the breaker for this client: every
// call always attempts the network, exactly as if this option were never
// applied.
func WithBreaker(failureThreshold int, openDuration time.Duration) Option {
	return func(c *ShardedClient) {
		c.breakerFailureThreshold = int64(failureThreshold)
		c.breakerOpenDuration = openDuration
	}
}

// NewShardedClient builds a client over a static list of shard addresses
// (host:port). Connections are opened lazily, on first use per shard.
//
// NewShardedClient panics if addrs is empty: shardFor's hash-modulo routing
// has no shard to route to, so an empty list is a caller configuration bug
// (e.g. an unset/empty address flag or environment variable) that's far
// clearer to catch here than as a divide-by-zero panic deep inside the first
// Throttle call.
func NewShardedClient(addrs []string, opts ...Option) *ShardedClient {
	if len(addrs) == 0 {
		panic("deadhorse: NewShardedClient requires at least one shard address")
	}
	c := &ShardedClient{
		timeout:                 defaultTimeout,
		failOpen:                true,
		breakerFailureThreshold: defaultBreakerFailureThreshold,
		breakerOpenDuration:     defaultBreakerOpenDuration,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.shards = make([]*shardConn, len(addrs))
	for i, addr := range addrs {
		c.shards[i] = &shardConn{addr: addr, breaker: newCircuitBreaker(c.breakerFailureThreshold, c.breakerOpenDuration)}
	}
	return c
}

// Throttle sends every entry in one call to a single shard -- hash(shardKey)
// % len(addrs) -- as one DHP/1 line, making the whole call one all-or-none
// transaction for its non-Peek entries. The returned slice is always fully
// populated, in the caller's original order, even when the returned error
// is non-nil.
func (c *ShardedClient) Throttle(ctx context.Context, shardKey string, entries []deadhorse.RequestEntry) ([]deadhorse.ResponseEntry, error) {
	shard := c.shardFor(shardKey)
	return c.shards[shard].throttle(ctx, entries, c.timeout, c.failOpen)
}

func (c *ShardedClient) shardFor(key string) int {
	if len(c.shards) == 1 {
		return 0
	}
	return int(fnv1a(key) % uint64(len(c.shards)))
}

// Close closes every shard's connection.
func (c *ShardedClient) Close() {
	for _, s := range c.shards {
		s.close()
	}
}

func fnv1a(s string) uint64 {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211

	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}
