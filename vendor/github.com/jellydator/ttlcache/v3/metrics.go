package ttlcache

// Metrics contains common cache metrics calculated over the course
// of the cache's lifetime.
type Metrics struct {
	// Insertions specifies how many items were inserted.
	Insertions uint64

	// Hits specifies how many items were successfully retrieved
	// from the cache.
	// Retrievals made with a loader function are not tracked.
	Hits uint64

	// Misses specifies how many items were not found in the cache.
	// Retrievals made with a loader function are considered misses as
	// well.
	Misses uint64

	// Evictions specifies how many items were removed from the
	// cache.
	Evictions uint64
}
