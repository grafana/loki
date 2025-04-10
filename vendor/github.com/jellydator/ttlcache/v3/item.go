package ttlcache

import (
	"sync"
	"time"
)

const (
	// NoTTL indicates that an item should never expire.
	NoTTL time.Duration = -1

	// PreviousOrDefaultTTL indicates that existing TTL of item should be used
	// default TTL will be used as fallback if item doesn't exist
	PreviousOrDefaultTTL time.Duration = -2

	// DefaultTTL indicates that the default TTL value of the cache
	// instance should be used.
	DefaultTTL time.Duration = 0
)

// Item holds all the information that is associated with a single
// cache value.
type Item[K comparable, V any] struct {
	// the mutex needs to be locked only when:
	// - data fields are being read inside accessor methods
	// - data fields are being updated
	// when data fields are being read in one of the cache's
	// methods, we can be sure that these fields are not modified
	// concurrently since the item list is locked by its own mutex as
	// well, so locking this mutex would be redundant.
	// In other words, this mutex is only useful when these fields
	// are being read from the outside (e.g. in event functions).
	mu         sync.RWMutex
	key        K
	value      V
	ttl        time.Duration
	expiresAt  time.Time
	queueIndex int
	version    int64
}

// newItem creates a new cache item.
func newItem[K comparable, V any](key K, value V, ttl time.Duration, enableVersionTracking bool) *Item[K, V] {
	item := &Item[K, V]{
		key:   key,
		value: value,
		ttl:   ttl,
	}

	if !enableVersionTracking {
		item.version = -1
	}

	item.touch()

	return item
}

// update modifies the item's value, TTL, and version.
func (item *Item[K, V]) update(value V, ttl time.Duration) {
	item.mu.Lock()
	defer item.mu.Unlock()

	item.value = value

	// update version if enabled
	if item.version > -1 {
		item.version++
	}

	// no need to update ttl or expiry in this case
	if ttl == PreviousOrDefaultTTL {
		return
	}

	item.ttl = ttl

	// reset expiration timestamp because the new TTL may be
	// 0 or below
	item.expiresAt = time.Time{}
	item.touchUnsafe()
}

// touch updates the item's expiration timestamp.
func (item *Item[K, V]) touch() {
	item.mu.Lock()
	defer item.mu.Unlock()

	item.touchUnsafe()
}

// touchUnsafe updates the item's expiration timestamp without
// locking the mutex.
func (item *Item[K, V]) touchUnsafe() {
	if item.ttl <= 0 {
		return
	}

	item.expiresAt = time.Now().Add(item.ttl)
}

// IsExpired returns a bool value that indicates whether the item
// is expired.
func (item *Item[K, V]) IsExpired() bool {
	item.mu.RLock()
	defer item.mu.RUnlock()

	return item.isExpiredUnsafe()
}

// isExpiredUnsafe returns a bool value that indicates whether the
// the item is expired without locking the mutex
func (item *Item[K, V]) isExpiredUnsafe() bool {
	if item.ttl <= 0 {
		return false
	}

	return item.expiresAt.Before(time.Now())
}

// Key returns the key of the item.
func (item *Item[K, V]) Key() K {
	item.mu.RLock()
	defer item.mu.RUnlock()

	return item.key
}

// Value returns the value of the item.
func (item *Item[K, V]) Value() V {
	item.mu.RLock()
	defer item.mu.RUnlock()

	return item.value
}

// TTL returns the TTL value of the item.
func (item *Item[K, V]) TTL() time.Duration {
	item.mu.RLock()
	defer item.mu.RUnlock()

	return item.ttl
}

// ExpiresAt returns the expiration timestamp of the item.
func (item *Item[K, V]) ExpiresAt() time.Time {
	item.mu.RLock()
	defer item.mu.RUnlock()

	return item.expiresAt
}

// Version returns the version of the item. It shows the total number of
// changes made to the item.
// If version tracking is disabled, the return value is always -1.
func (item *Item[K, V]) Version() int64 {
	item.mu.RLock()
	defer item.mu.RUnlock()

	return item.version
}
