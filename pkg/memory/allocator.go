package memory

import (
	"unsafe"

	"github.com/grafana/loki/v3/pkg/memory/internal/memalign"
)

// Allocator is an arena-style memory allocator that manages a set of memory
// regions.
//
// The Allocator can be reset to reclaim memory regions, marking them as free
// for future calls to [Allocator.Allocate].
//
// Allocator is unsafe and provides no use-after-free checks for [Region].
// Callers must take care to ensure that the lifetime of a returned Memory does
// not exceed the lifetime of an allocator reuse cycle.
//
// The zero value of Allocator is ready for use.
type Allocator struct {
	parent *Allocator

	regions []*Region
	free    Bitmap // Tracks free regions. 1=free, 0=used
	empty   Bitmap // Tracks nil elements of regions. 1=nil, 0=not-nil
}

// MakeAllocator creates a new Allocator with the given parent. If parent is
// nil, the returned allocator is a root allocator.
//
// Child allocators will obtain memory from their parent and can manage its own
// Trim and Reclaim lifecycle. All memory allocated from a child is invalidated
// when any of its parents (up to the root) reclaims memory.
func MakeAllocator(parent *Allocator) *Allocator {
	return &Allocator{parent: parent}
}

// Allocate retrieves the next free Memory region that can hold at least size
// bytes. If there is no such free Memory region, a new memory region will be
// created.
func (alloc *Allocator) Allocate(size int) *Region {
	// Iterate over the set bits in the freelist. Each set bit indicates an
	// available memory region.
	for i := range alloc.free.IterValues(true) {
		region := alloc.regions[i]
		if region != nil && cap(region.data) >= size {
			alloc.free.Set(i, false)
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

	alloc.free.Resize(len(alloc.regions))
	alloc.empty.Resize(len(alloc.regions))

	if free {
		alloc.free.Set(freeSlot, true)
	} else {
		alloc.free.Set(freeSlot, false)
	}

	alloc.empty.Set(freeSlot, false)
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

// Trim releases unused memory regions.
// If the allocator has a prent, released memory regions are returned to the parent allocator.
// Otherwise, released memory regions may be returned to the Go runtime for garbage collection.
//
// If Trim is called after Reclaim, all memory regions will be released.
func (alloc *Allocator) Trim() {
	for i := range alloc.free.IterValues(true) {
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
			alloc.free.Set(i, true)
			break
		}
	}
}

// Reclaim all memory regions back to the Allocator for reuse. After calling
// Reclaim, any [Region] returned by the Allocator or any child allocators must no longer be used.
func (alloc *Allocator) Reclaim() {
	alloc.free.SetRange(0, len(alloc.regions), true)
}

// AllocatedBytes returns the total amount of bytes owned by the Allocator.
func (alloc *Allocator) AllocatedBytes() int {
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
	var sum int
	for i := range alloc.free.IterValues(true) {
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
