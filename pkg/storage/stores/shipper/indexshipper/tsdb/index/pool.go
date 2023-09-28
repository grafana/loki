package index

import (
	"sync"

	"github.com/prometheus/prometheus/util/pool"
)

var (
	ChunkMetasPool       PoolChunkMetas
	chunkPageMarkersPool = poolChunkPageMarkers{
		// pools of lengths 64->1024
		pool: pool.New(64, 1024, 2, func(sz int) interface{} {
			return make(chunkPageMarkers, 0, sz)
		}),
	}
)

type PoolChunkMetas struct {
	pool sync.Pool
}

func (p *PoolChunkMetas) Get() []ChunkMeta {
	if xs := p.pool.Get(); xs != nil {
		return xs.([]ChunkMeta)
	}
	return make([]ChunkMeta, 0, 1<<10)
}

func (p *PoolChunkMetas) Put(xs []ChunkMeta) {
	xs = xs[:0]
	//nolint:staticcheck
	p.pool.Put(xs)
}

type poolChunkPageMarkers struct {
	pool *pool.Pool
}

func (p *poolChunkPageMarkers) Get(sz int) chunkPageMarkers {
	return p.pool.Get(sz).(chunkPageMarkers)
}

func (p *poolChunkPageMarkers) Put(xs chunkPageMarkers) {
	xs = xs[:0]
	//nolint:staticcheck
	p.pool.Put(xs)
}
