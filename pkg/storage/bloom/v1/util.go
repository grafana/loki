package v1

import (
	"hash"
	"hash/crc32"
	"io"
	"sync"

	"github.com/grafana/loki/v3/pkg/util/mempool"
)

const (
	magicNumber = uint32(0xCA7CAFE5)
)

var (
	castagnoliTable = crc32.MakeTable(crc32.Castagnoli)

	// Pool of crc32 hash
	Crc32HashPool = ChecksumPool{
		Pool: sync.Pool{
			New: func() interface{} {
				return crc32.New(castagnoliTable)
			},
		},
	}

	// buffer pool for series pages
	// 1KB 2KB 4KB 8KB 16KB 32KB 64KB 128KB
	SeriesPagePool = mempool.NewBytePoolAllocator(1<<10, 128<<10, 2)
)

func newCRC32() hash.Hash32 {
	return crc32.New(castagnoliTable)
}

type ChecksumPool struct {
	sync.Pool
}

func (p *ChecksumPool) Get() hash.Hash32 {
	h := p.Pool.Get().(hash.Hash32)
	h.Reset()
	return h
}

func (p *ChecksumPool) Put(h hash.Hash32) {
	p.Pool.Put(h)
}

type NoopCloser struct {
	io.Writer
}

func (n NoopCloser) Close() error {
	return nil
}

func NewNoopCloser(w io.Writer) NoopCloser {
	return NoopCloser{w}
}

func PointerSlice[T any](xs []T) []*T {
	out := make([]*T, len(xs))
	for i := range xs {
		out[i] = &xs[i]
	}
	return out
}
