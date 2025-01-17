package kgo

import "sync"

// The ring types below are fixed sized blocking MPSC ringbuffers. These
// replace channels in a few places in this client. The *main* advantage they
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
// We could use a slice that we always push to and pop the front of. This is a
// bit easier to reason about, but constantly reallocates and has no bounded
// capacity. The second we think about adding bounded capacity, we get this
// ringbuffer below.
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

type ringReq struct {
	mu sync.Mutex
	c  *sync.Cond

	elems [eight]promisedReq

	head uint8
	tail uint8
	l    uint8
	dead bool
}

func (r *ringReq) die() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.dead = true
	if r.c != nil {
		r.c.Broadcast()
	}
}

func (r *ringReq) push(pr promisedReq) (first, dead bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for r.l == eight && !r.dead {
		if r.c == nil {
			r.c = sync.NewCond(&r.mu)
		}
		r.c.Wait()
	}

	if r.dead {
		return false, true
	}

	r.elems[r.tail] = pr
	r.tail = (r.tail + 1) & mask7
	r.l++

	return r.l == 1, false
}

func (r *ringReq) dropPeek() (next promisedReq, more, dead bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.elems[r.head] = promisedReq{}
	r.head = (r.head + 1) & mask7
	r.l--

	// If the cond has been initialized, there could potentially be waiters
	// and we must always signal.
	if r.c != nil {
		r.c.Signal()
	}

	return r.elems[r.head], r.l > 0, r.dead
}

// ringResp duplicates the code above, but for promisedResp
type ringResp struct {
	mu sync.Mutex
	c  *sync.Cond

	elems [eight]promisedResp

	head uint8
	tail uint8
	l    uint8
	dead bool
}

func (r *ringResp) die() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.dead = true
	if r.c != nil {
		r.c.Broadcast()
	}
}

func (r *ringResp) push(pr promisedResp) (first, dead bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for r.l == eight && !r.dead {
		if r.c == nil {
			r.c = sync.NewCond(&r.mu)
		}
		r.c.Wait()
	}

	if r.dead {
		return false, true
	}

	r.elems[r.tail] = pr
	r.tail = (r.tail + 1) & mask7
	r.l++

	return r.l == 1, false
}

func (r *ringResp) dropPeek() (next promisedResp, more, dead bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.elems[r.head] = promisedResp{}
	r.head = (r.head + 1) & mask7
	r.l--

	if r.c != nil {
		r.c.Signal()
	}

	return r.elems[r.head], r.l > 0, r.dead
}

// ringSeqResp duplicates the code above, but for *seqResp. We leave off die
// because we do not use it, but we keep `c` for testing lowering eight/mask7.
type ringSeqResp struct {
	mu sync.Mutex
	c  *sync.Cond

	elems [eight]*seqResp

	head uint8
	tail uint8
	l    uint8
}

func (r *ringSeqResp) push(sr *seqResp) (first bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for r.l == eight {
		if r.c == nil {
			r.c = sync.NewCond(&r.mu)
		}
		r.c.Wait()
	}

	r.elems[r.tail] = sr
	r.tail = (r.tail + 1) & mask7
	r.l++

	return r.l == 1
}

func (r *ringSeqResp) dropPeek() (next *seqResp, more bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.elems[r.head] = nil
	r.head = (r.head + 1) & mask7
	r.l--

	if r.c != nil {
		r.c.Signal()
	}

	return r.elems[r.head], r.l > 0
}

// Also no die; this type is slightly different because we can have overflow.
// If we have overflow, we add to overflow until overflow is drained -- we
// always want strict odering.
type ringBatchPromise struct {
	mu sync.Mutex

	elems [eight]batchPromise

	head uint8
	tail uint8
	l    uint8

	overflow []batchPromise
}

func (r *ringBatchPromise) push(b batchPromise) (first bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If the ring is full, we go into overflow; if overflow is non-empty,
	// for ordering purposes, we add to the end of overflow. We only go
	// back to using the ring once overflow is finally empty.
	if r.l == eight || len(r.overflow) > 0 {
		r.overflow = append(r.overflow, b)
		return false
	}

	r.elems[r.tail] = b
	r.tail = (r.tail + 1) & mask7
	r.l++

	return r.l == 1
}

func (r *ringBatchPromise) dropPeek() (next batchPromise, more bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// We always drain the ring first. If the ring is ever empty, there
	// must be overflow: we would not be here if the ring is not-empty.
	if r.l > 1 {
		r.elems[r.head] = batchPromise{}
		r.head = (r.head + 1) & mask7
		r.l--
		return r.elems[r.head], true
	} else if r.l == 1 {
		r.elems[r.head] = batchPromise{}
		r.head = (r.head + 1) & mask7
		r.l--
		if len(r.overflow) == 0 {
			return next, false
		}
		return r.overflow[0], true
	}
	r.overflow = r.overflow[1:]
	if len(r.overflow) > 0 {
		return r.overflow[0], true
	}
	return next, false
}
