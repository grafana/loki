package mempool

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

var (
	errSlabExhausted = errors.New("slab exhausted")
)

type slab struct {
	buffer      chan unsafe.Pointer
	size, count int
	mtx         sync.Mutex
}

func newSlab(bufferSize, bufferCount int) *slab {
	return &slab{
		size:  bufferSize,
		count: bufferCount,
	}
}

func (s *slab) init() {
	s.buffer = make(chan unsafe.Pointer, s.count)
	for i := 0; i < s.count; i++ {
		buf := make([]byte, 0, s.size)
		ptr := unsafe.Pointer(unsafe.SliceData(buf))
		s.buffer <- ptr
	}
}

func (s *slab) get(size int) ([]byte, error) {
	s.mtx.Lock()
	if s.buffer == nil {
		s.init()
	}
	defer s.mtx.Unlock()

	// wait for available buffer on channel
	var buf []byte
	select {
	case ptr := <-s.buffer:
		buf = unsafe.Slice((*byte)(ptr), s.size)
	default:
		return nil, errSlabExhausted
	}

	// Taken from https://github.com/ortuman/nuke/blob/main/monotonic_arena.go#L37-L48
	// This piece of code will be translated into a runtime.memclrNoHeapPointers
	// invocation by the compiler, which is an assembler optimized implementation.
	// Architecture specific code can be found at src/runtime/memclr_$GOARCH.s
	// in Go source (since https://codereview.appspot.com/137880043).
	for i := range buf {
		buf[i] = 0
	}

	return buf[:size], nil
}

func (s *slab) put(buf []byte) {
	if s.buffer == nil {
		panic("slab is not initialized")
	}

	ptr := unsafe.Pointer(unsafe.SliceData(buf))
	s.buffer <- ptr
}

// MemPool is an Allocator implementation that uses a fixed size memory pool
// that is split into multiple slabs of different buffer sizes.
// Buffers are re-cycled and need to be returned back to the pool, otherwise
// the pool runs out of available buffers.
type MemPool struct {
	slabs []*slab
}

func New(buckets []Bucket) *MemPool {
	a := &MemPool{
		slabs: make([]*slab, 0, len(buckets)),
	}
	for _, b := range buckets {
		a.slabs = append(a.slabs, newSlab(b.Capacity, b.Size))
	}
	return a
}

// Get satisfies Allocator interface
// Allocating a buffer from an exhausted pool/slab will return an error.
// Allocating a buffer that exceeds the largest slab size will cause a panic.
func (a *MemPool) Get(size int) ([]byte, error) {
	for i := 0; i < len(a.slabs); i++ {
		if a.slabs[i].size < size {
			continue
		}
		return a.slabs[i].get(size)
	}
	panic(fmt.Sprintf("no slab found for size: %d", size))
}

// Put satisfies Allocator interface
// Every buffer allocated with Get(size int) needs to be returned to the pool
// using Put(buffer []byte) so it can be re-cycled.
func (a *MemPool) Put(buffer []byte) bool {
	size := cap(buffer)
	for i := 0; i < len(a.slabs); i++ {
		if a.slabs[i].size < size {
			continue
		}
		a.slabs[i].put(buffer)
		return true
	}
	return false
}
