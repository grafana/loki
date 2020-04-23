// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package pool

import (
	"sync"

	"github.com/pkg/errors"
)

type BytesPool interface {
	Get(sz int) (*[]byte, error)
	Put(b *[]byte)
}

// BucketedBytesPool is a bucketed pool for variably sized byte slices. It can be configured to not allow
// more than a maximum number of bytes being used at a given time.
// Every byte slice obtained from the pool must be returned.
type BucketedBytesPool struct {
	buckets   []sync.Pool
	sizes     []int
	maxTotal  uint64
	usedTotal uint64
	mtx       sync.Mutex

	new func(s int) *[]byte
}

// NewBytesPool returns a new BytesPool with size buckets for minSize to maxSize
// increasing by the given factor and maximum number of used bytes.
// No more than maxTotal bytes can be used at any given time unless maxTotal is set to 0.
func NewBucketedBytesPool(minSize, maxSize int, factor float64, maxTotal uint64) (*BucketedBytesPool, error) {
	if minSize < 1 {
		return nil, errors.New("invalid minimum pool size")
	}
	if maxSize < 1 {
		return nil, errors.New("invalid maximum pool size")
	}
	if factor < 1 {
		return nil, errors.New("invalid factor")
	}

	var sizes []int

	for s := minSize; s <= maxSize; s = int(float64(s) * factor) {
		sizes = append(sizes, s)
	}
	p := &BucketedBytesPool{
		buckets:  make([]sync.Pool, len(sizes)),
		sizes:    sizes,
		maxTotal: maxTotal,
		new: func(sz int) *[]byte {
			s := make([]byte, 0, sz)
			return &s
		},
	}
	return p, nil
}

// ErrPoolExhausted is returned if a pool cannot provide the request bytes.
var ErrPoolExhausted = errors.New("pool exhausted")

// Get returns a new byte slices that fits the given size.
func (p *BucketedBytesPool) Get(sz int) (*[]byte, error) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	if p.maxTotal > 0 && p.usedTotal+uint64(sz) > p.maxTotal {
		return nil, ErrPoolExhausted
	}

	for i, bktSize := range p.sizes {
		if sz > bktSize {
			continue
		}
		b, ok := p.buckets[i].Get().(*[]byte)
		if !ok {
			b = p.new(bktSize)
		}

		p.usedTotal += uint64(cap(*b))
		return b, nil
	}

	// The requested size exceeds that of our highest bucket, allocate it directly.
	p.usedTotal += uint64(sz)
	return p.new(sz), nil
}

// Put returns a byte slice to the right bucket in the pool.
func (p *BucketedBytesPool) Put(b *[]byte) {
	if b == nil {
		return
	}

	for i, bktSize := range p.sizes {
		if cap(*b) > bktSize {
			continue
		}
		*b = (*b)[:0]
		p.buckets[i].Put(b)
		break
	}

	p.mtx.Lock()
	defer p.mtx.Unlock()

	// We could assume here that our users will not make the slices larger
	// but lets be on the safe side to avoid an underflow of p.usedTotal.
	sz := uint64(cap(*b))
	if sz >= p.usedTotal {
		p.usedTotal = 0
	} else {
		p.usedTotal -= sz
	}
}
