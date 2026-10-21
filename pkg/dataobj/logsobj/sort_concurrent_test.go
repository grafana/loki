package logsobj

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestPlanSortRanges(t *testing.T) {
	distinctKeys := func(n int) []streams.SortKey {
		keys := make([]streams.SortKey, n+1)
		for rank := range keys {
			keys[rank] = streams.SortKey{Hash: uint64(rank)}
		}
		return keys
	}

	for _, tc := range []struct {
		name  string
		sizes []int64
		keys  []streams.SortKey
		want  []rankRange
	}{
		{
			name:  "returns no range when there are no streams",
			sizes: []int64{0},
			keys:  distinctKeys(0),
			want:  nil,
		},
		{
			name:  "returns one range when all streams fit in the target size",
			sizes: []int64{0, 10, 10, 10},
			keys:  distinctKeys(3),
			want:  []rankRange{{first: 1, last: 3}},
		},
		{
			name:  "ends a range before the stream that would take it over the target size",
			sizes: []int64{0, 60, 30, 50, 80, 10},
			keys:  distinctKeys(5),
			want:  []rankRange{{first: 1, last: 2}, {first: 3, last: 3}, {first: 4, last: 5}},
		},
		{
			name:  "keeps streams with equal sort keys in one range even when it exceeds the target size",
			sizes: []int64{0, 60, 50, 30, 40},
			keys: []streams.SortKey{
				{}, {Hash: 1}, {Hash: 1}, {Hash: 2}, {Hash: 3},
			},
			want: []rankRange{{first: 1, last: 2}, {first: 3, last: 4}},
		},
		{
			name:  "puts a stream larger than the target size in a range of its own",
			sizes: []int64{0, 10, 500, 10},
			keys:  distinctKeys(3),
			want:  []rankRange{{first: 1, last: 1}, {first: 2, last: 2}, {first: 3, last: 3}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, planSortRanges(tc.sizes, tc.keys, 100))
		})
	}
}

func TestStreamIDsByRange(t *testing.T) {
	t.Run("assigns each stream ID to the range of its rank and skips unranked IDs", func(t *testing.T) {
		remap := []rankedSortKey{
			{},        // ID 0 is unused.
			{rank: 3}, // ID 1
			{rank: 1}, // ID 2
			{},        // ID 3 has no rank.
			{rank: 2}, // ID 4
			{rank: 4}, // ID 5
		}
		ranges := []rankRange{{first: 1, last: 2}, {first: 3, last: 4}}
		require.Equal(t, [][]int64{{2, 4}, {1, 5}}, streamIDsByRange(remap, ranges))
	})
}

func TestBuilder_CopyAndSortWithConcurrency(t *testing.T) {
	cfg := BuilderBaseConfig{
		TargetPageSize:          4 << 10,
		MaxPageRows:             100,
		TargetObjectSize:        1 << 30,
		TargetSectionSize:       32 << 10,
		BufferSize:              16 << 10,
		SectionStripeMergeLimit: 2,
	}

	source := func(t *testing.T) *dataobj.Object {
		t.Helper()
		builder, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
		require.NoError(t, err)

		now := time.Date(2026, time.October, 19, 0, 0, 0, 0, time.UTC)
		for _, tenant := range []string{"tenant-a", "tenant-b"} {
			for i := range 3000 {
				ts := now.Add(time.Duration(i%97) * time.Second)
				require.NoError(t, builder.Append(tenant, logproto.Stream{
					Labels: fmt.Sprintf(`{app="app-%d", pod="pod-%d"}`, i%7, i%211),
					Entries: []push.Entry{{
						Timestamp:          ts,
						Line:               fmt.Sprintf("%s line %05d level=info msg=%q", tenant, i, "request done"),
						StructuredMetadata: push.LabelsAdapter{{Name: "trace_id", Value: fmt.Sprintf("%08x", i*2654435761)}},
					}},
				}, ts))
			}
		}
		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		return obj
	}

	sorted := func(t *testing.T, src *dataobj.Object, overrides TenantOverrides, concurrency int) *dataobj.Object {
		t.Helper()
		builder, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
		require.NoError(t, err)
		obj, closer, err := builder.CopyAndSortWithConcurrency(t.Context(), src, concurrency)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		return obj
	}

	for _, tc := range []struct {
		name      string
		overrides TenantOverrides
		resort    bool
	}{
		{
			name:   "merge-only copy with 4 goroutines gives the records in the same order as 1 goroutine",
			resort: false,
		},
		{
			name:      "re-sorting copy with 4 goroutines gives the records in the same order as 1 goroutine",
			overrides: tenantOverrides{"tenant-a": {"label:pod"}, "tenant-b": {"label:pod"}},
			resort:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := source(t)
			builder, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), tc.overrides)
			require.NoError(t, err)
			resort, err := builder.requiresResort(t.Context(), src)
			require.NoError(t, err)
			require.Equal(t, tc.resort, resort, "the case must take the copy path it names")

			serial := sorted(t, src, tc.overrides, 1)
			concurrent := sorted(t, src, tc.overrides, 4)

			want := objectRecords(t, serial)
			require.Len(t, want, 6000)
			require.Equal(t, want, objectRecords(t, concurrent))

			// Each tenant must span several ranges, or the concurrent path
			// would not run.
			require.Greater(t, concurrent.Sections().Count(logs.CheckSection), 2*2)
		})
	}
}

func TestBuilder_SortRangesConcurrently(t *testing.T) {
	t.Run("returns the error of the failed ranges when there are more ranges than goroutines", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
		require.NoError(t, err)
		now := time.Date(2026, time.October, 19, 0, 0, 0, 0, time.UTC)
		for i := range 200 {
			require.NoError(t, builder.Append("tenant", logproto.Stream{
				Labels:  fmt.Sprintf(`{pod="pod-%d"}`, i%20),
				Entries: []push.Entry{{Timestamp: now.Add(time.Duration(i) * time.Second), Line: "line"}},
			}, now))
		}
		src, closer, err := builder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		var sections []*dataobj.Section
		for _, sec := range src.Sections().Filter(logs.CheckSection) {
			sections = append(sections, sec)
		}
		// No stream has a rank, so the merge of every range fails at its
		// first record.
		remap := make([]rankedSortKey, 21)
		rangeIDs := [][]int64{{1, 2, 3, 4}, {5, 6, 7, 8}, {9, 10, 11, 12}, {13, 14, 15, 16}, {17, 18, 19, 20}}

		sorter, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
		require.NoError(t, err)
		newSection := func() *logs.Builder {
			lb := logs.NewBuilder(sorter.metrics.logs, logs.BuilderOptions{
				PageSizeHint:   int(testBuilderConfig.TargetPageSize),
				AppendStrategy: logs.AppendOrdered,
				SortOrder:      logs.SortSchemaASC,
			})
			lb.SetTenant("tenant")
			return lb
		}
		err = sorter.sortRangesConcurrently(t.Context(), "tenant", sections, remap, rangeIDs, newSection, 2)
		require.ErrorContains(t, err, "missing stream ID remap")
		require.Zero(t, sorter.builder.Bytes())
	})
}

// BenchmarkBuilder_CopyAndSortWithConcurrency sorts an object that holds one
// tenant with compressible logfmt lines, many streams and structured
// metadata, so that the sort of that tenant sets the time of the whole sort.
func BenchmarkBuilder_CopyAndSortWithConcurrency(b *testing.B) {
	const (
		numRows    = 600_000
		numStreams = 2_000
	)

	cfg := BuilderBaseConfig{
		TargetPageSize:          1 << 20,
		MaxPageRows:             10_000,
		TargetObjectSize:        1 << 30,
		TargetSectionSize:       16 << 20,
		BufferSize:              16 << 20,
		SectionStripeMergeLimit: 2,
	}

	rng := rand.New(rand.NewSource(42))
	levels := []string{"info", "debug", "warn", "error"}
	paths := []string{"/api/v1/push", "/loki/api/v1/query_range", "/metrics", "/ready"}
	randHex := func(n int) string {
		const hex = "0123456789abcdef"
		out := make([]byte, n)
		for i := range out {
			out[i] = hex[rng.Intn(len(hex))]
		}
		return string(out)
	}
	streamLabels := make([]string, numStreams)
	for i := range streamLabels {
		streamLabels[i] = fmt.Sprintf(`{namespace="ns-%d", app="app-%d", pod="pod-%d-%s"}`, i%17, i%113, i, randHex(5))
	}

	builder, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(b, err)
	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	var uncompressed int64
	for i := range numRows {
		// Records arrive in Kafka order: streams interleave and time moves
		// forward with jitter.
		ts := now.Add(time.Duration(i)*time.Millisecond + time.Duration(rng.Intn(5000))*time.Millisecond)
		line := fmt.Sprintf(`ts=%s level=%s caller=handler.go:%d msg="request done" path=%s status=%d duration=%dms user=%s trace_id=%s`,
			ts.Format(time.RFC3339Nano), levels[rng.Intn(len(levels))], 100+rng.Intn(400), paths[rng.Intn(len(paths))],
			[]int{200, 204, 400, 429, 500}[rng.Intn(5)], rng.Intn(5000), randHex(8), randHex(32))
		uncompressed += int64(len(line))
		require.NoError(b, builder.Append("tenant", logproto.Stream{
			Labels: streamLabels[rng.Intn(numStreams)],
			Entries: []push.Entry{{
				Timestamp:          ts,
				Line:               line,
				StructuredMetadata: push.LabelsAdapter{{Name: "trace_id", Value: randHex(32)}},
			}},
		}, ts))
	}
	unsorted, closer, err := builder.Flush()
	require.NoError(b, err)
	b.Cleanup(func() { _ = closer.Close() })

	for _, concurrency := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("concurrency=%d", concurrency), func(b *testing.B) {
			sorter, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
			require.NoError(b, err)
			b.SetBytes(uncompressed)
			b.ReportAllocs()
			for b.Loop() {
				sorted, closer, err := sorter.CopyAndSortWithConcurrency(b.Context(), unsorted, concurrency)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(sorted.Sections().Count(logs.CheckSection)), "sections")
				_ = closer.Close()
			}
		})
	}
}

// objectRecords returns every log record of obj in the order of the object,
// with the labels of its stream instead of the stream ID.
func objectRecords(t *testing.T, obj *dataobj.Object) []string {
	t.Helper()

	labelsByTenant := map[string]map[int64]string{}
	for _, sec := range obj.Sections().Filter(streams.CheckSection) {
		streamsSection, err := streams.Open(t.Context(), sec)
		require.NoError(t, err)
		labels := map[int64]string{}
		for res := range streams.IterSection(t.Context(), streamsSection) {
			stream, err := res.Value()
			require.NoError(t, err)
			labels[stream.ID] = stream.Labels.String()
		}
		labelsByTenant[sec.Tenant] = labels
	}

	var records []string
	for _, sec := range obj.Sections().Filter(logs.CheckSection) {
		logsSection, err := logs.Open(t.Context(), sec)
		require.NoError(t, err)
		for res := range logs.IterSection(t.Context(), logsSection) {
			rec, err := res.Value()
			require.NoError(t, err)
			labels, ok := labelsByTenant[sec.Tenant][rec.StreamID]
			require.True(t, ok, "record of tenant %s refers to unknown stream %d", sec.Tenant, rec.StreamID)
			records = append(records, fmt.Sprintf("%s|%s|%d|%s|%s", sec.Tenant, labels, rec.Timestamp.UnixNano(), rec.Metadata, rec.Line))
		}
	}
	return records
}
