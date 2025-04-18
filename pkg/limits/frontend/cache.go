package frontend

import (
	"sync"
	"time"

	"github.com/coder/quartz"
)

type Cache[K comparable, V any] interface {
	// Get returns the value for the key. It returns true if the key exists,
	// otherwise false.
	Get(K) (V, bool)
	// Set stores the value for the key.
	Set(K, V)
	// Delete removes the key. If the key does not exist, the operation is a
	// no-op.
	Delete(K)
	// Reset removes all keys.
	Reset()
}

// item contains the value and expiration time for a key.
type item[V any] struct {
	value     V
	expiresAt time.Time
}

func (i *item[V]) hasExpired(now time.Time) bool {
	return i.expiresAt.Before(now) || i.expiresAt.Equal(now)
}

// TTLCache is a simple, thread-safe cache with a single per-cache TTL.
type TTLCache[K comparable, V any] struct {
	items map[K]item[V]
	ttl   time.Duration
	mu    sync.RWMutex

	// Used for tests.
	clock quartz.Clock
}

func NewTTLCache[K comparable, V any](ttl time.Duration) *TTLCache[K, V] {
	return &TTLCache[K, V]{
		items: make(map[K]item[V]),
		ttl:   ttl,
		clock: quartz.NewReal(),
	}
}

// Get implements Cache.Get.
func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	var (
		value  V
		exists bool
		now    = c.clock.Now()
	)
	c.mu.RLock()
	defer c.mu.RUnlock()
	if item, ok := c.items[key]; ok && !item.hasExpired(now) {
		value = item.value
		exists = true
	}
	return value, exists
}

// Set implements Cache.Set.
func (c *TTLCache[K, V]) Set(key K, value V) {
	now := c.clock.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = item[V]{
		value:     value,
		expiresAt: now.Add(c.ttl),
	}
	c.removeExpiredItems(now)
}

// Delete implements Cache.Delete.
func (c *TTLCache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// Reset implements Cache.Reset.
func (c *TTLCache[K, V]) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[K]item[V])
}

// removeExpiredItems removes expired items.
func (c *TTLCache[K, V]) removeExpiredItems(now time.Time) {
	for key, item := range c.items {
		if item.hasExpired(now) {
			delete(c.items, key)
		}
	}
}

// NopCache is a no-op cache. It does not store any keys. It is used in tests
// and as a stub for disabled caches.
type NopCache[K comparable, V any] struct{}

func NewNopCache[K comparable, V any]() *NopCache[K, V] {
	return &NopCache[K, V]{}
}
func (c *NopCache[K, V]) Get(_ K) (V, bool) {
	var value V
	return value, false
}
func (c *NopCache[K, V]) Set(_ K, _ V) {}
func (c *NopCache[K, V]) Delete(_ K)   {}
func (c *NopCache[K, V]) Reset()       {}
