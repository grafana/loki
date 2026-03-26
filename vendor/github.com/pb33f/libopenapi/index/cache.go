// Copyright 2023-2024 Princess Beef Heavy Industries, LLC / Dave Shanley
// https://pb33f.io

package index

import (
	"sync"
	"sync/atomic"
)

// SetCache sets a sync map as a temporary cache for the index.
func (index *SpecIndex) SetCache(sync *sync.Map) {
	index.cache = sync
}

// HighCacheHit increments the counter of high cache hits by one, and returns the current value of hits.
func (index *SpecIndex) HighCacheHit() uint64 {
	index.highModelCache.AddHit()
	return index.highModelCache.GetHits()
}

// HighCacheMiss increments the counter of high cache misses by one, and returns the current value of misses.
func (index *SpecIndex) HighCacheMiss() uint64 {
	index.highModelCache.AddMiss()
	return index.highModelCache.GetMisses()
}

// GetHighCacheHits returns the number of hits on the high model cache.
func (index *SpecIndex) GetHighCacheHits() uint64 {
	return index.highModelCache.GetHits()
}

// GetHighCacheMisses returns the number of misses on the high model cache.
func (index *SpecIndex) GetHighCacheMisses() uint64 {
	return index.highModelCache.GetMisses()
}

// GetHighCache returns the high model cache for this index.
func (index *SpecIndex) GetHighCache() Cache {
	return index.highModelCache
}

// InitHighCache allocates a new high model cache onto the index.
func (index *SpecIndex) InitHighCache() {
	index.highModelCache = CreateNewCache()
}

// SetHighCache sets the high model cache for this index.
func (index *SpecIndex) SetHighCache(cache *SimpleCache) {
	index.highModelCache = cache
}

// Cache is an interface for a simple cache that can be used by any consumer.
type Cache interface {
	SetStore(*sync.Map)
	GetStore() *sync.Map
	AddHit() uint64
	AddMiss() uint64
	GetHits() uint64
	GetMisses() uint64
	Clear()
	Load(any) (any, bool)
	Store(any, any)
}

// Below is an implementation of Cache called SimpleCache.

// SimpleCache is a simple cache for the index, or any other consumer that needs it.
type SimpleCache struct {
	store  *sync.Map
	hits   atomic.Uint64
	misses atomic.Uint64
}

// CreateNewCache creates a new simple cache with a sync.Map store.
func CreateNewCache() Cache {
	return &SimpleCache{store: &sync.Map{}}
}

// SetStore sets the store for the cache.
func (c *SimpleCache) SetStore(store *sync.Map) {
	c.store = store
}

// GetStore returns the store for the cache.
func (c *SimpleCache) GetStore() *sync.Map {
	return c.store
}

// Load retrieves a value from the cache.
func (c *SimpleCache) Load(key any) (value any, ok bool) {
	return c.store.Load(key)
}

// Store stores a key-value pair in the cache.
func (c *SimpleCache) Store(key, value any) {
	c.store.Store(key, value)
}

// AddHit increments the hit counter by one, and returns the current value of hits.
func (c *SimpleCache) AddHit() uint64 {
	return c.hits.Add(1)
}

// AddMiss increments the miss counter by one, and returns the current value of misses.
func (c *SimpleCache) AddMiss() uint64 {
	return c.misses.Add(1)
}

// GetHits returns the current value of hits.
func (c *SimpleCache) GetHits() uint64 {
	return c.hits.Load()
}

// GetMisses returns the current value of misses.
func (c *SimpleCache) GetMisses() uint64 {
	return c.misses.Load()
}

// Clear clears the cache.
func (c *SimpleCache) Clear() {
	c.store.Clear()
}
