package distributor

import (
	"context"
	"hash/fnv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

const (
	// The number of stripe locks.
	segmentationKeyRateStoreNumStripes = 64
)

// segmentKeyRateStore tracks the ingestion rate (bytes/sec) for each
// segmentation key.
type segmentationKeyRateStore struct {
	// stripes contains the rate buckets for segmentation keys. It is striped
	// to allow concurrent reads/writes to non-overlapping stripes.
	stripes []map[string]map[uint64][]segmentationKeyRateBucket
	locks   []sync.Mutex

	// window is the time window for the rate buckets. For example, to store
	// the last 5 minutes of rates the window should be 5 minutes.
	window time.Duration

	// bucketSize is the size of each rate bucket. For example, to use 1
	// minute buckets the bucketSize should be 1 minute.
	bucketSize time.Duration

	// numBuckets is calculated as the window divided by the bucketSize.
	numBuckets int

	logger log.Logger
}

type segmentationKeyRateBucket struct {
	start time.Time
	size  uint64
}

// newSegmentationKeyRateStore returns a new rate store for segmentation keys.
func newSegmentationKeyRateStore(window, bucketSize time.Duration, logger log.Logger) *segmentationKeyRateStore {
	return &segmentationKeyRateStore{
		stripes:    make([]map[string]map[uint64][]segmentationKeyRateBucket, segmentationKeyRateStoreNumStripes),
		locks:      make([]sync.Mutex, segmentationKeyRateStoreNumStripes),
		window:     window,
		bucketSize: bucketSize,
		numBuckets: int(window / bucketSize),
		logger:     logger,
	}
}

// Update updates the rate for the segmentation key. It returns the
// average rate over the window.
func (s *segmentationKeyRateStore) Update(tenant string, segmentationKey SegmentationKey, n uint64, now time.Time) uint64 {
	var result uint64
	s.withBuckets(tenant, segmentationKey, func(buckets []segmentationKeyRateBucket) {
		// buckets are implemented as a circular buffer. To update a bucket we
		// need to calculate the bucket index.
		bucketNum := now.UnixNano() / int64(s.bucketSize)
		bucketIdx := int(bucketNum % int64(s.numBuckets))
		bucket := buckets[bucketIdx]
		// Once we have found the bucket, we need to check if it is inside the
		// rate window. If not, we need to reset it before we can use it.
		expectedBucketStart := now.Truncate(s.bucketSize)
		if bucket.start.Before(expectedBucketStart) {
			bucket.start = expectedBucketStart
			bucket.size = 0
		}
		bucket.size += n
		buckets[bucketIdx] = bucket
		// Return the average rate over the window.
		result = s.averageRate(buckets, now)
	})
	return result
}

// EvictExpired evicts expired entries from the store. It returns the number
// of evicted entries.
func (s *segmentationKeyRateStore) EvictExpired(now time.Time) int {
	windowStart := now.Add(-s.window)
	evicted := 0
	for i := 0; i < len(s.locks); i++ {
		s.locks[i].Lock()
		for tenant, segmentationKeys := range s.stripes[i] {
			for segmentationKey, buckets := range segmentationKeys {
				// Count the number of buckets outside the window.
				var expiredBuckets int
				for _, bucket := range buckets {
					bucketEnd := bucket.start.Add(s.bucketSize)
					// If bucketEnd less than or equal to windowStart then
					// it is outside the window.
					if !bucketEnd.After(windowStart) {
						expiredBuckets++
					}
				}
				// If all buckets are outside the window the segmentation key
				// can be deleted.
				if expiredBuckets == len(buckets) {
					delete(segmentationKeys, segmentationKey)
					evicted++
				}
			}
			// If the tenant has no segmentation keys left, it can be deleted
			// too.
			if len(segmentationKeys) == 0 {
				delete(s.stripes[i], tenant)
			}
		}
		s.locks[i].Unlock()
	}
	return evicted
}

// Runs the eviction loop.
func (s *segmentationKeyRateStore) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			evicted := s.EvictExpired(time.Now())
			level.Info(s.logger).Log("msg", "evicted expired segmentation key rates", "count", evicted)
		}
	}
}

// averageRate returns the average rate for buckets in the window using a
// sliding window algorithm.
func (s *segmentationKeyRateStore) averageRate(buckets []segmentationKeyRateBucket, now time.Time) uint64 {
	var (
		totalBytes   uint64
		totalSeconds float64
		windowStart  = now.Add(-s.window)
	)
	for _, bucket := range buckets {
		bucketEnd := bucket.start.Add(s.bucketSize)
		// Ignore buckets that are outside the window.
		if bucket.start.IsZero() || bucketEnd.Before(windowStart) {
			continue
		}
		totalBytes += bucket.size
		// If this is the current bucket, count the number of seconds start
		// the start of the bucket, otherwise count the full duration.
		if now.Before(bucketEnd) {
			totalSeconds += now.Sub(bucket.start).Seconds()
		} else {
			totalSeconds += s.bucketSize.Seconds()
		}
	}
	if totalSeconds == 0 {
		return 0
	}
	return uint64(float64(totalBytes) / totalSeconds)
}

func (s *segmentationKeyRateStore) withBuckets(tenant string, segmentationKey SegmentationKey, f func(buckets []segmentationKeyRateBucket)) {
	segmentationKeyHash := segmentationKey.Sum64()
	s.withLock(tenant, segmentationKey, func(stripe int) {
		// Get the buckets for the tenant and segmentation key.
		tenants := s.stripes[stripe]
		if tenants == nil {
			tenants = make(map[string]map[uint64][]segmentationKeyRateBucket)
			s.stripes[stripe] = tenants
		}
		segmentationKeys, ok := tenants[tenant]
		if !ok {
			segmentationKeys = make(map[uint64][]segmentationKeyRateBucket)
			tenants[tenant] = segmentationKeys
		}
		buckets, ok := segmentationKeys[segmentationKeyHash]
		if !ok {
			buckets = make([]segmentationKeyRateBucket, s.numBuckets)
			segmentationKeys[segmentationKeyHash] = buckets
		}
		f(buckets)
	})
}

func (s *segmentationKeyRateStore) withLock(tenant string, segmentationKey SegmentationKey, f func(i int)) {
	i := s.getStripe(tenant, segmentationKey)
	s.locks[i].Lock()
	defer s.locks[i].Unlock()
	f(i)
}

// getStripe returns the stripe index for the tenant and segmentation key.
// It ensures that the same segmentation key from different tenants is
// distributed evenly across the stripes.
func (s *segmentationKeyRateStore) getStripe(tenant string, segmentationKey SegmentationKey) int {
	h := fnv.New64a()
	_, _ = h.Write([]byte(tenant))
	_, _ = h.Write([]byte(segmentationKey))
	return int(h.Sum64() % uint64(len(s.locks)))
}
