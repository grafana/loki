package mempool

import (
	"github.com/prometheus/prometheus/util/pool"
)

// Allocator handles byte slices for bloom queriers.
// It exists to reduce the cost of allocations and allows to re-use already allocated memory.
type Allocator interface {
	Get(size int) ([]byte, error)
	Put([]byte) bool
}

// SimpleHeapAllocator allocates a new byte slice every time and does not re-cycle buffers.
type SimpleHeapAllocator struct{}

func (a *SimpleHeapAllocator) Get(size int) ([]byte, error) {
	return make([]byte, size), nil
}

func (a *SimpleHeapAllocator) Put([]byte) bool {
	return true
}

// BytePool uses a sync.Pool to re-cycle already allocated buffers.
type BytePool struct {
	pool *pool.Pool
}

func NewBytePoolAllocator(minSize, maxSize int, factor float64) *BytePool {
	return &BytePool{
		pool: pool.New(
			minSize, maxSize, factor,
			func(size int) interface{} {
				return make([]byte, size)
			}),
	}
}

// Get implements Allocator
func (p *BytePool) Get(size int) ([]byte, error) {
	return p.pool.Get(size).([]byte)[:size], nil
}

// Put implements Allocator
func (p *BytePool) Put(b []byte) bool {
	p.pool.Put(b)
	return true
}
