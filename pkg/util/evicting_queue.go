package util

import (
	"errors"
	"sync"
)

type EvictingQueue struct {
	sync.RWMutex

	capacity int
	entries  []interface{}
	onEvict  func()
}

func NewEvictingQueue(capacity int, onEvict func()) (*EvictingQueue, error) {
	if capacity <= 0 {
		// a queue of 0 (or smaller) capacity is invalid
		return nil, errors.New("queue cannot have a zero or negative capacity")
	}

	return &EvictingQueue{
		capacity: capacity,
		onEvict:  onEvict,
		entries:  make([]interface{}, 0, capacity),
	}, nil
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

	start := (len(q.entries) - q.Capacity()) + 1
	q.entries = append(q.entries[:0], q.entries[start:]...)
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

func (q *EvictingQueue) Capacity() int {
	return q.capacity
}

func (q *EvictingQueue) Clear() {
	q.Lock()
	defer q.Unlock()

	q.entries = q.entries[:0]
}
