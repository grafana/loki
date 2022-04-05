package index

import "sync"

var PoolChunkMetas poolChunkMetas

type poolChunkMetas struct {
	pool sync.Pool
}

func (p *poolChunkMetas) Get() []ChunkMeta {
	if xs := p.pool.Get(); xs != nil {
		return xs.([]ChunkMeta)
	}
	return make([]ChunkMeta, 0, 1<<10)
}

func (p *poolChunkMetas) Put(xs []ChunkMeta) {
	xs = xs[:0]
	//nolint:staticcheck
	p.pool.Put(xs)
}
