package simple

import (
	"sort"
	"time"
)

// Cache is a simple cache has no clear priority for evict cache.
type Cache[K comparable, V any] struct {
	items map[K]*entry[V]
}

type entry[V any] struct {
	val       V
	createdAt time.Time
}

// NewCache creates a new non-thread safe cache.
func NewCache[K comparable, V any]() *Cache[K, V] {
	return &Cache[K, V]{
		items: make(map[K]*entry[V], 0),
	}
}

// Set sets any item to the cache. replacing any existing item.
// The default item never expires.
func (c *Cache[K, V]) Set(k K, v V) {
	c.items[k] = &entry[V]{
		val:       v,
		createdAt: time.Now(),
	}
}

// Get gets an item from the cache.
// Returns the item or zero value, and a bool indicating whether the key was found.
func (c *Cache[K, V]) Get(k K) (val V, ok bool) {
	got, found := c.items[k]
	if !found {
		return
	}
	return got.val, true
}

// Keys returns cache keys. the order is sorted by created.
func (c *Cache[K, _]) Keys() []K {
	ret := make([]K, 0, len(c.items))
	for key := range c.items {
		ret = append(ret, key)
	}
	sort.Slice(ret, func(i, j int) bool {
		i1 := c.items[ret[i]]
		i2 := c.items[ret[j]]
		return i1.createdAt.Before(i2.createdAt)
	})
	return ret
}

// Delete deletes the item with provided key from the cache.
func (c *Cache[K, V]) Delete(key K) {
	delete(c.items, key)
}

// Len returns the number of items in the cache.
func (c *Cache[K, V]) Len() int {
	return len(c.items)
}
