package pool

import (
	"bytes"
	"sync"
)

// BufferPool is a bucketed pool for variably bytes buffers.
type BufferPool struct {
	buckets []sync.Pool
	sizes   []int
}

// NewBuffer a new Pool with size buckets for minSize to maxSize
// increasing by the given factor.
func NewBuffer(minSize, maxSize int, factor float64) *BufferPool {
	if minSize < 1 {
		panic("invalid minimum pool size")
	}
	if maxSize < 1 {
		panic("invalid maximum pool size")
	}
	if factor < 1 {
		panic("invalid factor")
	}

	var sizes []int

	for s := minSize; s <= maxSize; s = int(float64(s) * factor) {
		sizes = append(sizes, s)
	}

	return &BufferPool{
		buckets: make([]sync.Pool, len(sizes)),
		sizes:   sizes,
	}
}

// Get returns a byte buffer that fits the given size.
func (p *BufferPool) Get(sz int) *bytes.Buffer {
	for i, bktSize := range p.sizes {
		if sz > bktSize {
			continue
		}
		b := p.buckets[i].Get()
		if b == nil {
			b = bytes.NewBuffer(make([]byte, 0, bktSize))
		}
		buf := b.(*bytes.Buffer)
		buf.Reset()
		return b.(*bytes.Buffer)
	}
	return bytes.NewBuffer(make([]byte, 0, sz))
}

// Put adds a byte buffer to the right bucket in the pool.
func (p *BufferPool) Put(s *bytes.Buffer) {
	if s == nil {
		return
	}
	capt := s.Cap()
	for i, size := range p.sizes {
		if capt > size {
			continue
		}
		p.buckets[i].Put(s)
		return
	}
}
