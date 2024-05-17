package mempool

import (
	"fmt"
	"unsafe"
)

type Pool interface {
	Get(size int) []byte
	Put(buffer []byte)
}

type slab struct {
	buffer      chan unsafe.Pointer
	size, count int
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

func (s *slab) get(size int) []byte {
	// TODO(chaudum): Remove debug line
	// fmt.Fprintf(os.Stderr, "get %d bytes in slab %d\n", size, s.size)

	if s.buffer == nil {
		s.init()
	}

	// wait for available buffer on channel
	ptr := <-s.buffer
	buf := unsafe.Slice((*byte)(ptr), s.size)

	// Taken from https://github.com/ortuman/nuke/blob/main/monotonic_arena.go#L37-L48
	// This piece of code will be translated into a runtime.memclrNoHeapPointers
	// invocation by the compiler, which is an assembler optimized implementation.
	// Architecture specific code can be found at src/runtime/memclr_$GOARCH.s
	// in Go source (since https://codereview.appspot.com/137880043).
	for i := range buf {
		buf[i] = 0
	}

	return buf[:size]
}

func (s *slab) put(buf []byte) {
	// TODO(chaudum): Remove debug line
	// fmt.Fprintf(os.Stderr, "put %d/%d bytes in slab %d\n", len(buf), cap(buf), s.size)

	if s.buffer == nil {
		panic("slab is not initialized")
	}

	ptr := unsafe.Pointer(unsafe.SliceData(buf))
	s.buffer <- ptr
}

type mempool struct {
	slabs []*slab
}

func New(buckets []Bucket) Pool {
	a := &mempool{
		slabs: make([]*slab, 0, len(buckets)),
	}
	for _, b := range buckets {
		a.slabs = append(a.slabs, newSlab(b.Capacity, b.Size))
	}
	return a
}

// Get satisfies Pool interface
func (a *mempool) Get(size int) []byte {
	for i := 0; i < len(a.slabs); i++ {
		if a.slabs[i].size < size {
			continue
		}
		return a.slabs[i].get(size)
	}
	panic(fmt.Sprintf("no slab found for size: %d", size))
}

// Put satisfies Pool interface
func (a *mempool) Put(buffer []byte) {
	size := cap(buffer)
	for i := 0; i < len(a.slabs); i++ {
		if a.slabs[i].size < size {
			continue
		}
		a.slabs[i].put(buffer)
		return
	}
}
