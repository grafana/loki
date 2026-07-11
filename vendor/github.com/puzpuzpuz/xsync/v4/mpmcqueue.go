package xsync

import (
	"math/bits"
	"sync/atomic"
	"unsafe"
)

const mpmcQueueMaxRequestedCapacity uint64 = uint64(1) << (bits.UintSize - 2)

// Deprecated: use [MPMCQueue].
type MPMCQueueOf[I any] = MPMCQueue[I]

// A MPMCQueue is a bounded multi-producer multi-consumer concurrent
// queue.
//
// MPMCQueue instances must be created with NewMPMCQueue function.
// A MPMCQueue must not be copied after first use.
//
// Based on the data structure from the following C++ library:
// https://github.com/rigtorp/MPMCQueue
type MPMCQueue[I any] struct {
	capMask  uint64
	capShift uint64
	// Padding to isolate read-only fields from the head/tail counters.
	_     [cacheLineSize - 16]byte
	head  uint64
	_     [cacheLineSize - 8]byte
	tail  uint64
	_     [cacheLineSize - 8]byte
	slots []slotPadded[I]
}

type slotPadded[I any] struct {
	slot[I]
	// Unfortunately, proper padding like the below one:
	//
	// pad [cacheLineSize - (unsafe.Sizeof(slot[I]{}) % cacheLineSize)]byte
	//
	// won't compile, so here we add a best-effort padding for items up to
	// 56 bytes size.
	_ [cacheLineSize - unsafe.Sizeof(atomic.Uint64{})]byte
}

type slot[I any] struct {
	// atomic.Uint64 is used here to get proper 8 byte alignment on
	// 32-bit archs.
	turn atomic.Uint64
	item I
}

// Deprecated: use [NewMPMCQueue].
func NewMPMCQueueOf[I any](capacity int) *MPMCQueue[I] {
	return NewMPMCQueue[I](capacity)
}

// NewMPMCQueue creates a new MPMCQueue instance with the given
// capacity. The capacity is rounded up to the next power of 2.
func NewMPMCQueue[I any](capacity int) *MPMCQueue[I] {
	if capacity < 1 {
		panic("capacity must be positive number")
	}
	if uint64(capacity) > mpmcQueueMaxRequestedCapacity {
		panic("capacity is too large")
	}
	capPow2 := nextPowOf2(uint64(capacity))
	return &MPMCQueue[I]{
		capMask:  capPow2 - 1,
		capShift: uint64(bits.TrailingZeros64(capPow2)),
		slots:    make([]slotPadded[I], capPow2),
	}
}

// TryEnqueue inserts the given item into the queue. Does not block
// and returns immediately. The result indicates that the queue isn't
// full and the item was inserted.
func (q *MPMCQueue[I]) TryEnqueue(item I) bool {
	head := atomic.LoadUint64(&q.head)
	slot := &q.slots[q.idx(head)]
	turn := q.turn(head) * 2
	if slot.turn.Load() == turn {
		if atomic.CompareAndSwapUint64(&q.head, head, head+1) {
			slot.item = item
			slot.turn.Store(turn + 1)
			return true
		}
	}
	return false
}

// TryDequeue retrieves and removes the item from the head of the
// queue. Does not block and returns immediately. The ok result
// indicates that the queue isn't empty and an item was retrieved.
func (q *MPMCQueue[I]) TryDequeue() (item I, ok bool) {
	tail := atomic.LoadUint64(&q.tail)
	slot := &q.slots[q.idx(tail)]
	turn := q.turn(tail)*2 + 1
	if slot.turn.Load() == turn {
		if atomic.CompareAndSwapUint64(&q.tail, tail, tail+1) {
			var zeroI I
			item = slot.item
			ok = true
			slot.item = zeroI
			slot.turn.Store(turn + 1)
			return
		}
	}
	return
}

func (q *MPMCQueue[I]) idx(i uint64) uint64 {
	return i & q.capMask
}

func (q *MPMCQueue[I]) turn(i uint64) uint64 {
	return i >> q.capShift
}
