package pool

import "sync"

// lookback is how many of the last slabs Get checks for free space before it takes a new slab.
const lookback = 3

// SlabPool hands out slices that it carves from larger slabs, so many small allocations
// share a few pooled buffers. Release returns the slabs to the shared pool.
//
// A SlabPool is for one task. Slices from Get are invalid after Release. It is safe for
// concurrent use.
type SlabPool struct {
	slabs    *sync.Pool
	slabSize int

	mtx  sync.Mutex
	used []*[]byte
}

// NewSlabPool returns a SlabPool that takes slabs of slabSize bytes from slabs. The pool
// must hold only *[]byte values. An empty pool makes new slabs.
func NewSlabPool(slabs *sync.Pool, slabSize int) *SlabPool {
	return &SlabPool{slabs: slabs, slabSize: slabSize}
}

// Get returns a slice of the given length. A size above the slab size is not pooled. The slice
// of a reused slab is not zeroed.
func (p *SlabPool) Get(size int) []byte {
	if size <= 0 {
		return nil
	}
	if size > p.slabSize {
		return make([]byte, size)
	}

	p.mtx.Lock()
	defer p.mtx.Unlock()

	var slab *[]byte
	for i := len(p.used) - 1; i >= max(0, len(p.used)-lookback); i-- {
		if cap(*p.used[i])-len(*p.used[i]) >= size {
			slab = p.used[i]
			break
		}
	}
	if slab == nil {
		if reused, ok := p.slabs.Get().(*[]byte); ok && cap(*reused) >= p.slabSize {
			slab = reused
			*slab = (*slab)[:0]
		} else {
			fresh := make([]byte, 0, p.slabSize)
			slab = &fresh
		}
		p.used = append(p.used, slab)
	}

	n := len(*slab)
	*slab = (*slab)[:n+size]
	return (*slab)[n : n+size : n+size]
}

// Release returns the slabs to the shared pool.
func (p *SlabPool) Release() {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	for _, slab := range p.used {
		p.slabs.Put(slab)
	}
	p.used = nil
}

// Allocator returns an allocator that takes its memory from the pool. Its Put does nothing,
// because Release frees everything at once.
func (p *SlabPool) Allocator() *SlabAllocator { return &SlabAllocator{pool: p} }

// SlabAllocator adapts a SlabPool to the allocator interface of the memcached client.
type SlabAllocator struct{ pool *SlabPool }

// Get returns memory from the pool.
func (a *SlabAllocator) Get(size int) *[]byte {
	b := a.pool.Get(size)
	return &b
}

// Put does nothing.
func (a *SlabAllocator) Put(*[]byte) {}
