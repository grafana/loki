package tsdb

import (
	"sync"

	"github.com/grafana/loki/pkg/queue"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

var (
	ChunkMetasPool = &index.ChunkMetasPool // re-exporting
	SeriesPool     PoolSeries
	ChunkRefsPool  PoolChunkRefs
	sizedFPsPool   = queue.NewSlicePool[sizedFP](1<<8, 1<<16, 4) // 256->65536
)

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
