package util

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/prometheus/prometheus/util/pool"
	"golang.org/x/exp/slices"
)

var Closed atomic.Bool

//var ChunkAllocator = NewPoolAllocator(2 << 10) // 2KiB
var ChunkAllocator = NewSlabAllocator()

// TODO comment
type PoolAllocator struct {
	pool    *sync.Pool
}

func (p *PoolAllocator) Get(sz int) *[]byte {
	var sl *[]byte
	sl = p.pool.Get().(*[]byte)

	// clear the retrieved item, retain underlying memory
	if cap(*sl) < sz {
		*sl = slices.Grow(*sl, sz)
	}

	*sl = (*sl)[:0:sz]

	return sl
}

func (p *PoolAllocator) Put(b *[]byte) {
	if *b == nil {
		return
	}

	fmt.Printf("recycling %d bytes\n", cap(*b))

	// TODO(dannyk): once we upgrade to go1.21, replace with clear()
	for i := range *b {
		(*b)[i] = 0
	}

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

type SlabAllocator struct {
	pool *pool.Pool
}

func (p *SlabAllocator) Get(sz int) *[]byte {
	sl := p.pool.Get(sz).(*[]byte)
	*sl = (*sl)[:0:sz]

	return sl
}

func (p *SlabAllocator) Put(b *[]byte) {
	if *b == nil {
		return
	}

	fmt.Printf("recycling %d bytes\n", cap(*b))

	// TODO(dannyk): once we upgrade to go1.21, replace with clear()
	for i := range *b {
		(*b)[i] = 0
	}

	p.pool.Put(b)
}

func NewSlabAllocator() *SlabAllocator {
	return &SlabAllocator{
		pool: pool.New(2 << 10, 2 << 20, 2, func(size int) any {
			fmt.Println("allocating...")
			bytes := make([]byte, 0, size)
			return &bytes
		}),
	}
}
