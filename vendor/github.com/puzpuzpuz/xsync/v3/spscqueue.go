package xsync

import (
	"sync/atomic"
)

// A SPSCQueue is a bounded single-producer single-consumer concurrent
// queue. This means that not more than a single goroutine must be
// publishing items to the queue while not more than a single goroutine
// must be consuming those items.
//
// SPSCQueue instances must be created with NewSPSCQueue function.
// A SPSCQueue must not be copied after first use.
//
// Based on the data structure from the following article:
// https://rigtorp.se/ringbuffer/
type SPSCQueue struct {
	cap  uint64
	pidx uint64
	//lint:ignore U1000 prevents false sharing
	pad0       [cacheLineSize - 8]byte
	pcachedIdx uint64
	//lint:ignore U1000 prevents false sharing
	pad1 [cacheLineSize - 8]byte
	cidx uint64
	//lint:ignore U1000 prevents false sharing
	pad2       [cacheLineSize - 8]byte
	ccachedIdx uint64
	//lint:ignore U1000 prevents false sharing
	pad3  [cacheLineSize - 8]byte
	items []interface{}
}

// NewSPSCQueue creates a new SPSCQueue instance with the given
// capacity.
func NewSPSCQueue(capacity int) *SPSCQueue {
	if capacity < 1 {
		panic("capacity must be positive number")
	}
	return &SPSCQueue{
		cap:   uint64(capacity + 1),
		items: make([]interface{}, capacity+1),
	}
}

// TryEnqueue inserts the given item into the queue. Does not block
// and returns immediately. The result indicates that the queue isn't
// full and the item was inserted.
func (q *SPSCQueue) TryEnqueue(item interface{}) bool {
	// relaxed memory order would be enough here
	idx := atomic.LoadUint64(&q.pidx)
	nextIdx := idx + 1
	if nextIdx == q.cap {
		nextIdx = 0
	}
	cachedIdx := q.ccachedIdx
	if nextIdx == cachedIdx {
		cachedIdx = atomic.LoadUint64(&q.cidx)
		q.ccachedIdx = cachedIdx
		if nextIdx == cachedIdx {
			return false
		}
	}
	q.items[idx] = item
	atomic.StoreUint64(&q.pidx, nextIdx)
	return true
}

// TryDequeue retrieves and removes the item from the head of the
// queue. Does not block and returns immediately. The ok result
// indicates that the queue isn't empty and an item was retrieved.
func (q *SPSCQueue) TryDequeue() (item interface{}, ok bool) {
	// relaxed memory order would be enough here
	idx := atomic.LoadUint64(&q.cidx)
	cachedIdx := q.pcachedIdx
	if idx == cachedIdx {
		cachedIdx = atomic.LoadUint64(&q.pidx)
		q.pcachedIdx = cachedIdx
		if idx == cachedIdx {
			return
		}
	}
	item = q.items[idx]
	q.items[idx] = nil
	ok = true
	nextIdx := idx + 1
	if nextIdx == q.cap {
		nextIdx = 0
	}
	atomic.StoreUint64(&q.cidx, nextIdx)
	return
}
