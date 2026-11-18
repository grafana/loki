package indexgateway

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
)

func seriesFromRefs(refs []logproto.ChunkRefWithSizingInfo) sharding.ForSeries {
	return sharding.ForSeriesFunc(func(ctx context.Context, _ string, filter index.FingerprintFilter, _, _ model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) bool, _ ...*labels.Matcher) error {
		var borrowed [1]index.ChunkMeta
		for _, ref := range refs {
			if err := ctx.Err(); err != nil {
				return err
			}
			fp := model.Fingerprint(ref.Fingerprint)
			if filter != nil && !filter.Match(fp) {
				continue
			}
			borrowed[0] = index.ChunkMeta{MinTime: int64(ref.From), MaxTime: int64(ref.Through), Checksum: ref.Checksum, KB: ref.KB, Entries: ref.Entries}
			if fn(labels.EmptyLabels(), fp, borrowed[:]) {
				break
			}
			borrowed[0] = index.ChunkMeta{} // poison the borrowed buffer
		}
		return nil
	})
}

func TestPlanShardsOverlappingIndexes(t *testing.T) {
	const tenant = "fake"
	ref := func(fp uint64, from model.Time, checksum, kb uint32) logproto.ChunkRefWithSizingInfo {
		return logproto.ChunkRefWithSizingInfo{
			ChunkRef: logproto.ChunkRef{UserID: tenant, Fingerprint: fp, From: from, Through: 2, Checksum: checksum},
			KB:       kb, Entries: 1,
		}
	}
	refs := []logproto.ChunkRefWithSizingInfo{
		ref(0, -1, 9, 100), ref(0, 0, 1, 100), ref(0, 0, 2, 100),
		ref(2, 0, 10, 100), ref(2, 0, 20, 100), ref(2, 0, 30, 100),
		ref(3, 0, 10, 50), ref(4, 0, 10, 30), ref(4, 0, 20, 30),
		ref(7, 0, 10, 25), ref(math.MaxUint64, 0, math.MaxUint32, 5),
	}
	wantShards := []logproto.Shard{
		{Bounds: logproto.FPBounds{Min: 0, Max: 0}, Stats: &logproto.IndexStatsResponse{Streams: 1, Chunks: 3, Entries: 3, Bytes: 300 << 10}},
		{Bounds: logproto.FPBounds{Min: 1, Max: 2}, Stats: &logproto.IndexStatsResponse{Streams: 1, Chunks: 3, Entries: 3, Bytes: 300 << 10}},
		{Bounds: logproto.FPBounds{Min: 3, Max: 6}, Stats: &logproto.IndexStatsResponse{Streams: 2, Chunks: 3, Entries: 3, Bytes: 110 << 10}},
		{Bounds: logproto.FPBounds{Min: 7, Max: math.MaxUint64}, Stats: &logproto.IndexStatsResponse{Streams: 2, Chunks: 2, Entries: 2, Bytes: 30 << 10}},
	}
	wantGroups := []logproto.ChunkRefGroup{
		{Refs: []*logproto.ChunkRef{&refs[0].ChunkRef, &refs[1].ChunkRef, &refs[2].ChunkRef}},
		{Refs: []*logproto.ChunkRef{&refs[3].ChunkRef, &refs[4].ChunkRef, &refs[5].ChunkRef}},
		{Refs: []*logproto.ChunkRef{&refs[6].ChunkRef, &refs[7].ChunkRef, &refs[8].ChunkRef}},
		{Refs: []*logproto.ChunkRef{&refs[9].ChunkRef, &refs[10].ChunkRef}},
	}

	// Both files contain the middle chunks. One returns its refs in reverse
	// order. Run both callbacks concurrently, as MultiIndex can.
	first := slices.Clone(refs[:8])
	slices.Reverse(first)
	indexes := []sharding.ForSeries{seriesFromRefs(first), seriesFromRefs(refs[3:])}
	concurrent := sharding.ForSeriesFunc(func(ctx context.Context, user string, filter index.FingerprintFilter, from, through model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) bool, ms ...*labels.Matcher) error {
		errs := make(chan error, len(indexes))
		for _, idx := range indexes {
			go func() { errs <- idx.ForSeries(ctx, user, filter, from, through, fn, ms...) }()
		}
		return errors.Join(<-errs, <-errs)
	})
	req := &logproto.ShardsRequest{TargetBytesPerShard: 100 << 10}
	for _, precompute := range []bool{false, true} {
		for _, batchSize := range []int{1, 3, shardPlanningBatchSize} {
			t.Run(fmt.Sprintf("precompute=%t/batch=%d", precompute, batchSize), func(t *testing.T) {
				got, err := planShards(context.Background(), concurrent, tenant, req, nil, precompute, batchSize)
				require.NoError(t, err)
				require.Equal(t, wantShards, got.Shards)
				require.EqualValues(t, len(refs), got.Statistics.Index.TotalChunks)
				require.EqualValues(t, len(refs), got.Statistics.Index.PostFilterChunks)
				require.EqualValues(t, 6, got.Statistics.Index.TotalStreams)
				if precompute {
					require.Equal(t, wantGroups, got.ChunkGroups)
				} else {
					require.Nil(t, got.ChunkGroups)
				}
			})
		}
	}
}

func assertShardBounds(t *testing.T, resp *logproto.ShardsResponse) {
	t.Helper()
	require.Zero(t, resp.Shards[0].Bounds.Min)
	require.EqualValues(t, uint64(math.MaxUint64), resp.Shards[len(resp.Shards)-1].Bounds.Max)
	for i, shard := range resp.Shards {
		require.LessOrEqual(t, shard.Bounds.Min, shard.Bounds.Max)
		if i > 0 {
			require.Equal(t, resp.Shards[i-1].Bounds.Max+1, shard.Bounds.Min)
		}
	}
}

func TestPlanShardsSkew(t *testing.T) {
	for _, field := range []string{"from", "through", "checksum"} {
		t.Run(field, func(t *testing.T) {
			var refs []logproto.ChunkRefWithSizingInfo
			for i := range 1025 {
				ref := logproto.ChunkRefWithSizingInfo{ChunkRef: logproto.ChunkRef{UserID: "fake", Fingerprint: math.MaxUint64, From: -2000, Through: 2000}, KB: 1, Entries: 3}
				switch field {
				case "from":
					ref.From += model.Time(i)
				case "through":
					ref.Through += model.Time(i)
				case "checksum":
					ref.Checksum = uint32(i)
				}
				refs = append(refs, ref)
			}
			// A single callback may contain far more chunks than the batch budget.
			// Match the index's borrowed decode buffer lifetime, including duplicates.
			var metadata []index.ChunkMeta
			for range 2 {
				for _, ref := range refs {
					metadata = append(metadata, index.ChunkMeta{MinTime: int64(ref.From), MaxTime: int64(ref.Through), Checksum: ref.Checksum, KB: ref.KB, Entries: ref.Entries})
				}
			}
			slices.Reverse(metadata)
			series := sharding.ForSeriesFunc(func(_ context.Context, _ string, filter index.FingerprintFilter, _, _ model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) bool, _ ...*labels.Matcher) error {
				if filter != nil && !filter.Match(math.MaxUint64) {
					return nil
				}
				borrowed := slices.Clone(metadata)
				fn(labels.EmptyLabels(), math.MaxUint64, borrowed)
				clear(borrowed)
				return nil
			})
			const budget = 7
			var got []logproto.ChunkRefWithSizingInfo
			req := &logproto.ShardsRequest{TargetBytesPerShard: 1}
			err := forEachChunkBatch(context.Background(), series, "fake", req, nil, budget, func(batch []logproto.ChunkRefWithSizingInfo) error {
				require.LessOrEqual(t, len(batch), budget)
				got = append(got, batch...)
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, refs, got)
			for _, precompute := range []bool{false, true} {
				resp, err := planShards(context.Background(), series, "fake", req, nil, precompute, budget)
				require.NoError(t, err)
				require.Equal(t, []logproto.Shard{{
					Bounds: logproto.FPBounds{Max: math.MaxUint64},
					Stats:  &logproto.IndexStatsResponse{Streams: 1, Chunks: 1025, Entries: 3 * 1025, Bytes: 1025 << 10},
				}}, resp.Shards)
				require.EqualValues(t, 1, resp.Statistics.Index.TotalStreams)
				require.EqualValues(t, len(refs), resp.Statistics.Index.TotalChunks)
				require.EqualValues(t, len(refs), resp.Statistics.Index.PostFilterChunks)
				if precompute {
					require.Len(t, resp.ChunkGroups, 1)
					require.Len(t, resp.ChunkGroups[0].Refs, len(refs))
					for i := range refs {
						require.Equal(t, &refs[i].ChunkRef, resp.ChunkGroups[0].Refs[i])
					}
				} else {
					require.Nil(t, resp.ChunkGroups)
				}
			}
		})
	}
}

func TestPlanShardsEmptyAndLargeChunk(t *testing.T) {
	for _, refs := range [][]logproto.ChunkRefWithSizingInfo{nil, {{ChunkRef: logproto.ChunkRef{UserID: "fake", Fingerprint: 0}, KB: math.MaxUint32}}} {
		for _, precompute := range []bool{false, true} {
			for _, target := range []uint64{0, 1, math.MaxUint64} {
				req := &logproto.ShardsRequest{TargetBytesPerShard: target}
				resp, err := planShards(context.Background(), seriesFromRefs(refs), "fake", req, nil, precompute, 1)
				require.NoError(t, err)
				assertShardBounds(t, resp)
				require.EqualValues(t, len(refs), resp.Statistics.Index.TotalChunks)
				if len(refs) > 0 {
					require.Equal(t, uint64(math.MaxUint32)<<10, resp.Shards[0].Stats.Bytes)
				} else {
					require.Equal(t, []logproto.Shard{{
						Bounds: logproto.FPBounds{Max: math.MaxUint64},
						Stats:  &logproto.IndexStatsResponse{},
					}}, resp.Shards)
					require.Zero(t, resp.Statistics.Index.TotalStreams)
					require.Zero(t, resp.Statistics.Index.PostFilterChunks)
					require.Nil(t, resp.ChunkGroups)
				}
			}
		}
	}
}

func TestChunkBatchErrorsAndCancellation(t *testing.T) {
	sentinel := errors.New("index read failed")
	req := &logproto.ShardsRequest{}
	for _, cancel := range []bool{false, true} {
		ctx, stop := context.WithCancel(context.Background())
		series := sharding.ForSeriesFunc(func(_ context.Context, _ string, _ index.FingerprintFilter, _, _ model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) bool, _ ...*labels.Matcher) error {
			// Overflow must not swallow errors or cause a partial response.
			fn(labels.EmptyLabels(), 0, []index.ChunkMeta{{Checksum: 1}, {Checksum: 2}})
			if cancel {
				stop()
				return nil
			}
			return sentinel
		})
		resp, err := planShards(ctx, series, "fake", req, nil, true, 1)
		if cancel {
			require.ErrorIs(t, err, context.Canceled)
		} else {
			require.ErrorIs(t, err, sentinel)
		}
		require.Nil(t, resp)
		stop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := planShards(ctx, nil, "fake", req, nil, false, 1)
	require.ErrorIs(t, err, context.Canceled)
	err = forEachChunkBatch(context.Background(), seriesFromRefs(nil), "fake", req, nil, 1, func([]logproto.ChunkRefWithSizingInfo) error { return sentinel })
	require.ErrorIs(t, err, sentinel)
	err = forEachChunkBatch(context.Background(), nil, "fake", req, nil, 0, nil)
	require.Error(t, err)
}

func TestChunkBatchCancellationBetweenBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	batches := 0
	err := forEachChunkBatch(ctx, generatedSeries(8, 3), "fake", &logproto.ShardsRequest{}, nil, 2, func([]logproto.ChunkRefWithSizingInfo) error {
		batches++
		cancel()
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, batches)
}

// generatedSeries models a broad query with overlapping indexes without a
// materialized fixture. Each callback borrows and reuses the same decode buffer.
func generatedSeries(streams, chunks int) sharding.ForSeries {
	return sharding.ForSeriesFunc(func(ctx context.Context, _ string, filter index.FingerprintFilter, _, _ model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) bool, _ ...*labels.Matcher) error {
		buf := make([]index.ChunkMeta, chunks)
		for range 2 {
			for i := range streams {
				fp := model.Fingerprint(uint64(i) * (math.MaxUint64 / uint64(streams)))
				if !filter.Match(fp) {
					continue
				}
				for j := range buf {
					buf[j] = index.ChunkMeta{MinTime: int64(j), MaxTime: int64(j + 1), Checksum: uint32(j), KB: 1024, Entries: 100}
				}
				if fn(labels.EmptyLabels(), fp, buf) {
					break
				}
				if err := ctx.Err(); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func TestChunkBatchBroadQueryBound(t *testing.T) {
	const streams, chunks, budget = 10000, 20, 1536
	var count, peak int
	err := forEachChunkBatch(context.Background(), generatedSeries(streams, chunks), "fake", &logproto.ShardsRequest{}, nil, budget, func(batch []logproto.ChunkRefWithSizingInfo) error {
		count += len(batch)
		peak = max(peak, cap(batch))
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, streams*chunks, count)
	require.LessOrEqual(t, peak, budget)
	t.Logf("unique refs=%d; peak batch capacity=%d; budget=%d", count, peak, budget)
}

func TestChunkBatchSmallQuery(t *testing.T) {
	err := forEachChunkBatch(t.Context(), generatedSeries(1, 10), "fake", &logproto.ShardsRequest{}, nil, shardPlanningBatchSize, func(batch []logproto.ChunkRefWithSizingInfo) error {
		require.Len(t, batch, 10)
		require.LessOrEqual(t, cap(batch), 1024)
		return nil
	})
	require.NoError(t, err)
}

func BenchmarkSmallShardPlanning(b *testing.B) {
	req := &logproto.ShardsRequest{TargetBytesPerShard: 600 << 20}
	MeasureShardPlanning(b, generatedSeries(100, 10), req, nil, 1000)
}

// MeasureShardPlanning runs the cap sweep for the external TSDB benchmark.
func MeasureShardPlanning(b *testing.B, series sharding.ForSeries, req *logproto.ShardsRequest, matchers []*labels.Matcher, uniqueRefs int64) {
	b.Helper()
	for _, batchSize := range []int{16 << 10, 64 << 10, 128 << 10, 256 << 10, 512 << 10} {
		b.Run(fmt.Sprintf("batch=%d", batchSize), func(b *testing.B) {
			// Warm the indexes and measure the reference buffer capacity
			// separately from the timed planner runs.
			var peakSlots int
			err := forEachChunkBatch(b.Context(), series, "synthetic", req, matchers, batchSize, func(batch []logproto.ChunkRefWithSizingInfo) error {
				peakSlots = max(peakSlots, cap(batch))
				return nil
			})
			require.NoError(b, err)
			require.LessOrEqual(b, peakSlots, batchSize)
			var scans int
			countingSeries := sharding.ForSeriesFunc(func(ctx context.Context, user string, filter index.FingerprintFilter, from, through model.Time, fn func(labels.Labels, model.Fingerprint, []index.ChunkMeta) bool, ms ...*labels.Matcher) error {
				scans++
				return series.ForSeries(ctx, user, filter, from, through, fn, ms...)
			})
			ctx := b.Context()
			b.ReportAllocs()
			for b.Loop() {
				resp, err := planShards(ctx, countingSeries, "synthetic", req, matchers, false, batchSize)
				if err != nil {
					b.Fatal(err)
				}
				if got := resp.Statistics.Index.TotalChunks; got != uniqueRefs {
					b.Fatalf("resolved %d refs, want %d", got, uniqueRefs)
				}
			}
			b.ReportMetric(float64(scans)/float64(b.N), "scans/op")
			b.ReportMetric(float64(peakSlots), "peak-ref-slots")
		})
	}
}
