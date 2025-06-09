// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package staleness // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/staleness"

import (
	"container/heap"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/identity"
)

// PriorityQueue represents a way to store entries sorted by their priority.
// Pop() will return the oldest entry of the set.
type PriorityQueue interface {
	// Update will add or update an entry, and reshuffle the queue internally as needed to keep it sorted
	Update(id identity.Stream, newPrio time.Time)
	// Peek will return the entry at the HEAD of the queue *without* removing it from the queue
	Peek() (identity.Stream, time.Time)
	// Pop will remove the entry at the HEAD of the queue and return it
	Pop() (identity.Stream, time.Time)
	// Len will return the number of entries in the queue
	Len() int
}

// heapQueue implements heap.Interface.
// We use it as the inner implementation of a heap-based sorted queue
type heapQueue []*queueItem

type queueItem struct {
	key   identity.Stream
	prio  time.Time
	index int
}

func (pq heapQueue) Len() int { return len(pq) }

func (pq heapQueue) Less(i, j int) bool {
	// We want Pop to give us the lowest priority
	return pq[i].prio.Before(pq[j].prio)
}

func (pq heapQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *heapQueue) Push(x any) {
	n := len(*pq)
	item := x.(*queueItem)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *heapQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.index = -1 // for safety
	*pq = old[0 : n-1]
	return item
}

type heapPriorityQueue struct {
	inner      heapQueue
	itemLookup map[identity.Stream]*queueItem
}

func NewPriorityQueue() PriorityQueue {
	pq := &heapPriorityQueue{
		inner:      heapQueue{},
		itemLookup: map[identity.Stream]*queueItem{},
	}
	heap.Init(&pq.inner)

	return pq
}

func (pq *heapPriorityQueue) Update(id identity.Stream, newPrio time.Time) {
	// Check if the entry already exists in the queue
	item, ok := pq.itemLookup[id]
	if ok {
		// If so, we can update it in place
		item.prio = newPrio
		heap.Fix(&pq.inner, item.index)
	} else {
		item = &queueItem{
			key:  id,
			prio: newPrio,
		}
		heap.Push(&pq.inner, item)
		pq.itemLookup[id] = item
	}
}

func (pq *heapPriorityQueue) Peek() (identity.Stream, time.Time) {
	val := pq.inner[0]
	return val.key, val.prio
}

func (pq *heapPriorityQueue) Pop() (identity.Stream, time.Time) {
	val := heap.Pop(&pq.inner).(*queueItem)
	delete(pq.itemLookup, val.key)
	return val.key, val.prio
}

func (pq *heapPriorityQueue) Len() int {
	return pq.inner.Len()
}
