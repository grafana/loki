package tsdb

import (
	"sync"

	"github.com/grafana/loki/pkg/storage/tsdb/index"
)

var chunkMetasPool chunkMetaPool

type chunkMetaPool struct {
	pool sync.Pool
}

func (p *chunkMetaPool) Get() []index.ChunkMeta {
	if xs := p.pool.Get(); xs != nil {
		return xs.([]index.ChunkMeta)
	}
	return make([]index.ChunkMeta, 0, 1<<10)
}

func (p *chunkMetaPool) Put(xs []index.ChunkMeta) {
	xs = xs[:0]
	p.pool.Put(xs)
}
