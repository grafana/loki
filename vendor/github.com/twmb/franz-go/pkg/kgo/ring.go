package kgo

import (
	"sync"
)

// The ring type below in based on fixed sized blocking MPSC ringbuffer
// if the number of elements is less than or equal to 8, and fallback to a slice if
// the number of elements in greater than 8. The maximum number of elements
// in the ring is unlimited.
//
// This ring replace channels in a few places in this client. The *main* advantage it
// provide is to allow loops that terminate.
//
// With channels, we always have to have a goroutine draining the channel.  We
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
// We could exclusively use a slice that we always push to and pop the front of.
// This is a bit easier to reason about, but constantly reallocates and has no bounded
// capacity, so we use it only if the number of elements is greater than 8.
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
//
// We use size 8 buffers because eh why not. This gives us a small optimization
// of masking to increment and decrement, rather than modulo arithmetic.

const (
	mask7 = 0b0000_0111
	eight = mask7 + 1
)

type ring[T any] struct {
	mu sync.Mutex

	elems [eight]T

	head uint8
	tail uint8
	l    uint8
	dead bool

	overflow []T
}

func (r *ring[T]) die() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.dead = true
}

func (r *ring[T]) push(elem T) (first, dead bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.dead {
		return false, true
	}

	// If the ring is full, we go into overflow; if overflow is non-empty,
	// for ordering purposes, we add to the end of overflow. We only go
	// back to using the ring once overflow is finally empty.
	if r.l == eight || len(r.overflow) > 0 {
		r.overflow = append(r.overflow, elem)
		return false, false
	}

	r.elems[r.tail] = elem
	r.tail = (r.tail + 1) & mask7
	r.l++

	return r.l == 1, false
}

func (r *ring[T]) dropPeek() (next T, more, dead bool) {
	var zero T

	r.mu.Lock()
	defer r.mu.Unlock()

	// We always drain the ring first. If the ring is ever empty, there
	// must be overflow: we would not be here if the ring is not-empty.
	if r.l > 1 {
		r.elems[r.head] = zero
		r.head = (r.head + 1) & mask7
		r.l--
		return r.elems[r.head], true, r.dead
	} else if r.l == 1 {
		r.elems[r.head] = zero
		r.head = (r.head + 1) & mask7
		r.l--
		if len(r.overflow) == 0 {
			return next, false, r.dead
		}
		return r.overflow[0], true, r.dead
	}

	r.overflow[0] = zero

	// In case of continuous push and pulls to the overflow slice, the overflow
	// slice's underlying memory array is not expected to grow indefinitely because
	// append() will eventually re-allocate the memory and, when will do it, it will
	// only copy the "live" elements (the part of the slide pointed by the slice header).
	r.overflow = r.overflow[1:]

	if len(r.overflow) > 0 {
		return r.overflow[0], true, r.dead
	}

	// We have no more overflow elements. We reset the slice to nil to release memory.
	r.overflow = nil

	return next, false, r.dead
}
