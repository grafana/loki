package limits

import (
	"errors"
	"fmt"
	"hash/fnv"
	"iter"
	"sync"
	"time"

	"github.com/coder/quartz"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// The number of stripe locks.
const numStripes = 64

var (
	errOutsideActiveWindow = errors.New("outside active time window")
)

var (
	tenantStreamsDesc = prometheus.NewDesc(
		"loki_ingest_limits_streams",
		"The current number of streams per tenant, including streams outside the active window.",
		[]string{"tenant"},
		nil,
	)
	tenantActiveStreamsDesc = prometheus.NewDesc(
		"loki_ingest_limits_active_streams",
		"The current number of active streams per tenant.",
		[]string{"tenant"},
		nil,
	)
)

// iterateFunc is a closure called for each stream.
type iterateFunc func(tenant string, partition int32, stream streamUsage)

// usageStore stores per-tenant stream usage data.
type usageStore struct {
	activeWindow  time.Duration
	rateWindow    time.Duration
	bucketSize    time.Duration
	numBuckets    int
	numPartitions int
	stripes       []map[string]tenantUsage
	locks         []stripeLock

	// Used for tests.
	clock quartz.Clock
}

// tenantUsage contains the per-partition stream usage for a tenant.
type tenantUsage map[int32]map[uint64]streamUsage

// streamUsage represents the metadata for a stream loaded from the kafka topic.
// It contains the minimal information to count per tenant active streams and
// rate limits.
type streamUsage struct {
	hash        uint64
	lastSeenAt  int64
	totalSize   uint64
	rateBuckets []rateBucket
}

// RateBucket represents the bytes received during a specific time interval
// It is used to calculate the rate limit for a stream.
type rateBucket struct {
	timestamp int64  // start of the interval
	size      uint64 // bytes received during this interval
}

type stripeLock struct {
	sync.RWMutex
	// Padding to avoid multiple locks being on the same cache line.
	_ [40]byte
}

// newUsageStore returns a new UsageStore.
func newUsageStore(activeWindow, rateWindow, bucketSize time.Duration, numPartitions int, reg prometheus.Registerer) (*usageStore, error) {
	s := &usageStore{
		activeWindow:  activeWindow,
		rateWindow:    rateWindow,
		bucketSize:    bucketSize,
		numBuckets:    int(rateWindow / bucketSize),
		numPartitions: numPartitions,
		stripes:       make([]map[string]tenantUsage, numStripes),
		locks:         make([]stripeLock, numStripes),
		clock:         quartz.NewReal(),
	}
	for i := range s.stripes {
		s.stripes[i] = make(map[string]tenantUsage)
	}
	if err := reg.Register(s); err != nil {
		return nil, fmt.Errorf("failed to register metrics: %w", err)
	}
	return s, nil
}

// ActiveStreams returns an iterator over all active streams. As this method
// acquires a read lock, the iterator must not block.
func (s *usageStore) ActiveStreams() iter.Seq2[string, streamUsage] {
	// To prevent time moving forward while iterating, use the current time
	// to check the active and rate window.
	var (
		now            = s.clock.Now()
		inActiveWindow = newActiveWindowFunc(s.activeWindow, now)
		inRateWindow   = newRateWindowFunc(s.rateWindow, now)
	)
	return func(yield func(string, streamUsage) bool) {
		s.forEachRLock(func(i int) {
			for tenant, partitions := range s.stripes[i] {
				for _, streams := range partitions {
					for _, stream := range streams {
						if inActiveWindow(stream.lastSeenAt) {
							stream.rateBuckets = rateBucketsInWindow(
								stream.rateBuckets,
								inRateWindow,
							)
							if !yield(tenant, stream) {
								return
							}
						}
					}
				}
			}
		})
	}
}

// TenantActiveStreams returns an iterator over all active streams for the
// tenant. As this method acquires a read lock, the iterator must not block.
func (s *usageStore) TenantActiveStreams(tenant string) iter.Seq2[string, streamUsage] {
	// To prevent time moving forward while iterating, use the current time
	// to check the active and rate window.
	var (
		now            = s.clock.Now()
		inActiveWindow = newActiveWindowFunc(s.activeWindow, now)
		inRateWindow   = newRateWindowFunc(s.rateWindow, now)
	)
	return func(yield func(string, streamUsage) bool) {
		s.withRLock(tenant, func(i int) {
			for _, streams := range s.stripes[i][tenant] {
				for _, stream := range streams {
					if inActiveWindow(stream.lastSeenAt) {
						stream.rateBuckets = rateBucketsInWindow(
							stream.rateBuckets,
							inRateWindow,
						)
						if !yield(tenant, stream) {
							return
						}
					}
				}
			}
		})
	}
}

// Get returns the stream for the tenant. It returns false if the stream has
// expired or does not exist.
func (s *usageStore) Get(tenant string, streamHash uint64) (streamUsage, bool) {
	var (
		now            = s.clock.Now()
		inActiveWindow = newActiveWindowFunc(s.activeWindow, now)
		inRateWindow   = newRateWindowFunc(s.rateWindow, now)
		partition      = s.getPartitionForHash(streamHash)
		stream         streamUsage
		ok             bool
	)
	s.withRLock(tenant, func(i int) {
		stream, ok = s.get(i, tenant, partition, streamHash)
		if !ok {
			return
		}
		if !inActiveWindow(stream.lastSeenAt) {
			ok = false
			return
		}
		stream.rateBuckets = rateBucketsInWindow(stream.rateBuckets, inRateWindow)
	})
	return stream, ok
}

func (s *usageStore) Update(tenant string, metadata *proto.StreamMetadata, seenAt time.Time) error {
	var (
		now            = s.clock.Now()
		inActiveWindow = newActiveWindowFunc(s.activeWindow, now)
	)
	if !inActiveWindow(seenAt.UnixNano()) {
		return errOutsideActiveWindow
	}
	partition := s.getPartitionForHash(metadata.StreamHash)
	s.withLock(tenant, func(i int) {
		s.update(i, tenant, partition, metadata, seenAt)
	})
	return nil
}

func (s *usageStore) UpdateCond(tenant string, metadata []*proto.StreamMetadata, seenAt time.Time, limits Limits) ([]*proto.StreamMetadata, []*proto.StreamMetadata, error) {
	var (
		now            = s.clock.Now()
		inActiveWindow = newActiveWindowFunc(s.activeWindow, now)
	)
	if !inActiveWindow(seenAt.UnixNano()) {
		return nil, nil, errOutsideActiveWindow
	}
	var (
		accepted   = make([]*proto.StreamMetadata, 0, len(metadata))
		rejected   = make([]*proto.StreamMetadata, 0, len(metadata))
		cutoff     = seenAt.Add(-s.activeWindow).UnixNano()
		maxStreams = uint64(limits.MaxGlobalStreamsPerUser(tenant) / s.numPartitions)
	)
	s.withLock(tenant, func(i int) {
		for _, m := range metadata {
			partition := s.getPartitionForHash(m.StreamHash)
			s.checkInitMap(i, tenant, partition)
			streams := s.stripes[i][tenant][partition]
			stream, ok := streams[m.StreamHash]
			// If the stream does not exist, or exists but has expired,
			// we need to check if accepting it would exceed the maximum
			// stream limit.
			if !ok || stream.lastSeenAt < cutoff {
				if ok {
					// The stream has expired, delete it so it doesn't count
					// towards the active streams.
					delete(streams, m.StreamHash)
				}
				// Get the total number of streams, including expired
				// streams. While we would like to count just the number of
				// active streams, this would mean iterating all streams
				// in the partition which is O(N) instead of O(1). Instead,
				// we accept that expired streams will be counted towards the
				// limit until evicted.
				numStreams := uint64(len(s.stripes[i][tenant][partition]))
				if numStreams >= maxStreams {
					rejected = append(rejected, m)
					continue
				}
			}
			s.update(i, tenant, partition, m, seenAt)
			accepted = append(accepted, m)
		}
	})
	return accepted, rejected, nil
}

// Evict evicts all streams that have not been seen within the window.
func (s *usageStore) Evict() map[string]int {
	cutoff := s.clock.Now().Add(-s.activeWindow).UnixNano()
	evicted := make(map[string]int)
	s.forEachLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for partition, streams := range partitions {
				for streamHash, stream := range streams {
					if stream.lastSeenAt < cutoff {
						delete(s.stripes[i][tenant][partition], streamHash)
						evicted[tenant]++
					}
				}
			}
		}
	})
	return evicted
}

// EvictPartitions evicts all streams for the specified partitions.
func (s *usageStore) EvictPartitions(partitionsToEvict []int32) {
	s.forEachLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for _, partitionToEvict := range partitionsToEvict {
				delete(partitions, partitionToEvict)
			}
			if len(partitions) == 0 {
				delete(s.stripes[i], tenant)
			}
		}
	})
}

// Describe implements [prometheus.Collector].
func (s *usageStore) Describe(descs chan<- *prometheus.Desc) {
	descs <- tenantStreamsDesc
	descs <- tenantActiveStreamsDesc
}

// Collect implements [prometheus.Collector].
func (s *usageStore) Collect(metrics chan<- prometheus.Metric) {
	var (
		cutoff = s.clock.Now().Add(-s.activeWindow).UnixNano()
		active = make(map[string]int)
		total  = make(map[string]int)
	)
	// Count both the total number of active streams and the total number of
	// streams for each tenants.
	s.forEachRLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for _, streams := range partitions {
				for _, stream := range streams {
					total[tenant]++
					if stream.lastSeenAt >= cutoff {
						active[tenant]++
					}
				}
			}
		}
	})
	for tenant, numActiveStreams := range active {
		metrics <- prometheus.MustNewConstMetric(
			tenantActiveStreamsDesc,
			prometheus.GaugeValue,
			float64(numActiveStreams),
			tenant,
		)
	}
	for tenant, numStreams := range total {
		metrics <- prometheus.MustNewConstMetric(
			tenantStreamsDesc,
			prometheus.GaugeValue,
			float64(numStreams),
			tenant,
		)
	}
}

func (s *usageStore) get(i int, tenant string, partition int32, streamHash uint64) (stream streamUsage, ok bool) {
	partitions, ok := s.stripes[i][tenant]
	if !ok {
		return
	}
	streams, ok := partitions[partition]
	if !ok {
		return
	}
	stream, ok = streams[streamHash]
	return
}

func (s *usageStore) update(i int, tenant string, partition int32, metadata *proto.StreamMetadata, seenAt time.Time) {
	s.checkInitMap(i, tenant, partition)
	streamHash, totalSize := metadata.StreamHash, metadata.TotalSize
	// Get the stats for the stream.
	stream, ok := s.stripes[i][tenant][partition][streamHash]
	cutoff := seenAt.Add(-s.activeWindow).UnixNano()
	// If the stream does not exist, or it has expired, reset it.
	if !ok || stream.lastSeenAt < cutoff {
		stream.hash = streamHash
		stream.totalSize = 0
		stream.rateBuckets = make([]rateBucket, s.numBuckets)
	}
	seenAtUnixNano := seenAt.UnixNano()
	if stream.lastSeenAt <= seenAtUnixNano {
		stream.lastSeenAt = seenAtUnixNano
	}
	stream.totalSize += totalSize
	// rate buckets are implemented as a circular list. To update a rate
	// bucket we must first calculate the bucket index.
	bucketNum := seenAtUnixNano / int64(s.bucketSize)
	bucketIdx := int(bucketNum % int64(s.numBuckets))
	bucket := stream.rateBuckets[bucketIdx]
	// Once we have found the bucket, we then need to check if it is an old
	// bucket outside the rate window. If it is, we must reset it before we
	// can re-use it.
	bucketStart := seenAt.Truncate(s.bucketSize).UnixNano()
	if bucket.timestamp < bucketStart {
		bucket.timestamp = bucketStart
		bucket.size = 0
	}
	bucket.size += totalSize
	stream.rateBuckets[bucketIdx] = bucket
	s.stripes[i][tenant][partition][streamHash] = stream
}

// forEachRLock executes fn with a shared lock for each stripe.
func (s *usageStore) forEachRLock(fn func(i int)) {
	for i := range s.stripes {
		s.locks[i].RLock()
		fn(i)
		s.locks[i].RUnlock()
	}
}

// forEachLock executes fn with an exclusive lock for each stripe.
func (s *usageStore) forEachLock(fn func(i int)) {
	for i := range s.stripes {
		s.locks[i].Lock()
		fn(i)
		s.locks[i].Unlock()
	}
}

// withRLock executes fn with a shared lock on the stripe.
func (s *usageStore) withRLock(tenant string, fn func(i int)) {
	i := s.getStripe(tenant)
	s.locks[i].RLock()
	defer s.locks[i].RUnlock()
	fn(i)
}

// withLock executes fn with an exclusive lock on the stripe.
func (s *usageStore) withLock(tenant string, fn func(i int)) {
	i := s.getStripe(tenant)
	s.locks[i].Lock()
	defer s.locks[i].Unlock()
	fn(i)
}

// getStripe returns the stripe index for the tenant.
func (s *usageStore) getStripe(tenant string) int {
	h := fnv.New32()
	_, _ = h.Write([]byte(tenant))
	return int(h.Sum32() % uint32(len(s.locks)))
}

// getPartitionForHash returns the partition for the hash.
func (s *usageStore) getPartitionForHash(hash uint64) int32 {
	return int32(hash % uint64(s.numPartitions))
}

// checkInitMap checks if the maps for the tenant and partition are
// initialized, and if not, initializes them. It must not be called without
// the stripe lock for i.
func (s *usageStore) checkInitMap(i int, tenant string, partition int32) {
	if _, ok := s.stripes[i][tenant]; !ok {
		s.stripes[i][tenant] = make(tenantUsage)
	}
	if _, ok := s.stripes[i][tenant][partition]; !ok {
		s.stripes[i][tenant][partition] = make(map[uint64]streamUsage)
	}
}

// Used in tests. Is not goroutine-safe.
func (s *usageStore) getForTests(tenant string, streamHash uint64) (streamUsage, bool) {
	partition := s.getPartitionForHash(streamHash)
	i := s.getStripe(tenant)
	return s.get(i, tenant, partition, streamHash)
}

// Used in tests. Is not goroutine-safe.
func (s *usageStore) setForTests(tenant string, stream streamUsage) {
	partition := s.getPartitionForHash(stream.hash)
	s.withLock(tenant, func(i int) {
		s.checkInitMap(i, tenant, partition)
		s.stripes[i][tenant][partition][stream.hash] = stream
	})
}

// rateBucketsInWindow returns the buckets within the rate window.
func rateBucketsInWindow(buckets []rateBucket, withinRateWindow func(int64) bool) []rateBucket {
	result := make([]rateBucket, 0, len(buckets))
	for _, bucket := range buckets {
		if withinRateWindow(bucket.timestamp) {
			result = append(result, bucket)
		}
	}
	return result
}

// newActiveWindowFunc returns a func that returns true if t is within
// the active window. It memoizes the start of the active time window.
func newActiveWindowFunc(activeWindow time.Duration, now time.Time) func(t int64) bool {
	activeWindowStart := now.Add(-activeWindow).UnixNano()
	return func(t int64) bool {
		return activeWindowStart <= t
	}
}

// inActiveWindow returns true if t is within the active window.
func inActiveWindow(now time.Time, activeWindow time.Duration, t int64) bool {
	return now.Add(-activeWindow).UnixNano() <= t
}

// newRateWindowFunc returns a func that returns true if t is within
// the rate window. It memoizes the start of the rate time window.
func newRateWindowFunc(rateWindow time.Duration, now time.Time) func(t int64) bool {
	rateWindowStart := now.Add(-rateWindow).UnixNano()
	return func(t int64) bool {
		return rateWindowStart <= t
	}
}

// inRateWindow returns true if t is within the rate window.
func inRateWindow(now time.Time, rateWindow time.Duration, t int64) bool {
	return now.Add(-rateWindow).UnixNano() <= t
}
