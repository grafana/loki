package index

import "sync"

var ChunkMetasPool PoolChunkMetas

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
