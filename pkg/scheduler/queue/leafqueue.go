package queue

import (
	"fmt"
	"strings"
)

type QueuePath []string //nolint:revive

// LeafQueue is an hierarchical queue implementation where each sub-queue
// has the same guarantees to be chosen from.
// Each queue has also a local queue, which gets chosen from first. Only if the
// local queue is empty, items from the sub-queues are dequeued.
type LeafQueue struct {
	// local queue
	ch RequestChannel
	// index of where this item is located in the mapping
	pos QueueIndex
	// index of the sub-queues
	current QueueIndex
	// mapping for sub-queues
	mapping *Mapping[*LeafQueue]
	// name of the queue
	name string
	// maximum queue size of the local queue
	size int
}

// newLeafQueue creates a new LeafQueue instance
func newLeafQueue(size int, name string) *LeafQueue {
	m := &Mapping[*LeafQueue]{}
	m.Init(64) // TODO(chaudum): What is a good initial value?
	return &LeafQueue{
		ch:      make(RequestChannel, size),
		pos:     StartIndex,
		current: StartIndex,
		mapping: m,
		name:    name,
		size:    size,
	}
}

// add recursively adds queues based on given path
func (q *LeafQueue) add(path QueuePath) *LeafQueue {
	if len(path) == 0 {
		return q
	}
	curr, remaining := path[0], path[1:]
	queue, created := q.getOrCreate(curr)
	if created {
		q.mapping.Put(queue.Name(), queue)
	}
	return queue.add(remaining)
}

func (q *LeafQueue) getOrCreate(name string) (subq *LeafQueue, created bool) {
	subq = q.mapping.GetByKey(name)
	if subq == nil {
		subq = newLeafQueue(q.size, name)
		created = true
	}
	return subq, created
}

// Chan implements Queue
func (q *LeafQueue) Chan() RequestChannel {
	return q.ch
}

// Dequeue implements Queue
func (q *LeafQueue) Dequeue() Request {
	// first, return item from local channel
	if len(q.ch) > 0 {
		return <-q.ch
	}

	// only if there are no items queued in the local queue, dequeue from sub-queues
	maxIter := len(q.mapping.keys)
	for iters := 0; iters < maxIter; iters++ {
		subq := q.mapping.GetNext(q.current)
		if subq != nil {
			q.current = subq.pos
			item := subq.Dequeue()
			if item != nil {
				return item
			}
			q.mapping.Remove(subq.name)
		} else {
			q.current++
		}
	}
	return nil
}

// Name implements Queue
func (q *LeafQueue) Name() string {
	return q.name
}

// Len implements Queue
// It returns the length of the local queue and all sub-queues.
// This may be expensive depending on the size of the queue tree.
func (q *LeafQueue) Len() int {
	count := len(q.ch)
	for _, subq := range q.mapping.Values() {
		count += subq.Len()
	}
	return count
}

// Index implements Mapable
func (q *LeafQueue) Pos() QueueIndex {
	return q.pos
}

// Index implements Mapable
func (q *LeafQueue) SetPos(index QueueIndex) {
	q.pos = index
}

// String makes the queue printable
func (q *LeafQueue) String() string {
	sb := &strings.Builder{}
	sb.WriteString("{")
	fmt.Fprintf(sb, "name=%s, len=%d/%d, leafs=[", q.Name(), q.Len(), cap(q.ch))
	subqs := q.mapping.Values()
	for i, m := range subqs {
		sb.WriteString(m.String())
		if i < len(subqs)-1 {
			sb.WriteString(",")
		}
	}
	sb.WriteString("]")
	sb.WriteString("}")
	return sb.String()
}
