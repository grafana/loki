package cache

import (
	"github.com/grafana/gomemcache/memcache"
)

// Option changes one call to Fetch. A backend ignores an option that it does not support.
type Option func(*Options)

// Options holds the settings that the Option values of one Fetch call set.
type Options struct {
	Allocator memcache.Allocator
}

// WithAllocator makes a memcached Fetch allocate the values it returns from alloc.
//
// The caller owns the memory and must not release it while it holds a value. A cache that copies
// a fetched value into another cache must not use the allocator, so a tiered cache drops it.
func WithAllocator(alloc memcache.Allocator) Option {
	return func(o *Options) {
		o.Allocator = alloc
	}
}

func memcacheOptions(opts []Option) []memcache.Option {
	var o Options
	for _, opt := range opts {
		opt(&o)
	}
	if o.Allocator == nil {
		return nil
	}
	return []memcache.Option{memcache.WithAllocator(o.Allocator)}
}
