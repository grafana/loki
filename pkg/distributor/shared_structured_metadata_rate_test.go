package distributor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"

	"github.com/grafana/loki/pkg/push"
)

// pooledRateStream is a stream carrying its OTLP resource and scope attributes in a shared pool that
// every entry references, i.e. what a tenant with otlp_defer_structured_metadata_expansion on
// produces.
func pooledRateStream(entries int) logproto.Stream {
	stream := logproto.Stream{Labels: `{app="myapp"}`}
	for i := 0; i < entries; i++ {
		stream.Entries = append(stream.Entries, logproto.Entry{
			Timestamp:          time.Unix(int64(1600000000+i), 0),
			Line:               fmt.Sprintf("log line %d", i),
			StructuredMetadata: push.LabelsAdapter{{Name: "own", Value: "value"}},
		})
	}
	stream.SharedStructuredMetadataSets = sharedTestPool()
	refAllEntries(&stream)
	return stream
}

// expandedRateStream is the same logical payload as pooledRateStream with the same number of
// entries, but pushed the way a tenant with the flag off pushes it: no pool, the resource and scope
// attributes copied into every entry.
func expandedRateStream(entries int) logproto.Stream {
	pooled := pooledRateStream(entries)
	expanded := logproto.Stream{Labels: pooled.Labels}
	for i := range pooled.Entries {
		entry := pooled.Entries[i]
		sm := push.LabelsAdapter{}
		sm = append(sm, entry.StructuredMetadata...)
		sm = append(sm, sharedTestResource()...)
		sm = append(sm, sharedTestScope()...)
		expanded.Entries = append(expanded.Entries, logproto.Entry{
			Timestamp:          entry.Timestamp,
			Line:               entry.Line,
			StructuredMetadata: sm,
		})
	}
	return expanded
}

func segmented(stream logproto.Stream, hash uint64) segmentedStream {
	return segmentedStream{
		KeyedStream:         KeyedStream{Stream: stream, Policy: "default"},
		SegmentationKeyHash: hash,
	}
}

// TestSharedStructuredMetadataExpansionDelta pins the arithmetic: the delta turns a size counting
// the pool once for the whole stream into one charging every entry for the sets it references.
func TestSharedStructuredMetadataExpansionDelta(t *testing.T) {
	const entries = 5

	pooled := pooledRateStream(entries)
	poolSize := util.SharedSetsSize(pooled.SharedStructuredMetadataSets)
	require.NotZero(t, poolSize)

	// Every entry references both sets, so the expanded charge is the pool once per entry, and the
	// delta is that minus the single whole-stream charge already in the base.
	require.Equal(t, uint64(entries*poolSize-poolSize), sharedStructuredMetadataExpansionDelta(pooled))

	// No pool means the two units are the same number, so nothing is added and a flag-off stream is
	// untouched.
	require.Zero(t, sharedStructuredMetadataExpansionDelta(expandedRateStream(entries)))

	// A set nobody references cannot make the delta negative.
	orphan := pooledRateStream(entries)
	for i := range orphan.Entries {
		orphan.Entries[i].SharedResourceRef = 0
		orphan.Entries[i].SharedScopeRef = 0
	}
	require.Zero(t, sharedStructuredMetadataExpansionDelta(orphan))
}

// TestUpdateRatesRequest_ReportsExpandedEquivalentSize asserts that the size the distributor reports
// on the UpdateRates RPC - the only input to the per segmentation key rate that ends up sizing a
// dataobj partition shuffle shard - is expanded-equivalent: a pooled stream reports exactly what the
// same payload pushed without a pool reports.
//
// It used to report the unexpanded size (the pool once per stream), so a flag-on tenant looked like
// it was pushing less than it was and got a narrower partition fan-out while the consumer side work
// stayed expanded.
func TestUpdateRatesRequest_ReportsExpandedEquivalentSize(t *testing.T) {
	const entries = 6

	pooled := pooledRateStream(entries)
	expanded := expandedRateStream(entries)

	pooledReq, err := newUpdateRatesRequest("tenant", []segmentedStream{segmented(pooled, 1)})
	require.NoError(t, err)
	expandedReq, err := newUpdateRatesRequest("tenant", []segmentedStream{segmented(expanded, 1)})
	require.NoError(t, err)

	require.Equal(t,
		expandedReq.Streams[0].TotalSize,
		pooledReq.Streams[0].TotalSize,
		"a pooled stream must be rated like the same payload pushed without a pool",
	)

	// And it really is bigger than the tenant-facing unexpanded size it used to report.
	unexpandedEntries, unexpandedMetadata := calculateStreamSizes(pooled)
	require.Greater(t, pooledReq.Streams[0].TotalSize, unexpandedEntries+unexpandedMetadata)
}

// TestExceedsLimitsRequest_StaysUnexpanded is the other half of the split: admission and the
// per-tenant ingested bytes metric are tenant-facing, so their size must NOT pick up the expansion
// delta.
func TestExceedsLimitsRequest_StaysUnexpanded(t *testing.T) {
	const entries = 6

	pooled := pooledRateStream(entries)
	req, err := newExceedsLimitsRequest("tenant", []KeyedStream{{Stream: pooled, Policy: "default"}})
	require.NoError(t, err)

	entriesSize, metadataSize := calculateStreamSizes(pooled)
	require.Equal(t, entriesSize+metadataSize, req.Streams[0].TotalSize)

	// Which is strictly less than what the same payload pushed expanded would report, i.e. the
	// tenant is not charged for the pool once per entry.
	expandedReq, err := newExceedsLimitsRequest("tenant", []KeyedStream{{Stream: expandedRateStream(entries), Policy: "default"}})
	require.NoError(t, err)
	require.Less(t, req.Streams[0].TotalSize, expandedReq.Streams[0].TotalSize)
}

// TestRateBatcher_ReportsExpandedEquivalentSize covers the batched path, which reports the
// proto-encoded size of the stream. That size shrinks with the flag on, because the pool is encoded
// once for the stream instead of once per entry, so it gets the same expansion delta.
func TestRateBatcher_ReportsExpandedEquivalentSize(t *testing.T) {
	const entries = 6

	pooled := pooledRateStream(entries)
	expanded := expandedRateStream(entries)

	report := func(stream logproto.Stream) uint64 {
		client := &mockUpdateRatesClient{}
		batcher := newRateBatcher(
			RateBatcherConfig{BatchWindow: time.Hour},
			client,
			log.NewNopLogger(),
			prometheus.NewRegistry(),
		)
		batcher.Add("tenant", []segmentedStream{segmented(stream, 1)})
		batcher.flush(context.Background())
		require.Len(t, client.requests, 1)
		require.Len(t, client.requests[0].Streams, 1)
		return client.requests[0].Streams[0].TotalSize
	}

	pooledReported := report(pooled)
	expandedReported := report(expanded)

	// The old number: the proto size of the pooled stream on its own, which is what this path used
	// to report. It is barely half the flag-off number for this payload.
	old := uint64(pooled.Size())
	require.Less(t, old, expandedReported*3/5, "the unfixed size should be well under the flag-off one")

	// The fixed number is the proto size plus the per entry charge for the referenced sets, in
	// payload bytes.
	perEntryCharge := entries * util.SharedSetsSize(pooled.SharedStructuredMetadataSets)
	poolOnce := util.SharedSetsSize(pooled.SharedStructuredMetadataSets)
	require.Equal(t, old+uint64(perEntryCharge-poolOnce), pooledReported)

	// Which lands close to, and never above, the flag-off number. It is not exactly equal: the delta
	// counts payload bytes, so the protobuf framing of the pool sets is left in the base while the
	// framing of the per entry copies is not accounted for. That residue is a few percent (~9% for
	// this payload) and always in the conservative direction. The term that matters - the per entry
	// charge, which is what makes the number scale with the entries referencing the pool - is there.
	require.Greater(t, pooledReported, expandedReported*17/20)
	require.LessOrEqual(t, pooledReported, expandedReported)

	// A stream without a pool is reported exactly as before: its proto size, nothing added.
	require.Equal(t, uint64(expanded.Size()), expandedReported)
}

// TestRateBatcher_RateReachingPartitionResolverIsExpanded follows the value all the way to where
// DataObjTee reads it: the batcher reports a size, the limits-frontend turns it into a rate, and the
// rate the batcher hands back is the rateBytes given to the partition resolver to size the shuffle
// shard. The mock echoes the reported size as the rate, which is what the frontend's rate buckets do
// up to the bucket window, so this asserts the unit that reaches the resolver.
func TestRateBatcher_RateReachingPartitionResolverIsExpanded(t *testing.T) {
	const entries = 6

	rateFor := func(stream logproto.Stream) uint64 {
		client := &mockUpdateRatesClient{echoTotalSizeAsRate: true}
		batcher := newRateBatcher(
			RateBatcherConfig{BatchWindow: time.Hour},
			client,
			log.NewNopLogger(),
			prometheus.NewRegistry(),
		)
		batcher.Add("tenant", []segmentedStream{segmented(stream, 42)})
		batcher.flush(context.Background())
		// This is the lookup DataObjTee.Duplicate does before calling resolver.Resolve.
		return batcher.GetRate("tenant", 42)
	}

	pooled := pooledRateStream(entries)
	expanded := expandedRateStream(entries)

	pooledRate := rateFor(pooled)
	expandedRate := rateFor(expanded)

	require.NotZero(t, pooledRate)
	// The resolver sees essentially the flag-off rate for a pooled stream, so
	// numPartitionsForRateRendezvousHashing gives it the same fan-out. The few percent it is short by
	// is the protobuf framing residue explained in TestRateBatcher_ReportsExpandedEquivalentSize.
	require.Greater(t, pooledRate, expandedRate*17/20)
	require.Greater(t, pooledRate, uint64(pooled.Size()), "the pooled stream's own encoded size is not enough")
}
