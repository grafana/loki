package memcache

var nopAllocator = &defaultAllocator{}

func newOptions(opts ...Option) *Options {
	o := &Options{
		Alloc: nopAllocator,
	}

	for _, opt := range opts {
		opt(o)
	}

	return o
}

// shardKeyOf returns the shard key that opts set, or an empty string when they
// set none. With no options it allocates nothing.
func shardKeyOf(opts []Option) string {
	if len(opts) == 0 {
		return ""
	}
	return newOptions(opts...).ShardKey
}

// legalShardKey reports whether shardKey is empty, which means no shard key, or
// follows the rule of an item key.
func legalShardKey(shardKey string) bool {
	return shardKey == "" || legalKey(shardKey)
}

func selectorKey(itemKey, shardKey string) string {
	if shardKey != "" {
		return shardKey
	}
	return itemKey
}

// Options change the behavior of one Client call. The Client builds them by
// applying the Option callbacks of the call to a default Options instance.
type Options struct {
	Alloc Allocator

	// ShardKey, when it is not empty, picks the server of the call instead of
	// the item key.
	ShardKey string
}

// Option is a callback used to modify the Options that a particular Client
// method uses.
type Option func(opts *Options)

// WithAllocator creates a new Option that makes use of a specific memory Allocator
// for result values (Item.Value) loaded from memcached. Get and GetMulti use
// it. The other methods ignore it.
func WithAllocator(alloc Allocator) Option {
	return func(opts *Options) {
		opts.Alloc = alloc
	}
}

// WithShardKey creates an Option that picks the server of a call from shardKey,
// not from the item key. Every Client method that takes an item key accepts it.
// An empty shardKey means no shard key. If a call has several WithShardKey
// options, the last one wins.
//
// Use a shard key when items belong together. Without one, GetMulti splits its
// item keys across servers. With one, a single server gets one request with all
// the item keys. One response holds all the items, so the caller must limit the
// number of item keys.
//
// The caller owns the choice of the shard key. The client does not check that
// the items of a shard key fit on one server, or that one server can carry
// their load. A shard key that groups too many items can fill or overload its
// server. Spreading the items across servers can also be faster, because the
// servers work in parallel.
//
// The client passes the shard key to the ServerSelector. It does not send the
// shard key to the server.
//
// A shard key follows the rule of an item key: at most 250 bytes, with no space
// and no control character. A call with another shard key returns
// ErrMalformedKey.
//
// The item key alone identifies an item on a server. Items with the same item
// key and different shard keys collide when the shard keys pick the same
// server. Keep item keys unique across shard keys.
func WithShardKey(shardKey string) Option {
	return func(opts *Options) {
		opts.ShardKey = shardKey
	}
}

// Allocator allows memory for memcached result values (Item.Value) to be managed by
// callers of the Client instead of by the Client itself. For example, this can be
// used by callers to implement arena-style memory management. The default implementation
// used, when not otherwise overridden, uses `make` and relies on GC for cleanup.
type Allocator interface {
	// Get returns a byte slice with at least sz capacity. Length of the slice is
	// not guaranteed and so must be asserted by callers (the Client).
	Get(sz int) *[]byte
	// Put returns the byte slice to the underlying allocator. The Client will
	// only call this method during error handling when allocated values are not
	// returned to the caller as cache results.
	Put(b *[]byte)
}

type defaultAllocator struct{}

func (d defaultAllocator) Get(sz int) *[]byte {
	b := make([]byte, sz)
	return &b
}

func (d defaultAllocator) Put(_ *[]byte) {
	// no-op
}
