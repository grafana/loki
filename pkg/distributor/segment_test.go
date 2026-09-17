package distributor

import (
	"bytes"
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor/rendezvous"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// newPartitionRingWatcher creates a PartitionRingWatcher backed by an in-memory KV
// store seeded with the given active partition IDs. Passing no IDs produces a
// watcher whose sharder has zero active partitions.
func newPartitionRingWatcher(t *testing.T, partitionIDs ...int32) *rendezvous.PartitionRingWatcher {
	t.Helper()
	kvClient, closer := consul.NewInMemoryClient(ring.GetPartitionRingCodec(), log.NewNopLogger(), nil)
	t.Cleanup(func() { _ = closer.Close() })

	desc := ring.PartitionRingDesc{
		Partitions: make(map[int32]ring.PartitionDesc, len(partitionIDs)),
		Owners:     map[string]ring.OwnerDesc{},
	}
	for _, id := range partitionIDs {
		desc.Partitions[id] = ring.PartitionDesc{
			Id:             id,
			Tokens:         []uint32{uint32(id)},
			State:          ring.PartitionActive,
			StateTimestamp: time.Now().Unix(),
		}
	}
	err := kvClient.CAS(context.Background(), "test-ring", func(_ interface{}) (interface{}, bool, error) {
		return &desc, true, nil
	})
	require.NoError(t, err)

	watcher := rendezvous.New(rendezvous.Config{Key: "test-ring"}, kvClient, log.NewNopLogger())
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), watcher))
	t.Cleanup(func() { _ = services.StopAndAwaitTerminated(context.Background(), watcher) })
	return watcher
}

func newUninitialisedPartitionRingWatcher() *rendezvous.PartitionRingWatcher {
	return rendezvous.New(rendezvous.Config{Key: "test-ring"}, nil, log.NewNopLogger())
}

func TestGetSegmentationKey(t *testing.T) {
	t.Run("stream without labels", func(t *testing.T) {
		key, err := getSegmentationKey(KeyedStream{})
		require.NoError(t, err)
		require.Equal(t, segmentationKey("unknown_service"), key)
	})

	t.Run("stream with invalid labels", func(t *testing.T) {
		key, err := getSegmentationKey(KeyedStream{
			Stream: logproto.Stream{
				Labels: "{",
			},
		})
		require.EqualError(t, err, "1:2: parse error: unexpected end of input inside braces")
		require.Equal(t, segmentationKey(""), key)
	})

	t.Run("stream with service_name", func(t *testing.T) {
		key, err := getSegmentationKey(KeyedStream{
			Stream: logproto.Stream{
				Labels: "{service_name=\"foo\"}",
			},
		})
		require.NoError(t, err)
		require.Equal(t, segmentationKey("foo"), key)
	})

	t.Run("stream without service_name", func(t *testing.T) {
		key, err := getSegmentationKey(KeyedStream{
			Stream: logproto.Stream{
				Labels: "{bar=\"baz\"}",
			},
		})
		require.NoError(t, err)
		require.Equal(t, segmentationKey("unknown_service"), key)
	})
}

func TestSegmentationKey_Sum64(t *testing.T) {
	k1 := segmentationKey("")
	require.Equal(t, uint64(6134230144364956955), k1.Sum64())
	k2 := segmentationKey("abc")
	require.Equal(t, uint64(5348611747852513221), k2.Sum64())
	// The same key always produces the same 64 bit sum.
	k3 := segmentationKey("abc")
	require.Equal(t, k2.Sum64(), k3.Sum64())
}

func TestSegmentationPartitionResolver_Resolve(t *testing.T) {
	t.Run("returns error if no active partitions", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		watcher := newPartitionRingWatcher(t) // empty ring — zero active partitions
		resolver := newSegmentationPartitionResolver(1024, true, nil, watcher, reg, log.NewNopLogger())
		_, err := resolver.Resolve("tenant", segmentationKey("test"), 0x1, 0, 0)
		require.EqualError(t, err, "no active partitions")

		require.NoError(t, testutil.GatherAndCompare(reg, bytes.NewBufferString(`
			# HELP loki_distributor_segmentation_partition_resolver_keys_rate_absent_total Total number of segmentation keys that we skipped shuffle sharding on segmentation key for due to absent rate.
			# TYPE loki_distributor_segmentation_partition_resolver_keys_rate_absent_total counter
			loki_distributor_segmentation_partition_resolver_keys_rate_absent_total 1
			# HELP loki_distributor_segmentation_partition_resolver_keys_failed_total Total number of segmentation keys that could not be resolved.
			# TYPE loki_distributor_segmentation_partition_resolver_keys_failed_total counter
			loki_distributor_segmentation_partition_resolver_keys_failed_total 1
			# HELP loki_distributor_segmentation_partition_resolver_keys_total Total number of segmentation keys passed to the resolver.
			# TYPE loki_distributor_segmentation_partition_resolver_keys_total counter
			loki_distributor_segmentation_partition_resolver_keys_total 1
		`),
			"loki_distributor_segmentation_partition_resolver_keys_rate_absent_total",
			"loki_distributor_segmentation_partition_resolver_keys_failed_total",
			"loki_distributor_segmentation_partition_resolver_keys_total",
		))
	})

	t.Run("resolves to correct partition when rate is unknown", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		watcher := newPartitionRingWatcher(t, 1)
		resolver := newSegmentationPartitionResolver(1024, true, nil, watcher, reg, log.NewNopLogger())
		partition, err := resolver.Resolve("tenant", "test", 0x1, 0, 0)
		require.NoError(t, err)
		// Should return partition 1 since that is the only active partition.
		require.Equal(t, int32(1), partition)

		require.NoError(t, testutil.GatherAndCompare(reg, bytes.NewBufferString(`
			# HELP loki_distributor_segmentation_partition_resolver_keys_rate_absent_total Total number of segmentation keys that we skipped shuffle sharding on segmentation key for due to absent rate.
			# TYPE loki_distributor_segmentation_partition_resolver_keys_rate_absent_total counter
			loki_distributor_segmentation_partition_resolver_keys_rate_absent_total 1
			# HELP loki_distributor_segmentation_partition_resolver_keys_failed_total Total number of segmentation keys that could not be resolved.
			# TYPE loki_distributor_segmentation_partition_resolver_keys_failed_total counter
			loki_distributor_segmentation_partition_resolver_keys_failed_total 0
			# HELP loki_distributor_segmentation_partition_resolver_keys_total Total number of segmentation keys passed to the resolver.
			# TYPE loki_distributor_segmentation_partition_resolver_keys_total counter
			loki_distributor_segmentation_partition_resolver_keys_total 1
		`),
			"loki_distributor_segmentation_partition_resolver_keys_rate_absent_total",
			"loki_distributor_segmentation_partition_resolver_keys_failed_total",
			"loki_distributor_segmentation_partition_resolver_keys_total",
		))
	})

	t.Run("resolution is deterministic for same inputs", func(t *testing.T) {
		watcher := newPartitionRingWatcher(t, 1, 2, 3)
		resolver := newSegmentationPartitionResolver(1024, true, nil, watcher, prometheus.NewRegistry(), log.NewNopLogger())
		p1, err := resolver.Resolve("tenant-a", "svc", 0x1, 0, 0)
		require.NoError(t, err)
		p2, err := resolver.Resolve("tenant-a", "svc", 0x1, 0, 0)
		require.NoError(t, err)
		require.Equal(t, p1, p2)
	})

	t.Run("uninitialised ring returns error", func(t *testing.T) {
		watcher := newUninitialisedPartitionRingWatcher()
		resolver := newSegmentationPartitionResolver(1024, true, nil, watcher, prometheus.NewRegistry(), log.NewNopLogger())
		_, err := resolver.Resolve("tenant", "test", 0x1, 0, 0)
		require.EqualError(t, err, "no active partitions")
	})
}

func TestSegmentationPartitionResolver_TenantShuffleShard(t *testing.T) {
	watcher := newPartitionRingWatcher(t, 1, 2, 3, 4, 5)
	resolver := newSegmentationPartitionResolver(1024, true, nil, watcher, prometheus.NewRegistry(), log.NewNopLogger())

	tests := []struct {
		rateBytes            uint64
		tenantRateLimitBytes uint64
		expectedNumShards    int
	}{
		{500, 500, 1},
		{0, 0, 5},
		{1_000_000, 0, 5},
		{1500, 1500, 2},
		{3000, 1500, 2},
		{4000, 3000, 3},
		{1500, 4000, 2},
		{1_000_000, 1_000_000, 5},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("perPartitionRateBytes=1024,rateBytes=%d,tenantRateLimitBytes=%d", test.rateBytes, test.tenantRateLimitBytes), func(t *testing.T) {
			seen := make(map[int32]struct{}, 2)
			for i := 0; i < 100; i++ {
				partition, err := resolver.Resolve("tenant", "test", uint32(i), test.rateBytes, test.tenantRateLimitBytes)
				require.NoError(t, err)
				seen[partition] = struct{}{}
				require.InDelta(t, 3, partition, 2)
			}
			require.Len(t, seen, test.expectedNumShards)
		})
	}
}

// buildResolvers constructs a rendezvous and consistent-hashing segmentationPartitionResolver
// backed by the same set of partition IDs. Both are ready to use for uniformity tests.
func buildResolvers(t *testing.T, partitionIDs []int32, perPartitionRateBytes uint64) (rendezvousResolver, consistentResolver *segmentationPartitionResolver) {
	t.Helper()

	chRingDesc := ring.PartitionRingDesc{
		Partitions: make(map[int32]ring.PartitionDesc, len(partitionIDs)),
		Owners:     make(map[string]ring.OwnerDesc, len(partitionIDs)),
	}
	tokenSpacing := uint64(math.MaxUint32) / uint64(len(partitionIDs))
	for i, id := range partitionIDs {
		chRingDesc.Partitions[id] = ring.PartitionDesc{
			Id:             id,
			Tokens:         []uint32{uint32(uint64(i) * tokenSpacing)},
			State:          ring.PartitionActive,
			StateTimestamp: time.Now().Unix(),
		}
		chRingDesc.Owners[fmt.Sprintf("owner-%d", i)] = ring.OwnerDesc{
			OwnedPartition:   id,
			State:            ring.OwnerActive,
			UpdatedTimestamp: time.Now().Unix(),
		}
	}
	chRing, err := ring.NewPartitionRing(chRingDesc)
	require.NoError(t, err)

	rendezvousResolver = newSegmentationPartitionResolver(
		perPartitionRateBytes, true, nil,
		newPartitionRingWatcher(t, partitionIDs...),
		prometheus.NewRegistry(), log.NewNopLogger(),
	)
	consistentResolver = newSegmentationPartitionResolver(
		perPartitionRateBytes, false,
		mockPartitionRingReader{ring: chRing}, nil,
		prometheus.NewRegistry(), log.NewNopLogger(),
	)
	return rendezvousResolver, consistentResolver
}

type uniformityStream struct {
	tenant    string
	segKey    segmentationKey
	hashKey   uint32
	rateBytes uint64
	sizeBytes float64 // actual data volume, independent of rateBytes used for routing
}

// measureEntropy resolves every stream against the given resolver, accumulates
// sizeBytes per partition, and returns the normalized Shannon entropy.
func measureEntropy(t *testing.T, resolver *segmentationPartitionResolver, partitionIDs []int32, streams []uniformityStream, tenantRateLimitBytes uint64) float64 {
	t.Helper()
	partitionLoad := make(map[int32]float64, len(partitionIDs))
	for _, id := range partitionIDs {
		partitionLoad[id] = 0
	}
	for _, s := range streams {
		partition, err := resolver.Resolve(s.tenant, s.segKey, s.hashKey, s.rateBytes, tenantRateLimitBytes)
		require.NoError(t, err)
		partitionLoad[partition] += s.sizeBytes
	}

	total := 0.0
	for _, load := range partitionLoad {
		total += load
	}
	entropy := 0.0
	for _, load := range partitionLoad {
		if load > 0 {
			p := load / total
			entropy -= p * math.Log2(p)
		}
	}
	maxEntropy := math.Log2(float64(len(partitionIDs)))
	normalized := entropy / maxEntropy

	minLoad, maxLoad := math.MaxFloat64, 0.0
	for _, load := range partitionLoad {
		if load < minLoad {
			minLoad = load
		}
		if load > maxLoad {
			maxLoad = load
		}
	}
	t.Logf("total=%.1f MB/s  min=%.1f MB/s  max=%.1f MB/s  ratio=%.2f  entropy=%.4f",
		total/1e6, minLoad/1e6, maxLoad/1e6, maxLoad/minLoad, normalized)
	return normalized
}

// TestSegmentationPartitionResolver_ThroughputUniformity measures how evenly each
// implementation distributes ingestion load across partitions using Shannon entropy.
// Both resolvers receive the same (tenant, segmentation key) pairs with random
// throughputs, letting us compare their uniformity directly.
// Normalized entropy (H / log2(numPartitions)) of 1.0 means perfectly uniform load.
func TestSegmentationPartitionResolver_ThroughputUniformity(t *testing.T) {
	const (
		numPartitions        = 10
		numTenants           = 1
		numSegKeys           = 100 // per tenant — enough to cover all partitions reliably
		perPartitionRateMBps = 10
		numTenantPartitions  = 10 // partitions each tenant's rate limit maps to
		minThroughputMBps    = 1
		maxThroughputMBps    = 50
		minNormalizedEntropy = 0.95
	)

	perPartitionRateBytes := uint64(perPartitionRateMBps * 1024 * 1024)
	tenantRateLimitBytes := uint64(numTenantPartitions * perPartitionRateMBps * 1024 * 1024)

	partitionIDs := make([]int32, numPartitions)
	for i := range numPartitions {
		partitionIDs[i] = int32(i)
	}
	rendezvousRes, consistentRes := buildResolvers(t, partitionIDs, perPartitionRateBytes)

	rng := rand.New(rand.NewSource(42))
	streams := make([]uniformityStream, 0, numTenants*numSegKeys)
	for ti := range numTenants {
		for si := range numSegKeys {
			throughputMBps := minThroughputMBps + rng.Intn(maxThroughputMBps-minThroughputMBps+1)
			rate := uint64(throughputMBps) * 1024 * 1024
			streams = append(streams, uniformityStream{
				tenant:    fmt.Sprintf("tenant-%d", ti),
				segKey:    segmentationKey(fmt.Sprintf("segkey-%d", si)),
				hashKey:   fnv32(ti*numSegKeys + si),
				rateBytes: rate,
				sizeBytes: float64(rate),
			})
		}
	}

	for _, tc := range []struct {
		name                 string
		resolver             *segmentationPartitionResolver
		minNormalizedEntropy float64
	}{
		{"rendezvous", rendezvousRes, minNormalizedEntropy},
		// Consistent hashing included for comparison only — no minimum threshold.
		{"consistent", consistentRes, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			normalized := measureEntropy(t, tc.resolver, partitionIDs, streams, tenantRateLimitBytes)
			if tc.minNormalizedEntropy > 0 {
				require.GreaterOrEqual(t, normalized, tc.minNormalizedEntropy,
					"throughput should be uniformly distributed across partitions")
			}
		})
	}
}

// TestSegmentationPartitionResolver_ZeroRate_DistributesByHashKey verifies that
// rendezvous hashing distributes streams by their hash key when rateBytes=0.
func TestSegmentationPartitionResolver_ZeroRate_DistributesByHashKey(t *testing.T) {
	const (
		numPartitions        = 10
		numStreams           = 200
		minNormalizedEntropy = 0.9
	)

	partitionIDs := make([]int32, numPartitions)
	for i := range numPartitions {
		partitionIDs[i] = int32(i)
	}
	rendezvousRes, consistentRes := buildResolvers(t, partitionIDs, 10*1024*1024)

	rng := rand.New(rand.NewSource(42))
	streams := make([]uniformityStream, numStreams)
	for i := range numStreams {
		streams[i] = uniformityStream{
			tenant:    "tenant",
			segKey:    "my-service", // all streams share one segkey
			hashKey:   rng.Uint32(), // but each has a distinct label hash
			rateBytes: 0,            // unknown — simulates fresh deployment
			sizeBytes: 1,
		}
	}

	for _, tc := range []struct {
		name     string
		resolver *segmentationPartitionResolver
	}{
		{"rendezvous", rendezvousRes},
		{"consistent", consistentRes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			normalized := measureEntropy(t, tc.resolver, partitionIDs, streams, 0)
			require.GreaterOrEqual(t, normalized, float64(minNormalizedEntropy),
				"streams with rateBytes=0 must spread across partitions by hash key; "+
					"before the fix all streams for a segkey collapsed to one partition")
		})
	}
}

// fnv32 hashes an integer to a well-distributed uint32, suitable for use as a
// stream hash key in tests. Using raw sequential values is not representative —
// they cluster near 0 and defeat consistent hashing's clockwise ring walk.
func fnv32(n int) uint32 {
	h := fnv.New32a()
	b := [4]byte{byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24)}
	h.Write(b[:])
	return h.Sum32()
}
