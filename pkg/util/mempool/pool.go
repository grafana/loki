package mempool

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/dustin/go-humanize"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	errSlabExhausted = errors.New("slab exhausted")

	reasonSizeExceeded  = "size-exceeded"
	reasonSlabExhausted = "slab-exhausted"
)

type slab struct {
	once        sync.Once
	buffer      chan unsafe.Pointer
	size, count int
	metrics     *metrics
	name        string
}

func newSlab(bufferSize, bufferCount int, m *metrics) *slab {
	name := humanize.Bytes(uint64(bufferSize))
	m.availableBuffersPerSlab.WithLabelValues(name).Set(0) // initialize metric with value 0

	return &slab{
		size:    bufferSize,
		count:   bufferCount,
		metrics: m,
		name:    name,
	}
}

func (s *slab) init() {
	s.buffer = make(chan unsafe.Pointer, s.count)
	for i := 0; i < s.count; i++ {
		buf := make([]byte, 0, s.size)
		ptr := unsafe.Pointer(unsafe.SliceData(buf))
		s.buffer <- ptr
	}
	s.metrics.availableBuffersPerSlab.WithLabelValues(s.name).Set(float64(s.count))
}

func (s *slab) get(size int) []byte {
	s.metrics.accesses.WithLabelValues(s.name, opTypeGet).Inc()
	s.once.Do(s.init)

	// wait for available buffer on channel
	var buf []byte
	select {
	case ptr := <-s.buffer:
		buf = unsafe.Slice((*byte)(ptr), s.size)
	default:
		s.metrics.errorsCounter.WithLabelValues(s.name, reasonSlabExhausted).Inc()
		buf = make([]byte, s.size)
	}

	return buf[:size]
}

func (s *slab) put(buf []byte) (returned bool) {
	s.metrics.accesses.WithLabelValues(s.name, opTypePut).Inc()
	if s.buffer == nil {
		panic("slab is not initialized")
	}

	ptr := unsafe.Pointer(unsafe.SliceData(buf))

	// try to put buffer back on channel; if channel is full, discard buffer
	select {
	case s.buffer <- ptr:
		return true
	default:
		return false
	}
}

// MemPool is an Allocator implementation that uses a fixed size memory pool
// that is split into multiple slabs of different buffer sizes.
// Buffers are re-cycled and need to be returned back to the pool, otherwise
// the pool runs out of available buffers.
type MemPool struct {
	slabs   []*slab
	metrics *metrics
}

func New(name string, buckets []Bucket, r prometheus.Registerer) *MemPool {
	a := &MemPool{
		slabs:   make([]*slab, 0, len(buckets)),
		metrics: newMetrics(r, name),
	}
	for _, b := range buckets {
		a.slabs = append(a.slabs, newSlab(int(b.Capacity), b.Size, a.metrics))
	}
	return a
}

// Get satisfies Allocator interface
// Allocating a buffer from an exhausted pool/slab, or allocating a buffer that
// exceeds the largest slab size will return an error.
func (a *MemPool) Get(size int) []byte {
	for i := 0; i < len(a.slabs); i++ {
		if a.slabs[i].size < size {
			continue
		}
		return a.slabs[i].get(size)
	}
	a.metrics.errorsCounter.WithLabelValues("pool", reasonSizeExceeded).Inc()
	return make([]byte, size)
}

// Put satisfies Allocator interface
// Every buffer allocated with Get(size int) needs to be returned to the pool
// using Put(buffer []byte) so it can be re-cycled.
// NB(owen-d): MemPool ensures that buffer capacities are _exactly_ the same
// as individual slab sizes before returning them to the pool.
func (a *MemPool) Put(buffer []byte) (returned bool) {
	size := cap(buffer)
	for i := 0; i < len(a.slabs); i++ {
		if a.slabs[i].size == size {
			return a.slabs[i].put(buffer)
		}

		// if the current slab is too large, exit early
		// as slabs are sorted by size and we won't find a smaller slab
		if a.slabs[i].size > size {
			break
		}
	}

	return false
}
