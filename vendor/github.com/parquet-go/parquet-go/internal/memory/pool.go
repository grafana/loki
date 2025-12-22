package memory

import "sync"

// Pool is a generic wrapper around sync.Pool that provides type-safe pooling
// of pointers to values of type T.
type Pool[T any] struct {
	pool sync.Pool // *T
}

func (p *Pool[T]) Get(newT func() *T, resetT func(*T)) *T {
	v, _ := p.pool.Get().(*T)
	if v == nil {
		v = newT()
	} else {
		resetT(v)
	}
	return v
}

func (p *Pool[T]) Put(v *T) {
	if v != nil {
		p.pool.Put(v)
	}
}
