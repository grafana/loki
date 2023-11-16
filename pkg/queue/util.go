package queue

import "github.com/prometheus/prometheus/util/pool"

// BufferPool uses a bucket pool and wraps the Get() and Put() functions for
// simpler access.
type BufferPool[T any] struct {
	p *pool.Pool
}

func NewBufferPool[T any](minSize, maxSize int, factor float64) *BufferPool[T] {
	return &BufferPool[T]{
		p: pool.New(minSize, maxSize, factor, func(i int) interface{} {
			return make([]T, 0, i)
		}),
	}
}

func (p *BufferPool[T]) Get(n int) []T {
	return p.p.Get(n).([]T)
}

func (p *BufferPool[T]) Put(buf []T) {
	p.p.Put(buf[0:0])
}
