package pool

import (
	"fmt"
	"reflect"
	"sync"
)

// NOTE this is pretty much a copy of prometheus pool

// Pool is a bucketed pool for variably sized byte slices.
type Pool struct {
	buckets []sync.Pool
	sizes   []int
	// make is the function used to create an empty slice when none exist yet.
	make func(int) interface{}
}

// New returns a new Pool with size buckets for minSize to maxSize
// increasing by the given factor.
func New(minSize, maxSize int, factor float64, makeFunc func(int) interface{}) *Pool {
	if minSize < 1 {
		panic("invalid minimum pool size")
	}
	if maxSize < 1 {
		panic("invalid maximum pool size")
	}
	if factor < 2 {
		panic("invalid factor")
	}

	var sizes []int

	for s := minSize; s <= maxSize; s = int(float64(s) * factor) {
		sizes = append(sizes, s)
	}

	p := &Pool{
		buckets: make([]sync.Pool, len(sizes)),
		sizes:   sizes,
		make:    makeFunc,
	}

	return p
}

func NewWithSizes(sizes []int, makeFunc func(int) interface{}) *Pool {
	if len(sizes) < 1 {
		panic("invalid pool sizes")
	}

	p := &Pool{
		buckets: make([]sync.Pool, len(sizes)),
		sizes:   sizes,
		make:    makeFunc,
	}

	return p
}

// Get returns a new byte slices that fits the given size.
func (p *Pool) Get(sz int) interface{} {
	for i, bktSize := range p.sizes {
		if sz > bktSize {
			continue
		}
		b := p.buckets[i].Get()
		if b == nil {
			b = p.make(bktSize)
		}
		return b
	}
	return p.make(sz)
}

// Put adds a slice to the right bucket in the pool.
func (p *Pool) Put(s interface{}) {
	slice := reflect.ValueOf(s)

	if slice.Kind() != reflect.Slice {
		panic(fmt.Sprintf("%+v is not a slice", slice))
	}
	for i, size := range p.sizes {
		if slice.Cap() > size {
			continue
		}
		p.buckets[i].Put(slice.Slice(0, 0).Interface())
		return
	}
}
