package v1

import (
	"context"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"sync"

	"github.com/prometheus/prometheus/util/pool"
)

type Version byte

func (v Version) String() string {
	return fmt.Sprintf("v%d", v)
}

const (
	magicNumber = uint32(0xCA7CAFE5)
	// Add new versions below
	V1 Version = iota
	// V2 supports single series blooms encoded over multiple pages
	// to accommodate larger single series
	V2
)

const (
	DefaultSchemaVersion = V2
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
	SeriesPagePool = BytePool{
		pool: pool.New(
			1<<10, 128<<10, 2,
			func(size int) interface{} {
				return make([]byte, size)
			}),
	}

	// buffer pool for bloom pages
	// 128KB 256KB 512KB 1MB 2MB 4MB 8MB 16MB 32MB 64MB 128MB
	BloomPagePool = &BytePool{
		pool: pool.New(
			128<<10, 128<<20, 2,
			func(size int) interface{} {
				return make([]byte, size)
			}),
	}
)

// Allocator handles byte slices for bloom queriers.
// It exists to reduce the cost of allocations and allows to re-use already allocated memory.
type Allocator interface {
	Get(size int) ([]byte, error)
	Put([]byte) bool
}

// SimpleHeapAllocator allocates a new byte slice every time and does not re-cycle buffers.
type SimpleHeapAllocator struct{}

func (a *SimpleHeapAllocator) Get(size int) ([]byte, error) {
	return make([]byte, size), nil
}

func (a *SimpleHeapAllocator) Put([]byte) bool {
	return true
}

// BytePool uses a sync.Pool to re-cycle already allocated buffers.
type BytePool struct {
	pool *pool.Pool
}

// Get implements Allocator
func (p *BytePool) Get(size int) ([]byte, error) {
	return p.pool.Get(size).([]byte)[:size], nil
}

// Put implements Allocator
func (p *BytePool) Put(b []byte) bool {
	p.pool.Put(b)
	return true
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

type SizedIterator[T any] interface {
	Iterator[T]
	Remaining() int // remaining
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

func (it *SliceIter[T]) Remaining() int {
	return max(0, len(it.xs)-(it.cur+1))
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

type MapIter[A any, B any] struct {
	Iterator[A]
	f func(A) B
}

func NewMapIter[A any, B any](src Iterator[A], f func(A) B) *MapIter[A, B] {
	return &MapIter[A, B]{Iterator: src, f: f}
}

func (it *MapIter[A, B]) At() B {
	return it.f(it.Iterator.At())
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

func (it *EmptyIter[T]) Peek() (T, bool) {
	return it.zero, false
}

func (it *EmptyIter[T]) Remaining() int {
	return 0
}

// noop
func (it *EmptyIter[T]) Reset() {}

func NewEmptyIter[T any]() *EmptyIter[T] {
	return &EmptyIter[T]{}
}

type CancellableIter[T any] struct {
	ctx context.Context
	Iterator[T]
}

func (cii *CancellableIter[T]) Next() bool {
	select {
	case <-cii.ctx.Done():
		return false
	default:
		return cii.Iterator.Next()
	}
}

func (cii *CancellableIter[T]) Err() error {
	if err := cii.ctx.Err(); err != nil {
		return err
	}
	return cii.Iterator.Err()
}

func NewCancelableIter[T any](ctx context.Context, itr Iterator[T]) *CancellableIter[T] {
	return &CancellableIter[T]{ctx: ctx, Iterator: itr}
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

type CloseableIterator[T any] interface {
	Iterator[T]
	Close() error
}

func NewCloseableIterator[T io.Closer](itr Iterator[T]) *CloseIter[T] {
	return &CloseIter[T]{itr}
}

type CloseIter[T io.Closer] struct {
	Iterator[T]
}

func (i *CloseIter[T]) Close() error {
	return i.At().Close()
}

type PeekingCloseableIterator[T any] interface {
	PeekingIterator[T]
	CloseableIterator[T]
}

type PeekCloseIter[T any] struct {
	*PeekIter[T]
	close func() error
}

func NewPeekCloseIter[T any](itr CloseableIterator[T]) *PeekCloseIter[T] {
	return &PeekCloseIter[T]{PeekIter: NewPeekingIter[T](itr), close: itr.Close}
}

func (it *PeekCloseIter[T]) Close() error {
	return it.close()
}

type ResettableIterator[T any] interface {
	Reset() error
	Iterator[T]
}

type CloseableResettableIterator[T any] interface {
	CloseableIterator[T]
	ResettableIterator[T]
}

type Predicate[T any] func(T) bool

func NewFilterIter[T any](it Iterator[T], p Predicate[T]) *FilterIter[T] {
	return &FilterIter[T]{
		Iterator: it,
		match:    p,
	}
}

type FilterIter[T any] struct {
	Iterator[T]
	match Predicate[T]
}

func (i *FilterIter[T]) Next() bool {
	hasNext := i.Iterator.Next()
	for hasNext && !i.match(i.Iterator.At()) {
		hasNext = i.Iterator.Next()
	}
	return hasNext
}

type CounterIterator[T any] interface {
	Iterator[T]
	Count() int
}

type CounterIter[T any] struct {
	Iterator[T] // the underlying iterator
	count       int
}

func NewCounterIter[T any](itr Iterator[T]) *CounterIter[T] {
	return &CounterIter[T]{Iterator: itr}
}

func (it *CounterIter[T]) Next() bool {
	if it.Iterator.Next() {
		it.count++
		return true
	}
	return false
}

func (it *CounterIter[T]) Count() int {
	return it.count
}
