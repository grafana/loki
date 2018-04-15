package util

import (
	"hash/fnv"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"
)

// HashBucketHistogramOpts are the options for making a HashBucketHistogram
type HashBucketHistogramOpts struct {
	prometheus.HistogramOpts
	HashBuckets int
}

// HashBucketHistogram is used to track a histogram of per-bucket rates.
//
// For instance, I want to know that 50% of rows are getting X QPS or lower
// and 99% are getting Y QPS of lower.  At first glance, this would involve
// tracking write rate per row, and periodically sticking those numbers in
// a histogram.  To make this fit in memory: instead of per-row, we keep
// N buckets of counters and hash the key to a bucket.  Then every second
// we update a histogram with the bucket values (and zero the buckets).
//
// Note, we want this metric to be relatively independent of the number of
// hash buckets and QPS of the service - we're trying to measure how well
// load balanced the write load is.  So we normalise the values in the hash
// buckets such that if all buckets are '1', then we have even load.  We
// do this by multiplying the number of ops per bucket by the number of
// buckets, and dividing by the number of ops.
type HashBucketHistogram interface {
	prometheus.Metric
	prometheus.Collector

	Observe(string, uint32)
	Stop()
}

type hashBucketHistogram struct {
	prometheus.Histogram
	mtx     sync.RWMutex
	buckets *hashBuckets
	quit    chan struct{}
	opts    HashBucketHistogramOpts
}

type hashBuckets struct {
	ops     uint32
	buckets []uint32
}

// NewHashBucketHistogram makes a new HashBucketHistogram
func NewHashBucketHistogram(opts HashBucketHistogramOpts) HashBucketHistogram {
	result := &hashBucketHistogram{
		Histogram: prometheus.NewHistogram(opts.HistogramOpts),
		quit:      make(chan struct{}),
		opts:      opts,
	}
	result.swapBuckets()
	go result.loop()
	return result
}

// Stop the background goroutine
func (h *hashBucketHistogram) Stop() {
	h.quit <- struct{}{}
}

func (h *hashBucketHistogram) swapBuckets() *hashBuckets {
	h.mtx.Lock()
	buckets := h.buckets
	h.buckets = &hashBuckets{
		buckets: make([]uint32, h.opts.HashBuckets, h.opts.HashBuckets),
	}
	h.mtx.Unlock()
	return buckets
}

func (h *hashBucketHistogram) loop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			buckets := h.swapBuckets()
			for _, v := range buckets.buckets {
				if buckets.ops > 0 {
					h.Histogram.Observe(float64(v) * float64(h.opts.HashBuckets) / float64(buckets.ops))
				}
			}
		case <-h.quit:
			return
		}
	}
}

// Observe implements HashBucketHistogram
func (h *hashBucketHistogram) Observe(key string, value uint32) {
	h.mtx.RLock()
	hash := fnv.New32()
	hash.Write(bytesView(key))
	i := hash.Sum32() % uint32(h.opts.HashBuckets)
	atomic.AddUint32(&h.buckets.ops, 1)
	atomic.AddUint32(&h.buckets.buckets[i], value)
	h.mtx.RUnlock()
}

func bytesView(v string) []byte {
	strHeader := (*reflect.StringHeader)(unsafe.Pointer(&v))
	bytesHeader := reflect.SliceHeader{
		Data: strHeader.Data,
		Len:  strHeader.Len,
		Cap:  strHeader.Len,
	}
	return *(*[]byte)(unsafe.Pointer(&bytesHeader))
}
