// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package downsample

import (
	"sync"

	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

// Pool is a memory pool of chunk objects, supporting Thanos aggregated chunk encoding.
// It maintains separate pools for xor and aggr chunks.
type pool struct {
	wrapped chunkenc.Pool
	aggr    sync.Pool
}

// TODO(bwplotka): Add reasonable limits to our sync pooling them to detect OOMs early.
func NewPool() chunkenc.Pool {
	return &pool{
		wrapped: chunkenc.NewPool(),
		aggr: sync.Pool{
			New: func() interface{} {
				return &AggrChunk{}
			},
		},
	}
}

func (p *pool) Get(e chunkenc.Encoding, b []byte) (chunkenc.Chunk, error) {
	switch e {
	case ChunkEncAggr:
		c := p.aggr.Get().(*AggrChunk)
		*c = AggrChunk(b)
		return c, nil
	}

	return p.wrapped.Get(e, b)

}

func (p *pool) Put(c chunkenc.Chunk) error {
	switch c.Encoding() {
	case ChunkEncAggr:
		ac, ok := c.(*AggrChunk)
		// This may happen often with wrapped chunks. Nothing we can really do about
		// it but returning an error would cause a lot of allocations again. Thus,
		// we just skip it.
		if !ok {
			return nil
		}

		// Clear []byte.
		*ac = AggrChunk(nil)
		p.aggr.Put(ac)
		return nil
	}

	return p.wrapped.Put(c)
}
