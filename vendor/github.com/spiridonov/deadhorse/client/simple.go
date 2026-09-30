package client

import (
	"context"

	"github.com/spiridonov/deadhorse"
)

// Client is a DHP/1 client for a single DeadHorse server address. It's a
// thin wrapper around ShardedClient with exactly one shard: everything
// ShardedClient does for connection handling, timeouts, and circuit
// breaking still applies, but callers who only ever talk to one server
// don't need to think about shardKey or sharding at all -- see
// ShardedClient's doc comment if that need ever grows into more than one
// server.
type Client struct {
	sharded *ShardedClient
}

// NewClient builds a client for a single DeadHorse server at addr
// (host:port). The connection is opened lazily, on first use. Options are
// exactly ShardedClient's (WithTimeout, WithFailClosed, WithBreaker).
func NewClient(addr string, opts ...Option) *Client {
	return &Client{sharded: NewShardedClient([]string{addr}, opts...)}
}

// Throttle sends every entry together as one DHP/1 line -- one all-or-none
// transaction for its non-Peek entries (see server.InMemoryThrottler.Throttle
// for exactly how). Unlike ShardedClient.Throttle, there's no shardKey
// argument: with a single server there's nothing to route, so every call is
// already the whole transaction. The returned slice is always fully
// populated, in the caller's original order, even when the returned error is
// non-nil.
func (c *Client) Throttle(ctx context.Context, entries []deadhorse.RequestEntry) ([]deadhorse.ResponseEntry, error) {
	return c.sharded.Throttle(ctx, "", entries)
}

// Close closes the underlying connection.
func (c *Client) Close() {
	c.sharded.Close()
}
