package limits

import (
	"sync"
	"time"
)

// A rateStore uses configurable time buckets to track per-second rates.
type rateStore struct {
	window, bucketSize time.Duration
	numBuckets         int
	mtx                sync.RWMutex
	scopes             map[string]map[uint64]*rateEntry
}

// A rateEntry holds a circular buffer of buckets.
type rateEntry struct {
	buckets []rateBucket
}

// A rateBucket contains the total value and timestamp of a bucket.
type rateBucket struct {
	ts    int64
	value uint64
}

// rateRecordBatchParams are the params passed to [RecordBatch].
type rateRecordBatchParams []rateRecordBatchParam

// A rateRecordBatchParam contains the parameters to update a scope at a
// specific timestamp.
type rateRecordBatchParam struct {
	scope string
	key   uint64
	value uint64
	ts    time.Time
}

// newRateStore returns a new rateStore.
func newRateStore(window, bucketSize time.Duration) *rateStore {
	return &rateStore{
		window:     window,
		bucketSize: bucketSize,
		numBuckets: int(window / bucketSize),
		scopes:     make(map[string]map[uint64]*rateEntry),
	}
}

// Record updates the buckets for the specified key and scope.
func (s *rateStore) Record(scope string, key uint64, value uint64, ts time.Time) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.update(scope, key, value, ts)
}

// RecordBatch updates the buckets for all params.
func (s *rateStore) RecordBatch(params rateRecordBatchParams) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	for _, param := range params {
		s.update(param.scope, param.key, param.value, param.ts)
	}
}

// update the buckets for the specified key and scope.
func (s *rateStore) update(scope string, key uint64, value uint64, ts time.Time) {
	entries := s.scopes[scope]
	if entries == nil {
		entries = make(map[uint64]*rateEntry)
		s.scopes[scope] = entries
	}
	entry := entries[key]
	if entry == nil {
		entry = new(rateEntry)
		entry.buckets = make([]rateBucket, s.numBuckets)
		entries[key] = entry
	}
	// Find the correct bucket for ts.
	tsNano := ts.UnixNano()
	bucketNum := tsNano / int64(s.bucketSize)
	bucketIdx := int(bucketNum % int64(s.numBuckets))
	bucket := entry.buckets[bucketIdx]
	// Reset the bucket if its from a previous window.
	bucketStart := ts.Truncate(s.bucketSize).UnixNano()
	if bucket.ts < bucketStart {
		bucket.ts = bucketStart
		bucket.value = 0
	}
	bucket.value += value
	entry.buckets[bucketIdx] = bucket
}

// Rate returns the average per-second rate for the specified scope and key,
// computed over all buckets. If the key does not exist or there are no active
// buckets, it returns 0.
func (s *rateStore) Rate(scope string, key uint64) float64 {
	windowStartNano := time.Now().Add(-s.window).UnixNano()
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	entries, ok := s.scopes[scope]
	if !ok {
		return 0
	}
	entry, ok := entries[key]
	if !ok {
		return 0
	}
	return calcBucketRate(windowStartNano, s.bucketSize, entry.buckets)
}

// RatesForScope returns the average per-second rate for all entries in the
// specified scope.
func (s *rateStore) RatesForScope(scope string) map[uint64]float64 {
	windowStartNano := time.Now().Add(-s.window).UnixNano()
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	entries, ok := s.scopes[scope]
	if !ok {
		return nil
	}
	rates := make(map[uint64]float64, len(entries))
	for key, entry := range entries {
		rates[key] = calcBucketRate(windowStartNano, s.bucketSize, entry.buckets)
	}
	return rates
}

// Evict deletes all entries outside the rate window.
func (s *rateStore) Evict() {
	windowStartNano := time.Now().Add(-s.window).UnixNano()
	s.mtx.Lock()
	defer s.mtx.Unlock()
	for scope, entries := range s.scopes {
		for key, entry := range entries {
			if !hasActiveBuckets(entry, windowStartNano) {
				delete(entries, key)
			}
		}
		if len(entries) == 0 {
			delete(s.scopes, scope)
		}
	}
}

// hasActiveBuckets reports whether the entry has at least one bucket newer
// than tsNano.
func hasActiveBuckets(entry *rateEntry, tsNano int64) bool {
	for _, bucket := range entry.buckets {
		if bucket.ts >= tsNano {
			return true
		}
	}
	return false
}

// calcBucketRate returns the average per-second rate for the buckets.
func calcBucketRate(windowStartNano int64, bucketSize time.Duration, buckets []rateBucket) float64 {
	var (
		totalValue   uint64
		totalBuckets int
	)
	for _, bucket := range buckets {
		if bucket.ts >= windowStartNano {
			totalValue += bucket.value
		}
		totalBuckets++
	}
	return float64(totalValue) / float64((bucketSize * time.Duration(totalBuckets)).Seconds())
}
