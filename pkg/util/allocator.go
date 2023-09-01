package util

import (
	"fmt"
	"sync"
	"sync/atomic"

	"golang.org/x/exp/slices"
)

var Closed atomic.Bool

var ChunkAllocator = NewPoolAllocator(2 << 10) // 2KiB

// TODO comment
type PoolAllocator struct {
	pool    *sync.Pool
	maxSize int
}

func (p *PoolAllocator) Get(sz int) *[]byte {
	var sl *[]byte
	sl = p.pool.Get().(*[]byte)

	// clear the retrieved item, retain underlying memory
	if cap(*sl) < sz {
		*sl = slices.Grow(*sl, sz)
	}

	// TODO(dannyk): once we upgrade to go1.21, replace with clear(*sl)
	//clear(*sl)

	for i := range *sl {
		(*sl)[i] = 1
	}

	*sl = (*sl)[:0:sz]

	return sl
}

func (p *PoolAllocator) Put(b *[]byte) {
	fmt.Printf("recycling %d bytes\n", cap(*b))

	p.pool.Put(b)
}

func NewPoolAllocator(initialSize int) *PoolAllocator {
	return &PoolAllocator{
		pool: &sync.Pool{
			New: func() any {
				fmt.Println("allocating...")
				bytes := make([]byte, 0, initialSize)
				return &bytes
			},
		},
	}
}
