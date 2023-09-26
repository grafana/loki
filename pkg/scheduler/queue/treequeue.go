package queue

import (
	"fmt"
	"strings"
)

type QueuePath []string //nolint:revive

// TreeQueue is an hierarchical queue implementation where each sub-queue
// has the same guarantees to be chosen from.
// Each queue has also a local queue, which gets chosen with equal preference as the sub-queues.
type TreeQueue struct {
	// local queue
	ch RequestChannel
	// index of where this item is located in the mapping
	pos QueueIndex
	// index of the sub-queues
	current QueueIndex
	// mapping for sub-queues
	mapping *Mapping[*TreeQueue]
	// name of the queue
	name string
	// maximum queue size of the local queue
	size int
}

// newTreeQueue creates a new TreeQueue instance
func newTreeQueue(size int, name string) *TreeQueue {
	m := &Mapping[*TreeQueue]{}
	m.Init(64) // TODO(chaudum): What is a good initial value?
	return &TreeQueue{
		ch:      make(RequestChannel, size),
		pos:     StartIndexWithLocalQueue,
		current: StartIndexWithLocalQueue,
		mapping: m,
		name:    name,
		size:    size,
	}
}

// add recursively adds queues based on given path
func (q *TreeQueue) add(path QueuePath) *TreeQueue {
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

func (q *TreeQueue) getOrCreate(name string) (subq *TreeQueue, created bool) {
	subq = q.mapping.GetByKey(name)
	if subq == nil {
		subq = newTreeQueue(q.size, name)
		created = true
	}
	return subq, created
}

// Chan implements Queue
func (q *TreeQueue) Chan() RequestChannel {
	return q.ch
}

// Dequeue implements Queue
func (q *TreeQueue) Dequeue() Request {
	var item Request

	// shortcut of there are not sub-queues
	// always use local queue
	if q.mapping.Len() == 0 {
		if len(q.ch) > 0 {
			return <-q.ch
		}
		return nil
	}

	maxIter := len(q.mapping.keys) + 1
	for iters := 0; iters < maxIter; iters++ {
		if q.current == StartIndexWithLocalQueue {
			q.current++
			if len(q.ch) > 0 {
				item = <-q.ch
				if item != nil {
					return item
				}
			}
		}

		subq, err := q.mapping.GetNext(q.current)
		if err == ErrOutOfBounds {
			q.current = StartIndexWithLocalQueue
			continue
		}
		if subq != nil {
			q.current = subq.pos
			item := subq.Dequeue()
			if item != nil {
				if subq.Len() == 0 {
					q.mapping.Remove(subq.name)
				}
				return item
			}
		}
	}
	return nil
}

// Name implements Queue
func (q *TreeQueue) Name() string {
	return q.name
}

// Len implements Queue
// It returns the length of the local queue and all sub-queues.
// This may be expensive depending on the size of the queue tree.
func (q *TreeQueue) Len() int {
	count := len(q.ch)
	for _, subq := range q.mapping.Values() {
		count += subq.Len()
	}
	return count
}

// Index implements Mapable
func (q *TreeQueue) Pos() QueueIndex {
	return q.pos
}

// Index implements Mapable
func (q *TreeQueue) SetPos(index QueueIndex) {
	q.pos = index
}

// String makes the queue printable
func (q *TreeQueue) String() string {
	sb := &strings.Builder{}
	sb.WriteString("{")
	fmt.Fprintf(sb, "name=%s, len=%d/%d, children=[", q.Name(), q.Len(), cap(q.ch))
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
