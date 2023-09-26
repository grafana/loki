package v1

import (
	"hash"
	"hash/crc32"
	"sync"

	"github.com/prometheus/prometheus/util/pool"
)

const (
	magicNumber = uint32(0xCA7CAFE5)
	// Add new versions below
	V1 byte = iota
)

const (
	DefaultSchemaVersion = V1
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

	// 1KB -> 8MB
	BlockPool = BytePool{
		pool: pool.New(
			1<<10, 1<<24, 4,
			func(size int) interface{} {
				return make([]byte, size)
			}),
	}
)

type BytePool struct {
	pool *pool.Pool
}

func (p *BytePool) Get(size int) []byte {
	return p.pool.Get(size).([]byte)[:0]
}
func (p *BytePool) Put(b []byte) {
	p.pool.Put(b)
}

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

type Iterator[T any] interface {
	Next() bool
	Err() error
	At() T
}

type SliceIter[T any] struct {
	cur int
	xs  []T
}

func NewSliceIter[T any](xs []T) *SliceIter[T] {
	return &SliceIter[T]{xs: xs, cur: -1}
}

func (it *SliceIter[T]) Next() bool {
	it.cur++
	return it.cur < len(it.xs)
}

func (it *SliceIter[T]) Err() error {
	return nil
}

func (it *SliceIter[T]) At() T {
	return it.xs[it.cur]
}
