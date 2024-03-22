package lfu

import (
	"container/heap"
	"time"

	"github.com/Code-Hex/go-generics-cache/policy/internal/policyutil"
)

type entry[K comparable, V any] struct {
	index          int
	key            K
	val            V
	referenceCount int
	referencedAt   time.Time
}

func newEntry[K comparable, V any](key K, val V) *entry[K, V] {
	return &entry[K, V]{
		index:          0,
		key:            key,
		val:            val,
		referenceCount: policyutil.GetReferenceCount(val),
		referencedAt:   time.Now(),
	}
}

func (e *entry[K, V]) referenced() {
	e.referenceCount++
	e.referencedAt = time.Now()
}

type priorityQueue[K comparable, V any] []*entry[K, V]

func newPriorityQueue[K comparable, V any](cap int) *priorityQueue[K, V] {
	queue := make(priorityQueue[K, V], 0, cap)
	return &queue
}

// see example of priority queue: https://pkg.go.dev/container/heap
var _ heap.Interface = (*priorityQueue[struct{}, interface{}])(nil)

func (q priorityQueue[K, V]) Len() int { return len(q) }

func (q priorityQueue[K, V]) Less(i, j int) bool {
	if q[i].referenceCount == q[j].referenceCount {
		return q[i].referencedAt.Before(q[j].referencedAt)
	}
	return q[i].referenceCount < q[j].referenceCount
}

func (q priorityQueue[K, V]) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *priorityQueue[K, V]) Push(x interface{}) {
	entry := x.(*entry[K, V])
	entry.index = len(*q)
	*q = append(*q, entry)
}

func (q *priorityQueue[K, V]) Pop() interface{} {
	old := *q
	n := len(old)
	entry := old[n-1]
	old[n-1] = nil   // avoid memory leak
	entry.index = -1 // for safety
	new := old[0 : n-1]
	for i := 0; i < len(new); i++ {
		new[i].index = i
	}
	*q = new
	return entry
}

func (q *priorityQueue[K, V]) update(e *entry[K, V], val V) {
	e.val = val
	e.referenced()
	heap.Fix(q, e.index)
}
