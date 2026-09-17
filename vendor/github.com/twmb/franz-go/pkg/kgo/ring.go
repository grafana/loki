package kgo

import (
	"sync"

	"github.com/twmb/franz-go/pkg/kgo/internal/xsync"
)

// The ring type is a dynamically-sized circular buffer with optional blocking
// when full. The buffer starts at capacity 8 and grows as needed. When the
// buffer empties, it shrinks back to capacity 8 to release memory.
//
// This ring replaces channels in a few places in this client. The *main*
// advantage it provides is to allow loops that terminate.
//
// With channels, we always have to have a goroutine draining the channel. We
// cannot start the goroutine when we add the first element, because the
// goroutine will immediately drain the first and if something produces right
// away, it will start a second concurrent draining goroutine.
//
// We cannot fix that by adding a "working" field, because we would need a lock
// around checking if the goroutine still has elements *and* around setting the
// working field to false. If a push was blocked, it would be holding the lock,
// which would block the worker from grabbing the lock. Any other lock ordering
// has TOCTOU problems as well.
//
// The key insight is that we only pop the front *after* we are done with it.
// If there are still more elements, the worker goroutine can continue working.
// If there are no more elements, it can quit. When pushing, if the pusher
// pushed the first element, it starts the worker.
//
// Pushes fail if the ring is dead, allowing the pusher to fail any promise.
// If a die happens while a worker is running, all future pops will see the
// ring is dead and can fail promises immediately. If a worker is not running,
// then there are no promises that need to be called.

const minRingCap = 8

type ring[T any] struct {
	mu xsync.Mutex

	elems []T // circular buffer, min capacity minRingCap
	head  int // index of first element
	l     int // number of elements

	maxLen int        // if >0, push blocks when l >= maxLen
	cond   *sync.Cond // used for blocking when at maxLen
	dead   bool
}

// initMaxLen sets the maximum number of elements before push blocks.
// This must be called before any concurrent access.
func (r *ring[T]) initMaxLen(max int) {
	r.maxLen = max
	r.cond = sync.NewCond(&r.mu)
}

func (r *ring[T]) die() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.dead = true
	if r.cond != nil {
		r.cond.Broadcast()
	}
}

func (r *ring[T]) push(elem T) (first, dead bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If a max length is set, block until there's space.
	for r.maxLen > 0 && r.l >= r.maxLen && !r.dead {
		r.cond.Wait()
	}

	if r.dead {
		return false, true
	}

	// Grow: double capacity when full (or initialize to minRingCap).
	if r.l == cap(r.elems) {
		r.resize(max(cap(r.elems)*2, minRingCap))
	}

	// Write at tail position (head + l, wrapped).
	writePos := (r.head + r.l) % cap(r.elems)
	r.elems[writePos] = elem
	r.l++

	return r.l == 1, false
}

// resize changes the buffer capacity, copying elements in linear order.
// Must be called with r.mu held.
func (r *ring[T]) resize(newCap int) {
	newElems := make([]T, newCap)
	if r.l > 0 {
		// Copy elements in order: from head to end, then from start to head.
		if r.head+r.l <= len(r.elems) {
			copy(newElems, r.elems[r.head:r.head+r.l])
		} else {
			n := copy(newElems, r.elems[r.head:])
			copy(newElems[n:], r.elems[:r.l-n])
		}
	}
	r.elems = newElems
	r.head = 0
}

func (r *ring[T]) dropPeek() (next T, more, dead bool) {
	var zero T

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.l == 0 {
		return zero, false, r.dead
	}

	// Clear current head element.
	r.elems[r.head] = zero
	r.head = (r.head + 1) % cap(r.elems)
	r.l--

	// Signal any blocked pushers that space is available.
	if r.cond != nil {
		r.cond.Signal()
	}

	// Shrink: reduce to minRingCap when mostly empty to release memory.
	if r.l <= minRingCap/2 && cap(r.elems) > minRingCap {
		r.resize(minRingCap)
	}

	if r.l > 0 {
		return r.elems[r.head], true, r.dead
	}
	return zero, false, r.dead
}
