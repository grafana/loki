package cache

import (
	"container/heap"
	"time"
)

type expirationManager[K comparable] struct {
	queue   expirationQueue[K]
	mapping map[K]*expirationKey[K]
}

func newExpirationManager[K comparable]() *expirationManager[K] {
	q := make(expirationQueue[K], 0)
	heap.Init(&q)
	return &expirationManager[K]{
		queue:   q,
		mapping: make(map[K]*expirationKey[K]),
	}
}

func (m *expirationManager[K]) update(key K, expiration time.Time) {
	if e, ok := m.mapping[key]; ok {
		e.expiration = expiration
		heap.Fix(&m.queue, e.index)
	} else {
		v := &expirationKey[K]{
			key:        key,
			expiration: expiration,
		}
		heap.Push(&m.queue, v)
		m.mapping[key] = v
	}
}

func (m *expirationManager[K]) len() int {
	return m.queue.Len()
}

func (m *expirationManager[K]) pop() K {
	v := heap.Pop(&m.queue)
	key := v.(*expirationKey[K]).key
	delete(m.mapping, key)
	return key
}

func (m *expirationManager[K]) remove(key K) {
	if e, ok := m.mapping[key]; ok {
		heap.Remove(&m.queue, e.index)
		delete(m.mapping, key)
	}
}

type expirationKey[K comparable] struct {
	key        K
	expiration time.Time
	index      int
}

// expirationQueue implements heap.Interface and holds CacheItems.
type expirationQueue[K comparable] []*expirationKey[K]

var _ heap.Interface = (*expirationQueue[int])(nil)

func (pq expirationQueue[K]) Len() int { return len(pq) }

func (pq expirationQueue[K]) Less(i, j int) bool {
	// We want Pop to give us the least based on expiration time, not the greater
	return pq[i].expiration.Before(pq[j].expiration)
}

func (pq expirationQueue[K]) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *expirationQueue[K]) Push(x interface{}) {
	n := len(*pq)
	item := x.(*expirationKey[K])
	item.index = n
	*pq = append(*pq, item)
}

func (pq *expirationQueue[K]) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	item.index = -1 // For safety
	*pq = old[0 : n-1]
	return item
}
