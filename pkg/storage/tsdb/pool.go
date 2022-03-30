package tsdb

import (
	"sync"

	"github.com/grafana/loki/pkg/storage/tsdb/index"
)

var (
	chunkMetasPool poolChunkMetas // private, internal pkg use only
	SeriesPool     PoolSeries
	ChunkRefsPool  PoolChunkRefs
)

type poolChunkMetas struct {
	pool sync.Pool
}

func (p *poolChunkMetas) Get() []index.ChunkMeta {
	if xs := p.pool.Get(); xs != nil {
		return xs.([]index.ChunkMeta)
	}
	return make([]index.ChunkMeta, 0, 1<<10)
}

func (p *poolChunkMetas) Put(xs []index.ChunkMeta) {
	xs = xs[:0]
	//nolint:staticcheck
	p.pool.Put(xs)
}

type PoolSeries struct {
	pool sync.Pool
}

func (p *PoolSeries) Get() []Series {
	if xs := p.pool.Get(); xs != nil {
		return xs.([]Series)
	}
	return make([]Series, 0, 1<<10)
}

func (p *PoolSeries) Put(xs []Series) {
	xs = xs[:0]
	//nolint:staticcheck
	p.pool.Put(xs)
}

type PoolChunkRefs struct {
	pool sync.Pool
}

func (p *PoolChunkRefs) Get() []ChunkRef {
	if xs := p.pool.Get(); xs != nil {
		return xs.([]ChunkRef)
	}
	return make([]ChunkRef, 0, 1<<10)
}

func (p *PoolChunkRefs) Put(xs []ChunkRef) {
	xs = xs[:0]
	//nolint:staticcheck
	p.pool.Put(xs)
}
