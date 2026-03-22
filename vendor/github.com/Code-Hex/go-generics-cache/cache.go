package cache

import (
	"context"
	"sync"
	"time"

	"github.com/Code-Hex/go-generics-cache/policy/clock"
	"github.com/Code-Hex/go-generics-cache/policy/fifo"
	"github.com/Code-Hex/go-generics-cache/policy/lfu"
	"github.com/Code-Hex/go-generics-cache/policy/lru"
	"github.com/Code-Hex/go-generics-cache/policy/mru"
	"github.com/Code-Hex/go-generics-cache/policy/simple"
)

// Interface is a common-cache interface.
type Interface[K comparable, V any] interface {
	// Get looks up a key's value from the cache.
	Get(key K) (value V, ok bool)
	// Set sets a value to the cache with key. replacing any existing value.
	Set(key K, val V)
	// Keys returns the keys of the cache. The order is relied on algorithms.
	Keys() []K
	// Delete deletes the item with provided key from the cache.
	Delete(key K)
	// Len returns the number of items in the cache.
	Len() int
}

var (
	_ = []Interface[struct{}, any]{
		(*simple.Cache[struct{}, any])(nil),
		(*lru.Cache[struct{}, any])(nil),
		(*lfu.Cache[struct{}, any])(nil),
		(*fifo.Cache[struct{}, any])(nil),
		(*mru.Cache[struct{}, any])(nil),
		(*clock.Cache[struct{}, any])(nil),
	}
)

// Item is an item
type Item[K comparable, V any] struct {
	Key                   K
	Value                 V
	Expiration            time.Time
	InitialReferenceCount int
}

func (item *Item[K, V]) hasExpiration() bool {
	return !item.Expiration.IsZero()
}

// Expired returns true if the item has expired.
func (item *Item[K, V]) Expired() bool {
	if !item.hasExpiration() {
		return false
	}
	return nowFunc().After(item.Expiration)
}

// GetReferenceCount returns reference count to be used when setting
// the cache item for the first time.
func (item *Item[K, V]) GetReferenceCount() int {
	return item.InitialReferenceCount
}

var nowFunc = time.Now

// ItemOption is an option for cache item.
type ItemOption func(*itemOptions)

type itemOptions struct {
	expiration     time.Time // default none
	referenceCount int
}

// WithExpiration is an option to set expiration time for any items.
// If the expiration is zero or negative value, it treats as w/o expiration.
func WithExpiration(exp time.Duration) ItemOption {
	return func(o *itemOptions) {
		o.expiration = nowFunc().Add(exp)
	}
}

// WithReferenceCount is an option to set reference count for any items.
// This option is only applicable to cache policies that have a reference count (e.g., Clock, LFU).
// referenceCount specifies the reference count value to set for the cache item.
//
// the default is 1.
func WithReferenceCount(referenceCount int) ItemOption {
	return func(o *itemOptions) {
		o.referenceCount = referenceCount
	}
}

// newItem creates a new item with specified any options.
func newItem[K comparable, V any](key K, val V, opts ...ItemOption) *Item[K, V] {
	o := new(itemOptions)
	for _, optFunc := range opts {
		optFunc(o)
	}
	return &Item[K, V]{
		Key:                   key,
		Value:                 val,
		Expiration:            o.expiration,
		InitialReferenceCount: o.referenceCount,
	}
}

// Cache is a thread safe cache.
type Cache[K comparable, V any] struct {
	cache Interface[K, *Item[K, V]]
	// mu is used to do lock in some method process.
	mu         sync.Mutex
	janitor    *janitor
	expManager *expirationManager[K]
}

// Option is an option for cache.
type Option[K comparable, V any] func(*options[K, V])

type options[K comparable, V any] struct {
	cache           Interface[K, *Item[K, V]]
	janitorInterval time.Duration
}

func newOptions[K comparable, V any]() *options[K, V] {
	return &options[K, V]{
		cache:           simple.NewCache[K, *Item[K, V]](),
		janitorInterval: time.Minute,
	}
}

// AsLRU is an option to make a new Cache as LRU algorithm.
func AsLRU[K comparable, V any](opts ...lru.Option) Option[K, V] {
	return func(o *options[K, V]) {
		o.cache = lru.NewCache[K, *Item[K, V]](opts...)
	}
}

// AsLFU is an option to make a new Cache as LFU algorithm.
func AsLFU[K comparable, V any](opts ...lfu.Option) Option[K, V] {
	return func(o *options[K, V]) {
		o.cache = lfu.NewCache[K, *Item[K, V]](opts...)
	}
}

// AsFIFO is an option to make a new Cache as FIFO algorithm.
func AsFIFO[K comparable, V any](opts ...fifo.Option) Option[K, V] {
	return func(o *options[K, V]) {
		o.cache = fifo.NewCache[K, *Item[K, V]](opts...)
	}
}

// AsMRU is an option to make a new Cache as MRU algorithm.
func AsMRU[K comparable, V any](opts ...mru.Option) Option[K, V] {
	return func(o *options[K, V]) {
		o.cache = mru.NewCache[K, *Item[K, V]](opts...)
	}
}

// AsClock is an option to make a new Cache as clock algorithm.
func AsClock[K comparable, V any](opts ...clock.Option) Option[K, V] {
	return func(o *options[K, V]) {
		o.cache = clock.NewCache[K, *Item[K, V]](opts...)
	}
}

// WithJanitorInterval is an option to specify how often cache should delete expired items.
//
// Default is 1 minute.
func WithJanitorInterval[K comparable, V any](d time.Duration) Option[K, V] {
	return func(o *options[K, V]) {
		o.janitorInterval = d
	}
}

// New creates a new thread safe Cache.
// The janitor will not be stopped which is created by this function. If you
// want to stop the janitor gracefully, You should use the `NewContext` function
// instead of this.
//
// There are several Cache replacement policies available with you specified any options.
func New[K comparable, V any](opts ...Option[K, V]) *Cache[K, V] {
	return NewContext(context.Background(), opts...)
}

// NewContext creates a new thread safe Cache with context.
// This function will be stopped an internal janitor when the context is cancelled.
//
// There are several Cache replacement policies available with you specified any options.
func NewContext[K comparable, V any](ctx context.Context, opts ...Option[K, V]) *Cache[K, V] {
	o := newOptions[K, V]()
	for _, optFunc := range opts {
		optFunc(o)
	}
	cache := &Cache[K, V]{
		cache:      o.cache,
		janitor:    newJanitor(ctx, o.janitorInterval),
		expManager: newExpirationManager[K](),
	}
	cache.janitor.run(cache.DeleteExpired)
	return cache
}

// Get looks up a key's value from the cache.
func (c *Cache[K, V]) Get(key K) (zero V, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.cache.Get(key)

	if !ok {
		return
	}

	// Returns nil if the item has been expired.
	// Do not delete here and leave it to an external process such as Janitor.
	if item.Expired() {
		return zero, false
	}

	return item.Value, true
}

// GetOrSet atomically gets a key's value from the cache, or if the
// key is not present, sets the given value.
// The loaded result is true if the value was loaded, false if stored.
func (c *Cache[K, V]) GetOrSet(key K, val V, opts ...ItemOption) (actual V, loaded bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.cache.Get(key)

	if !ok || item.Expired() {
		item := newItem(key, val, opts...)
		c.cache.Set(key, item)
		return val, false
	}

	return item.Value, true
}

// DeleteExpired all expired items from the cache.
func (c *Cache[K, V]) DeleteExpired() {
	c.mu.Lock()
	l := c.expManager.len()
	c.mu.Unlock()

	evict := func() bool {
		key := c.expManager.pop()
		// if is expired, delete it and return nil instead
		item, ok := c.cache.Get(key)
		if ok {
			if item.Expired() {
				c.cache.Delete(key)
				return false
			}
			c.expManager.update(key, item.Expiration)
		}
		return true
	}

	for i := 0; i < l; i++ {
		c.mu.Lock()
		shouldBreak := evict()
		c.mu.Unlock()
		if shouldBreak {
			break
		}
	}
}

// Set sets a value to the cache with key. replacing any existing value.
func (c *Cache[K, V]) Set(key K, val V, opts ...ItemOption) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item := newItem(key, val, opts...)
	if item.hasExpiration() {
		c.expManager.update(key, item.Expiration)
	}
	c.cache.Set(key, item)
}

// Keys returns the keys of the cache. the order is relied on algorithms.
func (c *Cache[K, V]) Keys() []K {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cache.Keys()
}

// Delete deletes the item with provided key from the cache.
func (c *Cache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache.Delete(key)
	c.expManager.remove(key)
}

// Len returns the number of items in the cache.
func (c *Cache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cache.Len()
}

// Contains reports whether key is within cache.
func (c *Cache[K, V]) Contains(key K) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.cache.Get(key)
	return ok
}

// NumberCache is a in-memory cache which is able to store only Number constraint.
type NumberCache[K comparable, V Number] struct {
	*Cache[K, V]
	// nmu is used to do lock in Increment/Decrement process.
	// Note that this must be here as a separate mutex because mu in Cache struct is Locked in Get,
	// and if we call mu.Lock in Increment/Decrement, it will cause deadlock.
	nmu sync.Mutex
}

// NewNumber creates a new cache for Number constraint.
func NewNumber[K comparable, V Number](opts ...Option[K, V]) *NumberCache[K, V] {
	return &NumberCache[K, V]{
		Cache: New(opts...),
	}
}

// Increment an item of type Number constraint by n.
// Returns the incremented value.
func (nc *NumberCache[K, V]) Increment(key K, n V) V {
	// In order to avoid lost update, we must lock whole Increment/Decrement process.
	nc.nmu.Lock()
	defer nc.nmu.Unlock()
	got, _ := nc.Cache.Get(key)
	nv := got + n
	nc.Cache.Set(key, nv)
	return nv
}

// Decrement an item of type Number constraint by n.
// Returns the decremented value.
func (nc *NumberCache[K, V]) Decrement(key K, n V) V {
	nc.nmu.Lock()
	defer nc.nmu.Unlock()
	got, _ := nc.Cache.Get(key)
	nv := got - n
	nc.Cache.Set(key, nv)
	return nv
}
