package logsobj

import (
	"context"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
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
	byRank := func(sizes []int64, keys []streams.SortKey) []rankedStream {
		out := make([]rankedStream, len(sizes))
		for rank := range out {
			out[rank] = rankedStream{rank: int64(rank), size: sizes[rank], SortKey: keys[rank]}
		}
		return out
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
			name:  "keeps three streams with equal sort keys in one range even when it exceeds the target size",
			sizes: []int64{0, 60, 60, 60, 10},
			keys: []streams.SortKey{
				{}, {Hash: 1}, {Hash: 1}, {Hash: 1}, {Hash: 2},
			},
			want: []rankRange{{first: 1, last: 3}, {first: 4, last: 4}},
		},
		{
			name:  "keeps streams in one range when their total equals the target size",
			sizes: []int64{0, 60, 40, 10},
			keys:  distinctKeys(3),
			want:  []rankRange{{first: 1, last: 2}, {first: 3, last: 3}},
		},
		{
			name:  "returns one range for a single stream",
			sizes: []int64{0, 500},
			keys:  distinctKeys(1),
			want:  []rankRange{{first: 1, last: 1}},
		},
		{
			name:  "puts a stream larger than the target size in a range of its own",
			sizes: []int64{0, 10, 500, 10},
			keys:  distinctKeys(3),
			want:  []rankRange{{first: 1, last: 1}, {first: 2, last: 2}, {first: 3, last: 3}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, planSortRanges(byRank(tc.sizes, tc.keys), 100))
		})
	}
}

func TestStreamIDsByRange(t *testing.T) {
	t.Run("assigns each stream ID to the range of its rank and skips unranked IDs", func(t *testing.T) {
		remap := []rankedStream{
			{},        // ID 0 is unused.
			{rank: 3}, // ID 1
			{rank: 1}, // ID 2
			{},        // ID 3 has no rank.
			{rank: 2}, // ID 4
			{rank: 4}, // ID 5
		}
		ranges := []rankRange{{first: 1, last: 2}, {first: 3, last: 4}}
		got, err := streamIDsByRange(remap, ranges)
		require.NoError(t, err)
		require.Equal(t, [][]int64{{2, 4}, {1, 5}}, got)
	})

	t.Run("returns an error when a rank is in no range", func(t *testing.T) {
		remap := []rankedStream{{}, {rank: 1}, {rank: 5}}
		_, err := streamIDsByRange(remap, []rankRange{{first: 1, last: 2}})
		require.ErrorContains(t, err, "stream with old ID 2 and rank 5 is in no range")
	})
}

func TestBuilder_FlushAndSortParallelism(t *testing.T) {
	const (
		rowsPerTenant = 3000
		smallSection  = 32 << 10
		hugeSection   = 1 << 29
	)
	cfg := BuilderBaseConfig{
		TargetPageSize:          4 << 10,
		MaxPageRows:             100,
		TargetObjectSize:        1 << 30,
		TargetSectionSize:       smallSection,
		BufferSize:              16 << 10,
		SectionStripeMergeLimit: 2,
	}

	type shape struct {
		labels func(n int) string
		line   func(tenant string, n int) string
	}
	shortLine := func(tenant string, n int) string {
		return fmt.Sprintf("%s line %05d level=info msg=%q", tenant, n, "request done")
	}
	longLine := func(tenant string, n int) string {
		return fmt.Sprintf("%s line %05d %s", tenant, n, strings.Repeat("x", 100))
	}
	manySmallStreams := shape{
		labels: func(n int) string { return fmt.Sprintf(`{app="app-%d", pod="pod-%d"}`, n%7, n%211) },
		line:   shortLine,
	}
	fewStreamsLargerThanASection := shape{
		labels: func(n int) string { return fmt.Sprintf(`{app="app-%d"}`, n%3) },
		line:   longLine,
	}
	oneStream := shape{
		labels: func(int) string { return `{app="only"}` },
		line:   longLine,
	}

	// flushAndSort fills a builder with the same input for every call and sorts
	// it. Every ninth record repeats the previous one, so that a builder that
	// drops duplicates has work to do.
	flushAndSort := func(t *testing.T, sh shape, overrides TenantOverrides, dropDuplicates bool, parallelism int, sectionSize int) *dataobj.Object {
		t.Helper()
		cfg := cfg
		cfg.SortParallelism = parallelism
		cfg.DropDuplicates = dropDuplicates
		cfg.TargetSectionSize = flagext.Bytes(sectionSize)
		builder, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
		require.NoError(t, err)

		now := time.Date(2026, time.October, 19, 0, 0, 0, 0, time.UTC)
		for _, tenant := range []string{"tenant-a", "tenant-b"} {
			for i := range rowsPerTenant {
				n := i - i%9*(i%2)
				ts := now.Add(time.Duration(n%97) * time.Second)
				require.NoError(t, builder.Append(tenant, logproto.Stream{
					Labels: sh.labels(n),
					Entries: []push.Entry{{
						Timestamp:          ts,
						Line:               sh.line(tenant, n),
						StructuredMetadata: push.LabelsAdapter{{Name: "trace_id", Value: fmt.Sprintf("%08x", n*2654435761)}},
					}},
				}, ts))
			}
		}
		obj, closer, err := builder.FlushAndSort(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		return obj
	}

	for _, tc := range []struct {
		name           string
		shape          shape
		overrides      TenantOverrides
		dropDuplicates bool
		multiSection   bool
	}{
		{
			name:         "gives the records and stream stats of a single range for many small streams with the default sort schema",
			shape:        manySmallStreams,
			overrides:    NoTenantOverrides{},
			multiSection: true,
		},
		{
			name:         "gives the records and stream stats of a single range for many small streams with a custom sort schema",
			shape:        manySmallStreams,
			overrides:    tenantOverrides{"tenant-a": {"label:pod"}, "tenant-b": {"label:pod"}},
			multiSection: true,
		},
		{
			name:           "drops the duplicates of a single range for many small streams",
			shape:          manySmallStreams,
			overrides:      NoTenantOverrides{},
			dropDuplicates: true,
			multiSection:   true,
		},
		{
			name:         "gives the records and stream stats of a single range for few streams that are larger than a section",
			shape:        fewStreamsLargerThanASection,
			overrides:    NoTenantOverrides{},
			multiSection: true,
		},
		{
			name:           "drops the duplicates of a single range for few streams that are larger than a section",
			shape:          fewStreamsLargerThanASection,
			overrides:      NoTenantOverrides{},
			dropDuplicates: true,
			multiSection:   true,
		},
		{
			name:         "gives the records and stream stats of a single range for a tenant with one stream",
			shape:        oneStream,
			overrides:    NoTenantOverrides{},
			multiSection: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A section size above the size of all records puts every stream of
			// a tenant in one range, which is a plain merge of the sections.
			oracle := flushAndSort(t, tc.shape, tc.overrides, tc.dropDuplicates, 1, hugeSection)
			require.Equal(t, 2, oracle.Sections().Count(logs.CheckSection), "the oracle must have one section for each tenant")

			wantRecords := canonicalTies(objectRecords(t, oracle))
			if tc.dropDuplicates {
				require.Less(t, len(wantRecords), 2*rowsPerTenant, "the input must hold duplicates")
			} else {
				require.Len(t, wantRecords, 2*rowsPerTenant)
			}
			wantStreams := objectStreams(t, oracle)

			single := flushAndSort(t, tc.shape, tc.overrides, tc.dropDuplicates, 1, smallSection)
			parallel := flushAndSort(t, tc.shape, tc.overrides, tc.dropDuplicates, 4, smallSection)

			// Both sort the same input sections, so even the order of records
			// with the same stream and timestamp must agree.
			require.Equal(t, objectRecords(t, single), objectRecords(t, parallel))
			require.Equal(t, objectStreams(t, single), objectStreams(t, parallel))

			for name, got := range map[string]*dataobj.Object{"1 goroutine": single, "4 goroutines": parallel} {
				require.Equal(t, wantRecords, canonicalTies(objectRecords(t, got)), "records with %s", name)
				require.Equal(t, wantStreams, objectStreams(t, got), "stream stats with %s", name)
				if tc.multiSection {
					require.Greater(t, got.Sections().Count(logs.CheckSection), 2*2, "the sort with %s must cut each tenant into several sections", name)
				}
			}
		})
	}
}

func TestBuilder_SortRangesInParallel(t *testing.T) {
	const (
		numStreams    = 20
		rowsPerStream = 10
	)

	// The sections hold numStreams streams with the old IDs 1 to numStreams.
	// Every stream has the rank of its old ID, and every sort key is equal.
	newSorter := func(t *testing.T, parallelism int) (sorter *Builder, sections []*dataobj.Section, remap []rankedStream, rangeIDs [][]int64, newSection func() *logs.Builder) {
		t.Helper()
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
		require.NoError(t, err)
		now := time.Date(2026, time.October, 19, 0, 0, 0, 0, time.UTC)
		for i := range numStreams * rowsPerStream {
			require.NoError(t, builder.Append("tenant", logproto.Stream{
				Labels:  fmt.Sprintf(`{pod="pod-%d"}`, i%numStreams),
				Entries: []push.Entry{{Timestamp: now.Add(time.Duration(i) * time.Second), Line: "line"}},
			}, now))
		}
		src, closer, err := builder.flushWithoutLayout()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		for _, sec := range src.Sections().Filter(logs.CheckSection) {
			sections = append(sections, sec)
		}

		remap = make([]rankedStream, numStreams+1)
		for id := 1; id <= numStreams; id++ {
			remap[id] = rankedStream{rank: int64(id)}
		}
		rangeIDs = [][]int64{{1, 2, 3, 4}, {5, 6, 7, 8}, {9, 10, 11, 12}, {13, 14, 15, 16}, {17, 18, 19, 20}}

		cfg := testBuilderConfig
		cfg.SortParallelism = parallelism
		sorter, err = NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
		require.NoError(t, err)
		newSection = func() *logs.Builder {
			lb := logs.NewBuilder(sorter.metrics.logs, logs.BuilderOptions{
				PageSizeHint:   int(testBuilderConfig.TargetPageSize),
				AppendStrategy: logs.AppendOrdered,
				SortOrder:      logs.SortSchemaASC,
			})
			lb.SetTenant("tenant")
			return lb
		}
		return sorter, sections, remap, rangeIDs, newSection
	}

	t.Run("adds the records of every range to the totals", func(t *testing.T) {
		sorter, sections, remap, rangeIDs, newSection := newSorter(t, 2)

		totals, err := sorter.sortRangesInParallel(t.Context(), "tenant", sections, remap, rangeIDs, numStreams, newSection)
		require.NoError(t, err)
		require.Len(t, totals, numStreams+1)
		for id := 1; id <= numStreams; id++ {
			require.Equal(t, rowsPerStream, totals[id].rows, "rows of stream %d", id)
		}
	})

	t.Run("returns the error of a range, with the range and the tenant, when more ranges than goroutines fail", func(t *testing.T) {
		sorter, sections, remap, rangeIDs, newSection := newSorter(t, 2)
		for id := 5; id <= numStreams; id++ {
			remap[id] = rankedStream{}
		}

		_, err := sorter.sortRangesInParallel(t.Context(), "tenant", sections, remap, rangeIDs, numStreams, newSection)
		require.ErrorContains(t, err, "missing stream ID remap")
		require.ErrorContains(t, err, "sorting stream range")
		require.ErrorContains(t, err, "of tenant tenant")
	})

	t.Run("returns the context error when the context is canceled while the ranges run", func(t *testing.T) {
		sorter, sections, remap, rangeIDs, newSection := newSorter(t, 2)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		cancelOnFirstSection := func() *logs.Builder {
			cancel()
			return newSection()
		}

		_, err := sorter.sortRangesInParallel(ctx, "tenant", sections, remap, rangeIDs, numStreams, cancelOnFirstSection)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("returns no error and zero totals when there are no ranges", func(t *testing.T) {
		sorter, sections, remap, _, newSection := newSorter(t, 2)

		totals, err := sorter.sortRangesInParallel(t.Context(), "tenant", sections, remap, nil, numStreams, newSection)
		require.NoError(t, err)
		require.Equal(t, newStreamTotals(numStreams), totals)
	})
}

// BenchmarkBuilder_FlushAndSortParallelism sorts an object that holds one
// tenant with compressible logfmt lines, many streams and structured
// metadata, so that the sort of that tenant sets the time of the whole sort.
func BenchmarkBuilder_FlushAndSortParallelism(b *testing.B) {
	const (
		numRows    = 600_000
		numStreams = 2_000
	)

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

	type row struct {
		labels string
		entry  push.Entry
	}
	now := time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)
	rows := make([]row, numRows)
	var uncompressed int64
	for i := range rows {
		// Records arrive in Kafka order: streams interleave and time moves
		// forward with jitter.
		ts := now.Add(time.Duration(i)*time.Millisecond + time.Duration(rng.Intn(5000))*time.Millisecond)
		line := fmt.Sprintf(`ts=%s level=%s caller=handler.go:%d msg="request done" path=%s status=%d duration=%dms user=%s trace_id=%s`,
			ts.Format(time.RFC3339Nano), levels[rng.Intn(len(levels))], 100+rng.Intn(400), paths[rng.Intn(len(paths))],
			[]int{200, 204, 400, 429, 500}[rng.Intn(5)], rng.Intn(5000), randHex(8), randHex(32))
		uncompressed += int64(len(line))
		rows[i] = row{
			labels: streamLabels[rng.Intn(numStreams)],
			entry: push.Entry{
				Timestamp:          ts,
				Line:               line,
				StructuredMetadata: push.LabelsAdapter{{Name: "trace_id", Value: randHex(32)}},
			},
		}
	}

	for _, parallelism := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("parallelism=%d", parallelism), func(b *testing.B) {
			cfg := BuilderBaseConfig{
				TargetPageSize:          1 << 20,
				MaxPageRows:             10_000,
				TargetObjectSize:        1 << 30,
				TargetSectionSize:       16 << 20,
				BufferSize:              16 << 20,
				SectionStripeMergeLimit: 2,
				SortParallelism:         parallelism,
				DropDuplicates:          true,
			}
			builder, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
			require.NoError(b, err)
			b.SetBytes(uncompressed)
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				for _, r := range rows {
					require.NoError(b, builder.Append("tenant", logproto.Stream{Labels: r.labels, Entries: []push.Entry{r.entry}}, r.entry.Timestamp))
				}
				b.StartTimer()
				sorted, closer, err := builder.FlushAndSort(b.Context())
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

// canonicalTies returns records with the order of the records that share a
// tenant, stream and timestamp made canonical. The sort does not define the
// order of such records, and it can depend on how the input sections split.
func canonicalTies(records []string) []string {
	out := slices.Clone(records)
	tieKey := func(record string) string {
		return strings.Join(strings.SplitN(record, "|", 4)[:3], "|")
	}
	for start := 0; start < len(out); {
		end := start + 1
		for end < len(out) && tieKey(out[end]) == tieKey(out[start]) {
			end++
		}
		slices.Sort(out[start:end])
		start = end
	}
	return out
}

// objectStreams returns the ID, row count and uncompressed size of every
// stream of obj, keyed by tenant and labels.
func objectStreams(t *testing.T, obj *dataobj.Object) map[string]string {
	t.Helper()

	got := map[string]string{}
	for _, sec := range obj.Sections().Filter(streams.CheckSection) {
		streamsSection, err := streams.Open(t.Context(), sec)
		require.NoError(t, err)
		for res := range streams.IterSection(t.Context(), streamsSection) {
			stream, err := res.Value()
			require.NoError(t, err)
			got[sec.Tenant+"|"+stream.Labels.String()] = fmt.Sprintf("id=%d rows=%d size=%d", stream.ID, stream.Rows, stream.UncompressedSize)
		}
	}
	return got
}
