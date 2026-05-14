package cache

import "sync"

// DefaultCache is the default cache implementation using sync.Map for thread-safe concurrent access.
type DefaultCache struct {
	m *sync.Map
}

var _ SchemaCache = &DefaultCache{}

// NewDefaultCache creates a new DefaultCache with an initialized sync.Map.
func NewDefaultCache() *DefaultCache {
	return &DefaultCache{m: &sync.Map{}}
}

// Load retrieves a schema from the cache.
func (c *DefaultCache) Load(key uint64) (*SchemaCacheEntry, bool) {
	if c == nil || c.m == nil {
		return nil, false
	}
	val, ok := c.m.Load(key)
	if !ok {
		return nil, false
	}
	schemaCache, ok := val.(*SchemaCacheEntry)
	return schemaCache, ok
}

// Store saves a schema to the cache.
func (c *DefaultCache) Store(key uint64, value *SchemaCacheEntry) {
	if c == nil || c.m == nil {
		return
	}
	c.m.Store(key, value)
}

// Range calls f for each entry in the cache (for testing/inspection).
func (c *DefaultCache) Range(f func(key uint64, value *SchemaCacheEntry) bool) {
	if c == nil || c.m == nil {
		return
	}
	c.m.Range(func(k, v interface{}) bool {
		key, ok := k.(uint64)
		if !ok {
			return true
		}
		val, ok := v.(*SchemaCacheEntry)
		if !ok {
			return true
		}
		return f(key, val)
	})
}
