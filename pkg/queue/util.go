package queue

import "github.com/prometheus/prometheus/util/pool"

// SlicePool uses a bucket pool and wraps the Get() and Put() functions for
// simpler access.
type SlicePool[T any] struct {
	p *pool.Pool
}

func NewSlicePool[T any](minSize, maxSize int, factor float64) *SlicePool[T] {
	return &SlicePool[T]{
		p: pool.New(minSize, maxSize, factor, func(i int) interface{} {
			return make([]T, 0, i)
		}),
	}
}

func (sp *SlicePool[T]) Get(n int) []T {
	return sp.p.Get(n).([]T)
}

func (sp *SlicePool[T]) Put(buf []T) {
	sp.p.Put(buf[0:0])
}
