package chunkenc

import (
	"fmt"
	"sync"

	"golang.org/x/exp/slices"
)

// TODO comment
type PoolAllocator struct {
	pool    *sync.Pool
	maxSize int
}

func (p *PoolAllocator) Get(sz int) *[]byte {
	// TODO: resize instead
	//if sz > p.maxSize {
	//	panic(fmt.Sprintf("cannot allocate byte slice of %d bytes which is larger than the defined maximum size of %d", sz, p.maxSize))
	//}

	var sl *[]byte
	sl = p.pool.Get().(*[]byte)
	//// clear the retrieved item, retain underlying memory
	if cap(*sl) < sz {
		*sl = slices.Grow(*sl, sz)
	}

	clear(*sl)
	*sl = (*sl)[:0:sz]
	return sl
}

func (p *PoolAllocator) Put(b *[]byte) {
	fmt.Printf("recycling %d bytes\n", len(*b))

	p.pool.Put(b)
}

func NewPoolAllocator(maxSize int) *PoolAllocator {
	return &PoolAllocator{
		maxSize: maxSize,
		pool: &sync.Pool{
			New: func() any {
				fmt.Println("allocating...")
				bytes := make([]byte, 0, 10000)
				return &bytes
			},
		},
	}
}
