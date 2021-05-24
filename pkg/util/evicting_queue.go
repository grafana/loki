package util

import (
	"sync"
)

type EvictingQueue struct {
	sync.RWMutex

	capacity int
	entries  []interface{}
	onEvict  func()
}

func NewEvictingQueue(capacity int, onEvict func()) *EvictingQueue {
	return &EvictingQueue{
		capacity: capacity,
		onEvict:  onEvict,
	}
}

func (q *EvictingQueue) Append(entry interface{}) {
	q.Lock()
	defer q.Unlock()

	if len(q.entries) >= q.capacity {
		q.evictOldest()
	}

	q.entries = append(q.entries, entry)
}

func (q *EvictingQueue) evictOldest() {
	q.onEvict()

	q.entries = append(q.entries[:0], q.entries[1:]...)
}

func (q *EvictingQueue) Entries() []interface{} {
	q.RLock()
	defer q.RUnlock()

	return q.entries
}

func (q *EvictingQueue) Length() int {
	q.RLock()
	defer q.RUnlock()

	return len(q.entries)
}

func (q *EvictingQueue) Clear() {
	q.Lock()
	defer q.Unlock()

	q.entries = nil
}
