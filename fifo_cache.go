package chunk

import (
	"sync"
	"time"
)

// fifoCache is a simple string -> interface{} cache which uses a fifo slide to
// manage evictions.  O(1) inserts and updates, O(1) gets.
type fifoCache struct {
	lock    sync.RWMutex
	size    int
	entries []cacheEntry
	index   map[string]int

	// indexes into entries to identify the most recent and least recent entry.
	first, last int
}

type cacheEntry struct {
	updated    time.Time
	key        string
	value      interface{}
	prev, next int
}

func newFifoCache(size int) *fifoCache {
	return &fifoCache{
		size:    size,
		entries: make([]cacheEntry, 0, size),
		index:   make(map[string]int, size),
	}
}

func (c *fifoCache) put(key string, value interface{}) {
	if c.size == 0 {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	// See if we already have the entry
	index, ok := c.index[key]
	if ok {
		entry := c.entries[index]

		entry.updated = time.Now()
		entry.value = value

		// Remove this entry from the FIFO linked-list.
		c.entries[entry.prev].next = entry.next
		c.entries[entry.next].prev = entry.prev

		// Insert it at the beginning
		entry.next = c.first
		entry.prev = c.last
		c.entries[entry.next].prev = index
		c.entries[entry.prev].next = index
		c.first = index

		c.entries[index] = entry
		return
	}

	// Otherwise, see if we need to evict an entry.
	if len(c.entries) >= c.size {
		index = c.last
		entry := c.entries[index]

		c.last = entry.prev
		c.first = index
		delete(c.index, entry.key)
		c.index[key] = index

		entry.updated = time.Now()
		entry.value = value
		entry.key = key
		c.entries[index] = entry
		return
	}

	// Finally, no hit and we have space.
	index = len(c.entries)
	c.entries = append(c.entries, cacheEntry{
		updated: time.Now(),
		key:     key,
		value:   value,
		prev:    c.last,
		next:    c.first,
	})
	c.entries[c.first].prev = index
	c.entries[c.last].next = index
	c.first = index
	c.index[key] = index
}

func (c *fifoCache) get(key string) (value interface{}, updated time.Time, ok bool) {
	if c.size == 0 {
		return
	}

	c.lock.RLock()
	defer c.lock.RUnlock()

	var index int
	index, ok = c.index[key]
	if ok {
		value = c.entries[index].value
		updated = c.entries[index].updated
	}
	return
}
