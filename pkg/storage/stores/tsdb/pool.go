package tsdb

import (
	"encoding/binary"
	"sync"
	"sync/atomic"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/prometheus/common/model"
	"github.com/willf/bloom"
)

var (
	ChunkMetasPool = &index.ChunkMetasPool // re-exporting
	SeriesPool     PoolSeries
	ChunkRefsPool  PoolChunkRefs
	BloomPool      PoolBloom
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

type PoolBloom struct {
	pool sync.Pool
}

func (p *PoolBloom) Get() *StatsBlooms {
	if x := p.pool.Get(); x != nil {
		return x.(*StatsBlooms)
	}

	return newStatsBlooms()

}

func (p *PoolBloom) Put(x *StatsBlooms) {
	x.Streams.ClearAll()
	x.Chunks.ClearAll()
	x.Stats = Stats{}
	p.pool.Put(x)
}

// These are very expensive in terms of memory usage,
// each requiring ~12.5MB. Therefore we heavily rely on pool usage.
// See https://hur.st/bloomfilter for play around with this idea.
func newStatsBlooms() *StatsBlooms {
	// 1 million streams @ 1% error =~ 1.14MB
	streams := bloom.NewWithEstimates(1e6, 0.01)
	// 10 million chunks @ 1% error =~ 11.43MB
	chunks := bloom.NewWithEstimates(10e6, 0.01)
	return &StatsBlooms{
		Streams: streams,
		Chunks:  chunks,
	}
}

// Bloom filters for estimating duplicate statistics across both series
// and chunks within TSDB indices. These are used to calculate data topology
// statistics prior to running queries.
type StatsBlooms struct {
	sync.RWMutex
	Streams, Chunks *bloom.BloomFilter
	Stats           Stats
}

func (b *StatsBlooms) AddStream(fp model.Fingerprint) {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, uint64(fp))
	b.add(b.Streams, key, func() {
		atomic.AddUint64(&b.Stats.Streams, 1)
	})
}

func (b *StatsBlooms) AddChunk(fp model.Fingerprint, chk index.ChunkMeta) {
	// fingerprint + mintime + maxtime + checksum
	ln := 8 + 8 + 8 + 4
	key := make([]byte, ln)
	binary.BigEndian.PutUint64(key, uint64(fp))
	binary.BigEndian.PutUint64(key[8:], uint64(chk.MinTime))
	binary.BigEndian.PutUint64(key[16:], uint64(chk.MaxTime))
	binary.BigEndian.PutUint32(key[24:], chk.Checksum)
	b.add(b.Chunks, key, func() {
		atomic.AddUint64(&b.Stats.Chunks, 1)
		atomic.AddUint64(&b.Stats.Bytes, uint64(chk.KB<<10))
		atomic.AddUint64(&b.Stats.Entries, uint64(chk.Entries))
	})
}

func (b *StatsBlooms) add(filter *bloom.BloomFilter, key []byte, update func()) {
	b.RLock()
	ok := b.Streams.Test(key)
	b.RUnlock()

	if ok {
		return
	}

	b.Lock()
	defer b.Unlock()
	if ok = b.Streams.TestAndAdd(key); !ok {
		update()
	}
}
