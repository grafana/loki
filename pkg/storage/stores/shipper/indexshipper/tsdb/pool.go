package tsdb

import (
	"sync"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

var (
	ChunkMetasPool = &index.ChunkMetasPool // re-exporting
	SeriesPool     PoolSeries
	ChunkRefsPool  PoolChunkRefs
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

func (p *PoolChunkRefs) Get() []logproto.ChunkRefWithSizingInfo {
	if xs := p.pool.Get(); xs != nil {
		return xs.([]logproto.ChunkRefWithSizingInfo)
	}
	return make([]logproto.ChunkRefWithSizingInfo, 0, 1<<10)
}

func (p *PoolChunkRefs) Put(xs []logproto.ChunkRefWithSizingInfo) {
	xs = xs[:0]
	//nolint:staticcheck
	p.pool.Put(xs)
}
