package endpointdiscovery

import (
	"sync"
	"sync/atomic"
)

// EndpointCache is an LRU cache that holds a series of endpoints
// based on some key. The data structure makes use of a read write
// mutex to enable asynchronous use.
type EndpointCache struct {
	// size is used to count the number elements in the cache.
	// The atomic package is used to ensure this size is accurate when
	// using multiple goroutines.
	size          int64
	endpoints     sync.Map
	endpointLimit int64
}

// NewEndpointCache will return a newly initialized cache with a limit
// of endpointLimit entries.
func NewEndpointCache(endpointLimit int64) *EndpointCache {
	return &EndpointCache{
		endpointLimit: endpointLimit,
		endpoints:     sync.Map{},
	}
}

// Get is a concurrent safe get operation that will retrieve an endpoint
// based on endpointKey. A boolean will also be returned to illustrate whether
// or not the endpoint had been found.
func (c *EndpointCache) get(endpointKey string) (Endpoint, bool) {
	endpoint, ok := c.endpoints.Load(endpointKey)
	if !ok {
		return Endpoint{}, false
	}

	ev := endpoint.(Endpoint)
	ev.Prune()

	c.endpoints.Store(endpointKey, ev)
	return endpoint.(Endpoint), true
}

// Has returns if the enpoint cache contains a valid entry for the endpoint key
// provided.
func (c *EndpointCache) Has(endpointKey string) bool {
	_, found := c.Get(endpointKey)
	return found
}

// Get will retrieve a weighted address  based off of the endpoint key. If an endpoint
// should be retrieved, due to not existing or the current endpoint has expired
// the Discoverer object that was passed in will attempt to discover a new endpoint
// and add that to the cache.
func (c *EndpointCache) Get(endpointKey string) (WeightedAddress, bool) {
	endpoint, ok := c.get(endpointKey)
	if !ok {
		return WeightedAddress{}, false
	}
	return endpoint.GetValidAddress()
}

// Add is a concurrent safe operation that will allow new endpoints to be added
// to the cache. If the cache is full, the number of endpoints equal endpointLimit,
// then this will remove the oldest entry before adding the new endpoint.
func (c *EndpointCache) Add(endpoint Endpoint) {
	// de-dups multiple adds of an endpoint with a pre-existing key
	if iface, ok := c.endpoints.Load(endpoint.Key); ok {
		e := iface.(Endpoint)
		if e.Len() > 0 {
			return
		}
	}

	size := atomic.AddInt64(&c.size, 1)
	if size > 0 && size > c.endpointLimit {
		c.deleteRandomKey()
	}

	c.endpoints.Store(endpoint.Key, endpoint)
}

// deleteRandomKey will delete a random key from the cache. If
// no key was deleted false will be returned.
func (c *EndpointCache) deleteRandomKey() bool {
	atomic.AddInt64(&c.size, -1)
	found := false

	c.endpoints.Range(func(key, value interface{}) bool {
		found = true
		c.endpoints.Delete(key)

		return false
	})

	return found
}
