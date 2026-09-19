package memory

import (
	"errors"
	"unsafe"

	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/memory/internal/memalign"
)

var errConcurrentUse = errors.New("detected concurrent use of allocator")

// Allocator is an arena-style memory allocator that manages a set of memory
// regions.
//
// An Allocator must not be copied after first use.
//
// The Allocator can be reset to reclaim memory regions, marking them as free
// for future calls to [Allocator.Allocate].
//
// Allocator is unsafe and provides no use-after-free checks for [Region].
// Callers must take care to ensure that the lifetime of a returned Memory does
// not exceed the lifetime of an allocator reuse cycle.
//
// Allocators are not goroutine safe. If an Allocator methods are called
// concurrently, the method will panic.
type Allocator struct {
	// locked is set to true when the allocator is in use.
	locked atomic.Bool

	parent *Allocator

	regions []*Region
	avail   Bitmap // Tracks available regions. 1=available, 0=in-use
	used    Bitmap // Tracks regions used since the previous Reclaim. 1=used, 0=unused
	empty   Bitmap // Tracks nil elements of regions. 1=nil, 0=not-nil
}

// NewAllocator creates a new Allocator with the given parent. If parent is
// nil, the returned allocator is a root allocator.
//
// Child allocators will obtain memory from their parent and can manage its own
// Trim and Reclaim lifecycle. All memory allocated from a child is invalidated
// when any of its parents (up to the root) reclaims memory.
func NewAllocator(parent *Allocator) *Allocator {
	return &Allocator{parent: parent}
}

// Allocate retrieves the next free Memory region that can hold at least size
// bytes. If there is no such free Memory region, a new memory region will be
// created.
func (alloc *Allocator) Allocate(size int) *Region {
	alloc.lock()
	defer alloc.unlock()

	// Iterate over the set bits in the freelist. Each set bit indicates an
	// available memory region.
	for i := range alloc.avail.IterValues(true) {
		region := alloc.regions[i]
		if region != nil && cap(region.data) >= size {
			alloc.avail.Set(i, false)
			alloc.used.Set(i, true)
			return region
		}
	}

	if alloc.parent != nil {
		// No memory in our pool, ask the parent allocator for a region, if we have one.
		region := alloc.parent.Allocate(size)
		alloc.addRegion(region, false)
		return region
	}

	// Otherwise, allocate a new region from the runtime.
	region := &Region{data: allocBytes(size)}
	alloc.addRegion(region, false) // Track the new region.
	return region
}

func (alloc *Allocator) lock() {
	if !alloc.locked.CompareAndSwap(false, true) {
		panic(errConcurrentUse)
	}
}

func (alloc *Allocator) unlock() {
	if !alloc.locked.CompareAndSwap(true, false) {
		panic(errConcurrentUse)
	}
}

// addRegion inserts a new region into alloc's list of regions, taking
// ownership over it.
func (alloc *Allocator) addRegion(region *Region, free bool) {
	// Elements in alloc.regions may be set to nil after [Allocator.Trim], so we
	// should check to see if there's a free slot before appending to the slice.
	freeSlot := -1
	for i := range alloc.empty.IterValues(true) {
		freeSlot = i
		break
	}

	if freeSlot == -1 {
		freeSlot = len(alloc.regions)
		alloc.regions = append(alloc.regions, region)
	} else {
		alloc.regions[freeSlot] = region
	}

	alloc.avail.Resize(len(alloc.regions))
	alloc.used.Resize(len(alloc.regions))
	alloc.empty.Resize(len(alloc.regions))

	alloc.avail.Set(freeSlot, free)
	alloc.used.Set(freeSlot, !free)  // Region is in-use if it's not free.
	alloc.empty.Set(freeSlot, false) // We just filled the slot.
}

// Reset resets the Allocator for reuse. It is a convenience wrapper for calling
// [Allocator.Trim] and [Allocator.Reclaim] (in that order).
//
// Advanced use cases may wish to selectively call Trim depending on the values
// of [Allocator.AllocatedBytes] and [Allocator.FreeBytes].
func (alloc *Allocator) Reset() {
	alloc.Trim()
	alloc.Reclaim()
}

// Trim releases unused memory regions. If the allocator has a parent, released
// memory regions are returned to the parent allocator. Otherwise, released
// memory regions may be returned to the Go runtime for garbage collection.
//
// If Trim is called after Reclaim, all memory regions will be released.
func (alloc *Allocator) Trim() {
	alloc.lock()
	defer alloc.unlock()

	for i := range alloc.used.IterValues(false) {
		region := alloc.regions[i]
		if region == nil {
			continue
		}

		alloc.regions[i] = nil
		alloc.empty.Set(i, true)

		if alloc.parent != nil {
			// Return the region to the parent allocator, if there is one.
			alloc.parent.returnRegion(region)
		}
	}
}

func (alloc *Allocator) returnRegion(region *Region) {
	for i := range alloc.regions {
		if alloc.regions[i] == region {
			alloc.avail.Set(i, true)
			break
		}
	}
}

// Free returns all memory regions back to the parent allocator, if there is
// one. Otherwise, released memory regions are returned to the Go runtime for
// garbage collection.
//
// It is a convenience wrapper for calling [Allocator.Reclaim] and
// [Allocator.Trim] (in that order).
func (alloc *Allocator) Free() {
	alloc.Reclaim()
	alloc.Trim()
}

// Reclaim all memory regions back to the Allocator for reuse. After calling
// Reclaim, any [Region] returned by the Allocator or any child allocators must no longer be used.
func (alloc *Allocator) Reclaim() {
	alloc.lock()
	defer alloc.unlock()

	alloc.avail.SetRange(0, len(alloc.regions), true)
	alloc.used.SetRange(0, len(alloc.regions), false)
}

// AllocatedBytes returns the total amount of bytes owned by the Allocator.
func (alloc *Allocator) AllocatedBytes() int {
	alloc.lock()
	defer alloc.unlock()

	var sum int
	for _, region := range alloc.regions {
		if region == nil {
			continue
		}
		sum += cap(region.data)
	}
	return sum
}

// FreeBytes returns the total amount of bytes available for Memory to use
// without requiring additional allocations.
func (alloc *Allocator) FreeBytes() int {
	alloc.lock()
	defer alloc.unlock()

	var sum int
	for i := range alloc.avail.IterValues(true) {
		region := alloc.regions[i]
		if region != nil {
			sum += cap(region.data)
		}
	}
	return sum
}

// allocBytes allocates a new slice which can hold up to at least size bytes. The
// returned slice will be 64-byte aligned for cache line efficiency.
func allocBytes(size int) []byte {
	const alignmentPadding = 64

	buf := make([]byte, size+alignmentPadding)

	addr := uint64(uintptr(unsafe.Pointer(&buf[0])))
	alignedAddr := memalign.Align64(addr)

	if alignedAddr != addr {
		offset := int(alignedAddr - addr)
		return buf[offset : offset+size : offset+size]
	}
	return buf[:size:size]
}
