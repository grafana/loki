package mru

import (
	"container/list"
)

// Cache is used a MRU (Most recently used) cache replacement policy.
//
// In contrast to Least Recently Used (LRU), MRU discards the most recently used items first.
type Cache[K comparable, V any] struct {
	cap   int
	list  *list.List
	items map[K]*list.Element
}

type entry[K comparable, V any] struct {
	key K
	val V
}

// Option is an option for MRU cache.
type Option func(*options)

type options struct {
	capacity int
}

func newOptions() *options {
	return &options{
		capacity: 128,
	}
}

// WithCapacity is an option to set cache capacity.
func WithCapacity(cap int) Option {
	return func(o *options) {
		o.capacity = cap
	}
}

// NewCache creates a new non-thread safe MRU cache whose capacity is the default size (128).
func NewCache[K comparable, V any](opts ...Option) *Cache[K, V] {
	o := newOptions()
	for _, optFunc := range opts {
		optFunc(o)
	}
	return &Cache[K, V]{
		cap:   o.capacity,
		list:  list.New(),
		items: make(map[K]*list.Element, o.capacity),
	}
}

// Get looks up a key's value from the cache.
func (c *Cache[K, V]) Get(key K) (zero V, _ bool) {
	e, ok := c.items[key]
	if !ok {
		return
	}
	// updates cache order
	c.list.MoveToBack(e)
	return e.Value.(*entry[K, V]).val, true
}

// Set sets a value to the cache with key. replacing any existing value.
func (c *Cache[K, V]) Set(key K, val V) {
	if e, ok := c.items[key]; ok {
		// updates cache order
		c.list.MoveToBack(e)
		entry := e.Value.(*entry[K, V])
		entry.val = val
		return
	}

	if c.list.Len() == c.cap {
		c.deleteNewest()
	}

	newEntry := &entry[K, V]{
		key: key,
		val: val,
	}
	e := c.list.PushBack(newEntry)
	c.items[key] = e
}

// Keys returns the keys of the cache. the order is from recently used.
func (c *Cache[K, V]) Keys() []K {
	keys := make([]K, 0, len(c.items))
	for ent := c.list.Back(); ent != nil; ent = ent.Prev() {
		entry := ent.Value.(*entry[K, V])
		keys = append(keys, entry.key)
	}
	return keys
}

// Len returns the number of items in the cache.
func (c *Cache[K, V]) Len() int {
	return c.list.Len()
}

// Delete deletes the item with provided key from the cache.
func (c *Cache[K, V]) Delete(key K) {
	if e, ok := c.items[key]; ok {
		c.delete(e)
	}
}

func (c *Cache[K, V]) deleteNewest() {
	e := c.list.Front()
	c.delete(e)
}

func (c *Cache[K, V]) delete(e *list.Element) {
	c.list.Remove(e)
	entry := e.Value.(*entry[K, V])
	delete(c.items, entry.key)
}
