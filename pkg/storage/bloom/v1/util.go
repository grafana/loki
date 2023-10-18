package v1

import (
	"hash"
	"hash/crc32"
	"io"
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

type PeekingIterator[T any] interface {
	Peek() (T, bool)
	Iterator[T]
}

type PeekIter[T any] struct {
	itr Iterator[T]

	// the first call to Next() will populate cur & next
	init      bool
	zero      T // zero value of T for returning empty Peek's
	cur, next *T
}

func NewPeekingIter[T any](itr Iterator[T]) *PeekIter[T] {
	return &PeekIter[T]{itr: itr}
}

// populates the first element so Peek can be used and subsequent Next()
// calls will work as expected
func (it *PeekIter[T]) ensureInit() {
	if it.init {
		return
	}
	if it.itr.Next() {
		at := it.itr.At()
		it.next = &at
	}
	it.init = true
}

// load the next element and return the cached one
func (it *PeekIter[T]) cacheNext() {
	it.cur = it.next
	if it.cur != nil && it.itr.Next() {
		at := it.itr.At()
		it.next = &at
	} else {
		it.next = nil
	}
}

func (it *PeekIter[T]) Next() bool {
	it.ensureInit()
	it.cacheNext()
	return it.cur != nil
}

func (it *PeekIter[T]) Peek() (T, bool) {
	it.ensureInit()
	if it.next == nil {
		return it.zero, false
	}
	return *it.next, true
}

func (it *PeekIter[T]) Err() error {
	return it.itr.Err()
}

func (it *PeekIter[T]) At() T {
	return *it.cur
}

type SeekIter[K, V any] interface {
	Seek(K) error
	Iterator[V]
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

type EmptyIter[T any] struct {
	zero T
}

func (it *EmptyIter[T]) Next() bool {
	return false
}

func (it *EmptyIter[T]) Err() error {
	return nil
}

func (it *EmptyIter[T]) At() T {
	return it.zero
}

// noop
func (it *EmptyIter[T]) Reset() {}

func NewEmptyIter[T any](zero T) *EmptyIter[T] {
	return &EmptyIter[T]{zero: zero}
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
