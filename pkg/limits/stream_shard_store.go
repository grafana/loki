package limits

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"time"

	"github.com/coder/quartz"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

var (
	streamShardTrackedStreamsDesc = prometheus.NewDesc(
		"loki_ingest_limits_stream_shard_tracked_streams",
		"The current number of logical (pre-shard) streams tracked for stream sharding per tenant.",
		[]string{"tenant"},
		nil,
	)
	streamShardTotalStreamsDesc = prometheus.NewDesc(
		"loki_ingest_limits_stream_shard_total_streams",
		"The current number of physical streams (unsharded streams plus the shards of sharded streams) per tenant implied by the granted shard counts. Compare against loki_ingester_memory_streams for the physical stream count the ingesters see.",
		[]string{"tenant"},
		nil,
	)
	streamShardTotalShardsDesc = prometheus.NewDesc(
		"loki_ingest_limits_stream_shard_total_shards",
		"The current number of physical streams that belong to a sharded stream per tenant. Subtracting this from loki_ingest_limits_stream_shard_total_streams gives the number of unsharded streams.",
		[]string{"tenant"},
		nil,
	)
)

// streamShardStore decides how many shards a stream should use and tracks the
// physical stream footprint those decisions imply. It is separate from
// usageStore, with its own maps and its own eviction, so it cannot regress
// stream count enforcement in usageStore.
//
// The store is in-memory and per-instance. A restart or a partition rebalance
// loses the rate history of the affected streams, which then warm up again
// from scratch (see the brand-new or expired case in checkAndShard).
//
// Shard growth is capped against max_global_streams_per_user using only the
// streams this store has evaluated itself. A tenant whose streams predate the
// store warms up until it has seen them, so the cap can be looser than
// reality during that window.
type streamShardStore struct {
	activeWindow  time.Duration
	rateWindow    time.Duration
	bucketSize    time.Duration
	numBuckets    int
	numPartitions int
	// zone is this instance's zone. It tells a merged record apart from
	// another zone's, see merge.
	zone string
	// durabilityEnabled makes checkAndShard return the records that keep the
	// rate buckets across restarts, see [Config.StreamShardingDurabilityEnabled].
	durabilityEnabled bool

	stripes []map[string]streamShardTenantUsage
	locks   []stripeLock

	limits Limits

	// Used in tests to fake current time
	clock quartz.Clock
}

// streamShardTenantUsage holds the per-tenant state, partition to policy
// bucket, mirroring usageStore's tenantUsage shape.
type streamShardTenantUsage map[int32]map[string]*streamShardPolicyUsage

// streamShardPolicyUsage is the state tracked for one policy bucket of one
// partition of one tenant.
type streamShardPolicyUsage struct {
	streams map[uint64]streamShardUsage

	// slots is the sum of the streams' slots. It is the budget the bucket
	// consumes of the tenant's max streams limit, maintained as streams are
	// granted shards and as they are evicted, so that a push does not have to
	// scan the bucket to find out what budget is left. A stream's slots are
	// recomputed when it is pushed to or evicted, so between those points the
	// total does not reflect shards that have since expired.
	slots uint64
}

// streamShardUsage is the state tracked for a single logical (pre-shard)
// stream.
type streamShardUsage struct {
	hash        uint64
	lastSeenAt  int64
	shardCount  uint32
	policy      string
	rateBuckets []shardRateBucket

	// lastProducedBucket is the start of the most recent rate bucket written
	// to the metadata topic, and bounds the records this stream produces to
	// one per bucket.
	lastProducedBucket int64

	// remoteBuckets holds the rate buckets of the other zones, keyed by zone,
	// as merged from their records. Rates are kept per zone rather than as a
	// single aggregate so that merging a record is an assignment and not an
	// addition, which is what makes replay idempotent.
	//
	// It is nil until another zone's record is merged. The frontend tries
	// zones in a fixed order, so in steady state a stream is answered by one
	// zone, which pays for the local ring only, while the other zones pay for
	// one map entry each.
	remoteBuckets map[string][]shardRateBucket

	// slots is the budget this stream consumes: one per live shard, with a
	// floor of one so a tracked unsharded stream still counts as one stream.
	slots uint64

	// shardLastUsed holds, for shard i, the time at which shard i was last
	// covered by a granted shard count. It tracks the physical footprint of
	// past decisions, which is not the same as the current recommendation: a
	// shard the recommendation stops covering keeps its timestamp and expires
	// on its own after activeWindow, the way a sharded sub-stream keeps
	// existing at the ingesters after the rate drops. Entry i is refreshed
	// only when every lower entry is, so the live shards are a prefix.
	shardLastUsed []int64
}

// shardRateBucket is a rate bucket for the shard decision. It counts pushes
// as well as bytes, because the shard count amortizes the current push over
// the push rate the way the distributor's local rate store does.
// This struct is mirrorint [rateBucket].
type shardRateBucket struct {
	timestamp int64 // start of the interval
	size      uint64
	pushes    uint64
}

func newStreamShardStore(activeWindow, rateWindow, bucketSize time.Duration, numPartitions int, zone string, durabilityEnabled bool, limits Limits, reg prometheus.Registerer) (*streamShardStore, error) {
	s := &streamShardStore{
		activeWindow:      activeWindow,
		rateWindow:        rateWindow,
		bucketSize:        bucketSize,
		numBuckets:        int(rateWindow / bucketSize),
		numPartitions:     numPartitions,
		zone:              zone,
		durabilityEnabled: durabilityEnabled,
		stripes:           make([]map[string]streamShardTenantUsage, numStripes),
		locks:             make([]stripeLock, numStripes),
		limits:            limits,
		clock:             quartz.NewReal(),
	}
	for i := range s.stripes {
		s.stripes[i] = make(map[string]streamShardTenantUsage)
	}
	if err := reg.Register(s); err != nil {
		return nil, fmt.Errorf("failed to register metrics: %w", err)
	}
	return s, nil
}

// checkAndShard returns the shard count for each stream in metadata:
//
//   - A brand-new or expired stream gets one shard, as there is no rate
//     history to justify more. It is rejected outright, with zero shards, if
//     the tenant has no room left for even one stream.
//   - A stream whose policy has sharding disabled gets one shard and is not
//     tracked.
//   - Any other stream gets the shard count justified by its rate over the
//     rate window plus this push amortized over its push rate, capped by
//     what the other streams leave of the tenant's stream count budget. The
//     count shrinks freely when the rate drops, but the shards it stops
//     covering keep counting against the budget until they expire.
//   - A stream with no traffic in the rate window, whether it has never been
//     observed or has gone idle, gets one shard. There is no rate to
//     amortize this push over, so nothing distinguishes a burst from
//     sustained load.
//
// The second return value holds the records the caller should produce to keep
// the decisions durable, and is always empty when durability is disabled.
func (s *streamShardStore) checkAndShard(ctx context.Context, tenant string, metadata []*proto.StreamMetadata, seenAt time.Time) ([]*proto.StreamShardResult, []*proto.StreamMetadataRecord) {
	var (
		cutoff    = seenAt.Add(-s.activeWindow).UnixNano()
		results   = make([]*proto.StreamShardResult, 0, len(metadata))
		toProduce []*proto.StreamMetadataRecord
	)
	s.withLock(tenant, func(i int) {
		for _, m := range metadata {
			if ctx.Err() != nil {
				return
			}
			partition := s.getPartitionForHash(m.StreamHash)
			shardCfg, _ := s.limits.PolicyShardStreams(tenant, m.IngestionPolicy)
			policyBucket, maxStreams := getPolicyBucketAndStreamsLimit(s.limits, s.numPartitions, tenant, m.IngestionPolicy)
			bucket := s.checkInitMap(i, tenant, partition, policyBucket)

			if !shardCfg.Enabled {
				// Drop any tracked state so it stops consuming budget: a
				// stream whose policy flipped to disabled must not keep its
				// stale shards.
				bucket.remove(m.StreamHash)
				results = append(results, &proto.StreamShardResult{
					StreamHash: m.StreamHash,
					Shards:     1,
				})
				continue
			}

			existing, ok := bucket.streams[m.StreamHash]
			isNewOrExpired := !ok || existing.lastSeenAt < cutoff
			if isNewOrExpired {
				// Drop the expired entry now. If this push ends up rejected
				// it must not linger, and keep counting against the other
				// streams, until the next eviction sweep.
				bucket.remove(m.StreamHash)
				existing = streamShardUsage{}
			}

			// budget is the most live shards this stream may hold, and is
			// only enforced when maxStreams is not 0: maxStreams minus the
			// slots the other streams in the bucket consume. Excluding this
			// stream lets it keep the shards it already holds for free.
			budget := max(0, int64(maxStreams)-int64(bucket.slots-existing.slots))

			var (
				stream streamShardUsage
				// desired is the shard count the rate justifies, before the budget cap.
				desired uint32
				// evaluatedRate is the byte rate that drove the decision.
				// It stays 0 when no rate was computed.
				evaluatedRate uint64
			)
			switch {
			case isNewOrExpired:
				if maxStreams > 0 && budget < 1 {
					results = append(results, &proto.StreamShardResult{
						StreamHash:   m.StreamHash,
						Shards:       0,
						RejectReason: ReasonMaxStreams.String(),
					})
					continue
				}
				// Reset, mirroring usageStore.update's reset on expiry. There
				// is no rate history, so never shard on first sight.
				stream = streamShardUsage{hash: m.StreamHash, policy: policyBucket}
				// Seed the first rate bucket with this push even though the
				// decision stays at one shard. Otherwise these bytes never
				// reach any rate computation, and only the stream's second
				// push initializes the buckets, which biases the rate low for
				// bursty or infrequently pushed streams.
				s.updateRateBucket(&stream, m.TotalSize, seenAt)
				desired = 1
			default:
				stream = existing
				// The rate must be read before this push is recorded. This
				// push does count towards its own shard count, but through
				// the amortization term below, which is how shardCountFor
				// counts it: the local rate store is fed by the ingesters
				// and so always lags the push being decided on. Letting the
				// push into the rate as well would charge it twice, and at
				// very different weights, as the rate spreads it over the
				// whole rate window while the amortization term adds up to
				// the whole push.
				var pushRate float64
				window := clampShardRateWindow(shardCfg.LimitsServiceStreamShardingRateWindow, s.bucketSize, s.rateWindow)
				evaluatedRate, pushRate = existing.currentRate(seenAt, window)
				s.updateRateBucket(&stream, m.TotalSize, seenAt)
				if pushRate == 0 {
					// No traffic observed in the rate window, so there is no
					// push rate to amortize this push over and no rate to add
					// it to. shardCountFor short-circuits the same case with
					// "first push, don't shard until the rate is understood".
					desired = 1
					break
				}
				// Amortize this push the way shardCountFor does: add
				// totalSize * min(1, pushRate). Capping the push rate at 1
				// adds the whole push for a frequently pushed stream but
				// only a fraction of it for an infrequent one, so a single
				// large push does not over-shard it.
				amortizedRate := evaluatedRate + uint64(float64(m.TotalSize)*min(1, pushRate))
				desired = max(1, ceilDivU32(amortizedRate, uint64(shardCfg.DesiredRate.Val())))
			}

			// Cap the count to the budget, but never below one shard: an
			// accepted stream is written whatever happens, and zero shards is
			// reserved for a rejection. A tracked stream can be left without
			// any budget of its own after max_global_streams_per_user is
			// lowered while its bucket is full.
			granted := desired
			if maxStreams != 0 && int64(desired) > budget {
				granted = uint32(max(1, budget))
			}
			reason := ReasonUnknown
			if granted < desired {
				reason = ReasonStreamShardsCapped
			}

			stream.hash = m.StreamHash
			stream.policy = policyBucket
			stream.shardCount = granted
			stream.lastSeenAt = max(seenAt.UnixNano(), stream.lastSeenAt)
			stream.shardLastUsed = refreshLiveShards(stream.shardLastUsed, granted, seenAt.UnixNano())
			if rec := s.recordToProduce(&stream, tenant, m, seenAt); rec != nil {
				toProduce = append(toProduce, rec)
			}
			bucket.put(stream, cutoff)

			results = append(results, &proto.StreamShardResult{
				StreamHash: m.StreamHash,
				Shards:     granted,
				Stats: &proto.ShardStats{
					ShardDecisionContext: uint32(reason),
					EvaluatedRate:        evaluatedRate,
				},
			})
		}
	})
	return results, toProduce
}

// recordToProduce returns the record for the newest complete rate bucket the
// stream has not published yet, or nil when there is nothing new to produce,
// and marks that bucket as produced.
//
// A complete bucket is published rather than the one in flight, so that a
// stream produces one record per bucket and the value it carries is final.
// The cost is that a restart loses up to one bucket of the newest traffic,
// which understates the rate by at most one bucket's share of the rate
// window. Publishing the bucket in flight instead would either cost a record
// per push or, throttled to one record per bucket, publish a value that
// stops at the bucket's first push.
//
// The bucket published is the one holding the previous push, which is not
// always the immediate predecessor of the current one: a stream pushed to
// less often than once per bucket leaves gaps. Publishing only the immediate
// predecessor would drop every bucket of such a stream, so it would never
// restore any rate at all. Since a push publishes at most one bucket either
// way, closing the gap costs no extra records.
func (s *streamShardStore) recordToProduce(stream *streamShardUsage, tenant string, m *proto.StreamMetadata, seenAt time.Time) *proto.StreamMetadataRecord {
	if !s.durabilityEnabled {
		return nil
	}
	lastComplete := seenAt.Truncate(s.bucketSize).Add(-s.bucketSize).UnixNano()
	if stream.lastProducedBucket >= lastComplete {
		return nil
	}
	// Scan the ring for the newest bucket that is complete, has traffic, is
	// newer than the last one published, and still falls in the rate window.
	// The window test uses the same cutoff as currentRate and merge, so a
	// bucket a consumer would drop as too old is not produced at all.
	cutoff := seenAt.Add(-s.rateWindow).UnixNano()
	var b shardRateBucket
	for _, candidate := range stream.rateBuckets {
		if candidate.pushes == 0 || candidate.timestamp > lastComplete || candidate.timestamp < cutoff {
			continue
		}
		if candidate.timestamp > stream.lastProducedBucket && candidate.timestamp > b.timestamp {
			b = candidate
		}
	}
	if b.pushes == 0 {
		return nil
	}
	stream.lastProducedBucket = b.timestamp
	return &proto.StreamMetadataRecord{
		Tenant: tenant,
		Metadata: &proto.StreamMetadata{
			StreamHash:      m.StreamHash,
			IngestionPolicy: m.IngestionPolicy,
		},
		ShardRateBucket: &proto.ShardRateBucket{
			BucketStart: b.timestamp,
			Size_:       b.size,
			Pushes:      b.pushes,
		},
		ShardCount: stream.shardCount,
	}
}

// merge applies a record produced by this or another zone, restoring the
// rate history and the shard footprint the producing zone had when it wrote
// the record. It is how a restarted instance, or an instance that has just
// been assigned a partition, avoids the warm-up window in which every stream
// looks brand new and gets a single shard.
//
// The record states the producing zone's absolute totals for one rate bucket,
// so merging is not additive and a record can be applied any number of times.
// Where a bucket is already tracked for that zone, the larger totals win:
// within a bucket a zone's totals only grow, so this is independent of the
// order records arrive in, and it cannot regress a ring that holds fresher
// pushes than the record does. That covers replaying our own records into a
// store that is already serving, and a record produced by the previous owner
// of a partition landing after the new owner's.
//
// Records older than the rate window are ignored: they carry no rate that
// still counts, and tracking the stream for its footprint alone would keep
// idle streams in memory for the rest of the active window. The cost is that
// the stream count budget is not restored for streams that have been idle
// for longer than a rate window.
func (s *streamShardStore) merge(tenant string, rec *proto.StreamMetadataRecord) {
	if rec.Metadata == nil || rec.ShardRateBucket == nil {
		return
	}
	var (
		now         = s.clock.Now()
		bucketStart = rec.ShardRateBucket.BucketStart
	)
	if bucketStart < now.Add(-s.rateWindow).UnixNano() {
		return
	}
	policyBucket, _ := getPolicyBucketAndStreamsLimit(s.limits, s.numPartitions, tenant, rec.Metadata.IngestionPolicy)
	var (
		hash      = rec.Metadata.StreamHash
		partition = s.getPartitionForHash(hash)
		cutoff    = now.Add(-s.activeWindow).UnixNano()
	)
	s.withLock(tenant, func(i int) {
		bucket := s.checkInitMap(i, tenant, partition, policyBucket)
		stream := bucket.streams[hash]
		stream.hash = hash
		stream.policy = policyBucket
		if rec.Zone == s.zone {
			stream.rateBuckets = s.mergeRateBucket(stream.rateBuckets, rec.ShardRateBucket)
			// The topic already holds a record for this bucket, so advance the
			// produce cursor to keep the one-record-per-bucket bound across a
			// restart or a change of partition owner. A record is only ever
			// written for a complete bucket, so this cannot suppress a bucket
			// that is still accumulating pushes.
			stream.lastProducedBucket = max(stream.lastProducedBucket, bucketStart)
		} else {
			if stream.remoteBuckets == nil {
				stream.remoteBuckets = make(map[string][]shardRateBucket, 1)
			}
			stream.remoteBuckets[rec.Zone] = s.mergeRateBucket(stream.remoteBuckets[rec.Zone], rec.ShardRateBucket)
		}
		// The record's shard count is only adopted when the record is newer
		// than anything we know about the stream. Otherwise a replayed record
		// would age the footprint of a stream that has been pushed to since,
		// and shrink it back to a count that has already grown.
		if bucketStart > stream.lastSeenAt {
			stream.lastSeenAt = bucketStart
			stream.shardCount = rec.ShardCount
			stream.shardLastUsed = refreshLiveShards(stream.shardLastUsed, rec.ShardCount, bucketStart)
		}
		bucket.put(stream, cutoff)
	})
}

// mergeRateBucket writes the record's bucket into the ring, allocating it if
// needed, and returns it.
func (s *streamShardStore) mergeRateBucket(buckets []shardRateBucket, in *proto.ShardRateBucket) []shardRateBucket {
	if len(buckets) == 0 {
		buckets = make([]shardRateBucket, s.numBuckets)
	}
	idx := int((in.BucketStart / int64(s.bucketSize)) % int64(s.numBuckets))
	b := buckets[idx]
	switch {
	case b.timestamp < in.BucketStart:
		// A newer bucket reusing the slot: replace it rather than merge.
		b = shardRateBucket{timestamp: in.BucketStart, size: in.Size_, pushes: in.Pushes}
	case b.timestamp == in.BucketStart:
		b.size = max(b.size, in.Size_)
		b.pushes = max(b.pushes, in.Pushes)
	}
	buckets[idx] = b
	return buckets
}

// Evict evicts all streams that have not been seen within the active window,
// mirroring usageStore.Evict. For the streams that survive it also drops the
// shards that have expired on their own, so the tracked footprint, and its
// memory, follow the live shards.
func (s *streamShardStore) Evict() map[string]int {
	cutoff := s.clock.Now().Add(-s.activeWindow).UnixNano()
	evicted := make(map[string]int)
	s.forEachLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for _, policies := range partitions {
				for _, bucket := range policies {
					for streamHash, stream := range bucket.streams {
						if stream.lastSeenAt < cutoff {
							bucket.remove(streamHash)
							evicted[tenant]++
							continue
						}
						// The live shards are the leading entries, so drop
						// the expired remainder.
						if n := int(liveShardCount(stream.shardLastUsed, cutoff)); n < len(stream.shardLastUsed) {
							trimmed := make([]int64, n)
							copy(trimmed, stream.shardLastUsed)
							stream.shardLastUsed = trimmed
						}
						// Recompute the slots either way: shards can expire
						// without the slice shrinking.
						bucket.put(stream, cutoff)
					}
				}
			}
		}
	})
	return evicted
}

// EvictPartitions evicts all streams for the specified partitions, mirroring
// usageStore.EvictPartitions. It is called when this instance is no longer
// assigned those partitions, see partition_lifecycler.go.
func (s *streamShardStore) EvictPartitions(partitionsToEvict []int32) {
	s.forEachLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for _, p := range partitionsToEvict {
				delete(partitions, p)
			}
			if len(partitions) == 0 {
				delete(s.stripes[i], tenant)
			}
		}
	})
}

// Describe implements [prometheus.Collector].
func (s *streamShardStore) Describe(descs chan<- *prometheus.Desc) {
	descs <- streamShardTrackedStreamsDesc
	descs <- streamShardTotalStreamsDesc
	descs <- streamShardTotalShardsDesc
}

// Collect implements [prometheus.Collector]. The physical stream counts are
// the slots the streams consume, which follow the live shard footprint
// rather than the current recommendation, so they track the stream count the
// ingesters see.
func (s *streamShardStore) Collect(metrics chan<- prometheus.Metric) {
	var (
		trackedStreams = make(map[string]int)
		totalStreams   = make(map[string]uint64)
		totalShards    = make(map[string]uint64)
	)
	s.forEachRLock(func(i int) {
		for tenant, partitions := range s.stripes[i] {
			for _, policies := range partitions {
				for _, bucket := range policies {
					trackedStreams[tenant] += len(bucket.streams)
					totalStreams[tenant] += bucket.slots
					for _, stream := range bucket.streams {
						if stream.slots >= 2 {
							totalShards[tenant] += stream.slots
						}
					}
				}
			}
		}
	})
	for tenant, n := range trackedStreams {
		metrics <- prometheus.MustNewConstMetric(streamShardTrackedStreamsDesc, prometheus.GaugeValue, float64(n), tenant)
	}
	for tenant, n := range totalStreams {
		metrics <- prometheus.MustNewConstMetric(streamShardTotalStreamsDesc, prometheus.GaugeValue, float64(n), tenant)
	}
	for tenant, n := range totalShards {
		metrics <- prometheus.MustNewConstMetric(streamShardTotalShardsDesc, prometheus.GaugeValue, float64(n), tenant)
	}
}

func (s *streamShardStore) updateRateBucket(stream *streamShardUsage, sizeDelta uint64, seenAt time.Time) {
	if len(stream.rateBuckets) == 0 {
		stream.rateBuckets = make([]shardRateBucket, s.numBuckets)
	}
	bucketNum := seenAt.UnixNano() / int64(s.bucketSize)
	bucketIdx := int(bucketNum % int64(s.numBuckets))
	b := stream.rateBuckets[bucketIdx]
	bucketStart := seenAt.Truncate(s.bucketSize).UnixNano()
	if b.timestamp < bucketStart {
		b.timestamp = bucketStart
		b.size = 0
		b.pushes = 0
	}
	b.size += sizeDelta
	b.pushes++
	stream.rateBuckets[bucketIdx] = b
}

func (s *streamShardStore) getPartitionForHash(hash uint64) int32 {
	return int32(hash % uint64(s.numPartitions))
}

func (s *streamShardStore) getStripe(tenant string) int {
	h := fnv.New32()
	_, _ = h.Write([]byte(tenant))
	return int(h.Sum32() % uint32(len(s.locks)))
}

func (s *streamShardStore) withLock(tenant string, fn func(i int)) {
	i := s.getStripe(tenant)
	s.locks[i].Lock()
	defer s.locks[i].Unlock()
	fn(i)
}

func (s *streamShardStore) forEachLock(fn func(i int)) {
	for i := range s.stripes {
		s.locks[i].Lock()
		fn(i)
		s.locks[i].Unlock()
	}
}

func (s *streamShardStore) forEachRLock(fn func(i int)) {
	for i := range s.stripes {
		s.locks[i].RLock()
		fn(i)
		s.locks[i].RUnlock()
	}
}

// checkInitMap initializes the maps for the tenant, partition and policy if
// needed and returns the policy bucket. It must not be called without the
// stripe lock.
func (s *streamShardStore) checkInitMap(i int, tenant string, partition int32, policy string) *streamShardPolicyUsage {
	if _, ok := s.stripes[i][tenant]; !ok {
		s.stripes[i][tenant] = make(streamShardTenantUsage)
	}
	if _, ok := s.stripes[i][tenant][partition]; !ok {
		s.stripes[i][tenant][partition] = make(map[string]*streamShardPolicyUsage)
	}
	if _, ok := s.stripes[i][tenant][partition][policy]; !ok {
		s.stripes[i][tenant][partition][policy] = &streamShardPolicyUsage{
			streams: make(map[uint64]streamShardUsage),
		}
	}
	return s.stripes[i][tenant][partition][policy]
}

// put stores stream and keeps the bucket's slot total in step with it. The
// stream's slots are recomputed from its shard footprint as of cutoff, which
// is what keeps a push O(1) in the number of streams in the bucket: the
// bucket total never has to be recomputed by scanning them.
func (b *streamShardPolicyUsage) put(stream streamShardUsage, cutoff int64) {
	previous := b.streams[stream.hash].slots
	stream.slots = max(1, uint64(liveShardCount(stream.shardLastUsed, cutoff)))
	b.slots += stream.slots - previous
	b.streams[stream.hash] = stream
}

// remove deletes the stream, if tracked, and releases its slots.
func (b *streamShardPolicyUsage) remove(streamHash uint64) {
	stream, ok := b.streams[streamHash]
	if !ok {
		return
	}
	b.slots -= stream.slots
	delete(b.streams, streamHash)
}

// liveShardCount returns how many of a stream's shards were last covered by a
// granted shard count within the active window.
func liveShardCount(shardLastUsed []int64, cutoff int64) uint32 {
	var n uint32
	for _, ts := range shardLastUsed {
		if ts >= cutoff {
			n++
		}
	}
	return n
}

// refreshLiveShards marks the shards below count as used at now, growing the
// slice when the granted count reached a new high, and returns it. Entries at
// or above count are left alone so they keep aging towards expiry.
func refreshLiveShards(shardLastUsed []int64, count uint32, now int64) []int64 {
	if int(count) > len(shardLastUsed) {
		grown := make([]int64, count)
		copy(grown, shardLastUsed)
		shardLastUsed = grown
	}
	for i := uint32(0); i < count; i++ {
		shardLastUsed[i] = now
	}
	return shardLastUsed
}

// currentRate returns the windowed average byte rate and push rate per
// second over all zones, using the same approach as Service.UpdateRates.
// Summing the zones gives the stream's full rate even while the frontend is
// spreading its pushes over more than one zone.
func (s streamShardUsage) currentRate(now time.Time, rateWindow time.Duration) (bytesRate uint64, pushRate float64) {
	seconds := rateWindow.Seconds()
	if seconds <= 0 {
		return 0, 0
	}
	cutoff := now.Add(-rateWindow).UnixNano()
	totalBytes, totalPushes := sumRateBuckets(s.rateBuckets, cutoff)
	for _, buckets := range s.remoteBuckets {
		bytes, pushes := sumRateBuckets(buckets, cutoff)
		totalBytes += bytes
		totalPushes += pushes
	}
	return uint64(float64(totalBytes) / seconds), float64(totalPushes) / seconds
}

// sumRateBuckets sums the buckets that start at or after cutoff. The ring
// buffer resets a slot when it is reused, so a slot not touched this window
// still holds stale data that must be skipped rather than counted.
func sumRateBuckets(buckets []shardRateBucket, cutoff int64) (totalBytes, totalPushes uint64) {
	for _, b := range buckets {
		if b.timestamp >= cutoff {
			totalBytes += b.size
			totalPushes += b.pushes
		}
	}
	return totalBytes, totalPushes
}

// clampShardRateWindow resolves the window the rate is averaged over for the
// shard decision: the per-tenant or per-policy override when set, otherwise
// the store's own window. The result is clamped to [bucketSize, rateWindow],
// as the per-stream bucket ring holds no more than rateWindow of history and
// a window shorter than one bucket cannot be measured. A shorter window
// reacts to shorter bursts, closer to the distributor's local rate store.
func clampShardRateWindow(configured, bucketSize, rateWindow time.Duration) time.Duration {
	if configured <= 0 {
		return rateWindow
	}
	return min(max(configured, bucketSize), rateWindow)
}

func ceilDivU32(a, b uint64) uint32 {
	if b == 0 {
		return 1
	}
	// Clamp rather than letting the conversion wrap: a huge rate over a tiny
	// desired rate would otherwise overflow to a small shard count, which
	// under-shards the hottest streams.
	return uint32(min(math.MaxUint32, (a+b-1)/b))
}
