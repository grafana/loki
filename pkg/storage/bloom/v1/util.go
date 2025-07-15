package v1

import (
	"hash"
	"hash/crc32"
	"io"
	"sync"

	"github.com/grafana/loki/v3/pkg/util/mempool"
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

type Set[V comparable] struct {
	internal map[V]struct{}
}

func NewSet[V comparable](size int) Set[V] {
	return Set[V]{make(map[V]struct{}, size)}
}

func NewSetFromLiteral[V comparable](v ...V) Set[V] {
	set := NewSet[V](len(v))
	for _, elem := range v {
		set.Add(elem)
	}
	return set
}

func (s Set[V]) Add(v V) bool {
	_, ok := s.internal[v]
	if !ok {
		s.internal[v] = struct{}{}
	}
	return !ok
}

func (s Set[V]) Len() int {
	return len(s.internal)
}

func (s Set[V]) Items() []V {
	set := make([]V, 0, s.Len())
	for k := range s.internal {
		set = append(set, k)
	}
	return set
}

func (s Set[V]) Union(other Set[V]) {
	for _, v := range other.Items() {
		s.Add(v)
	}
}
