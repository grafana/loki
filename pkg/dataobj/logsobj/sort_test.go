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
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestPlanSortRanges(t *testing.T) {
	distinctKeys := func(n int) []streams.SortKey {
		keys := make([]streams.SortKey, n)
		for i := range keys {
			keys[i] = streams.SortKey{Hash: uint64(i)}
		}
		return keys
	}
	streamsOf := func(sizes []int64, keys []streams.SortKey) []sortedStream {
		out := make([]sortedStream, len(sizes))
		for i := range out {
			out[i] = sortedStream{Stream: streams.Stream{ID: int64(i + 1)}, key: keys[i], uncompressedBytes: sizes[i]}
		}
		return out
	}
	idsOf := func(ranges [][]sortedStream) [][]int64 {
		var out [][]int64
		for _, r := range ranges {
			ids := make([]int64, len(r))
			for i := range r {
				ids[i] = r[i].ID
			}
			out = append(out, ids)
		}
		return out
	}

	for _, tc := range []struct {
		name  string
		sizes []int64
		keys  []streams.SortKey
		want  [][]int64
	}{
		{
			name: "returns no range when there are no streams",
			want: nil,
		},
		{
			name:  "returns one range when all streams fit in the target size",
			sizes: []int64{10, 10, 10},
			keys:  distinctKeys(3),
			want:  [][]int64{{1, 2, 3}},
		},
		{
			name:  "ends a range before the stream that would take it over the target size",
			sizes: []int64{60, 30, 50, 80, 10},
			keys:  distinctKeys(5),
			want:  [][]int64{{1, 2}, {3}, {4, 5}},
		},
		{
			name:  "keeps streams with equal sort keys in one range even when it exceeds the target size",
			sizes: []int64{60, 50, 30, 40},
			keys:  []streams.SortKey{{Hash: 1}, {Hash: 1}, {Hash: 2}, {Hash: 3}},
			want:  [][]int64{{1, 2}, {3, 4}},
		},
		{
			name:  "keeps three streams with equal sort keys in one range even when it exceeds the target size",
			sizes: []int64{60, 60, 60, 10},
			keys:  []streams.SortKey{{Hash: 1}, {Hash: 1}, {Hash: 1}, {Hash: 2}},
			want:  [][]int64{{1, 2, 3}, {4}},
		},
		{
			name:  "keeps streams in one range when their total equals the target size",
			sizes: []int64{60, 40, 10},
			keys:  distinctKeys(3),
			want:  [][]int64{{1, 2}, {3}},
		},
		{
			name:  "returns one range for a single stream",
			sizes: []int64{500},
			keys:  distinctKeys(1),
			want:  [][]int64{{1}},
		},
		{
			name:  "puts a stream larger than the target size in a range of its own",
			sizes: []int64{10, 500, 10},
			keys:  distinctKeys(3),
			want:  [][]int64{{1}, {2}, {3}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, idsOf(planSortRanges(streamsOf(tc.sizes, tc.keys), 100)))
		})
	}

	t.Run("returns ranges that share the memory of the sorted streams", func(t *testing.T) {
		sortedStreams := streamsOf([]int64{60, 60}, distinctKeys(2))
		ranges := planSortRanges(sortedStreams, 100)
		require.Len(t, ranges, 2)

		ranges[1][0].Rows = 7
		require.Equal(t, 7, sortedStreams[1].Rows)
	})
}

func TestNewSortedStreams(t *testing.T) {
	seqOf := func(list ...streams.Stream) result.Seq[streams.Stream] {
		return result.Iter(func(yield func(streams.Stream) bool) error {
			for _, s := range list {
				if !yield(s) {
					return nil
				}
			}
			return nil
		})
	}
	stream := func(id int64, app string, rows int, size int64) streams.Stream {
		return streams.Stream{ID: id, Labels: labels.FromStrings("app", app), Rows: rows, UncompressedSize: size}
	}
	schema := []string{"label:app"}

	t.Run("gives the streams the new IDs 1 to N in sort key order", func(t *testing.T) {
		input := []streams.Stream{stream(1, "c", 0, 0), stream(2, "a", 0, 0), stream(3, "b", 0, 0)}
		keyOf := func(s streams.Stream) streams.SortKey {
			key, err := streamSortKey(s.Labels, schema, streams.DefaultShardLayout())
			require.NoError(t, err)
			return key
		}
		wantOrder := slices.Clone(input)
		slices.SortFunc(wantOrder, func(a, b streams.Stream) int { return streams.CompareSortKey(keyOf(a), keyOf(b)) })

		sortedStreams, err := newSortedStreams(seqOf(input...), "tenant", schema, streams.DefaultShardLayout(), 3)
		require.NoError(t, err)

		require.Len(t, sortedStreams, len(wantOrder))
		for i, want := range wantOrder {
			require.Equal(t, want.ID, sortedStreams[i].oldID, "old ID at position %d", i)
			require.Equal(t, int64(i+1), sortedStreams[i].ID, "new ID at position %d", i)
			require.Equal(t, keyOf(want), sortedStreams[i].key, "sort key at position %d", i)
		}
	})

	t.Run("takes the section size from the stream stats and starts the counters at zero", func(t *testing.T) {
		sortedStreams, err := newSortedStreams(seqOf(stream(1, "a", 4, 100)), "tenant", schema, streams.DefaultShardLayout(), 1)
		require.NoError(t, err)

		require.Equal(t, int64(100+4*logs.RecordOverhead), sortedStreams[0].uncompressedBytes)
		require.Zero(t, sortedStreams[0].Rows)
		require.Zero(t, sortedStreams[0].UncompressedSize)
	})

	t.Run("returns an error when an old ID is out of range", func(t *testing.T) {
		_, err := newSortedStreams(seqOf(stream(1, "a", 0, 0), stream(5, "b", 0, 0)), "tenant", schema, streams.DefaultShardLayout(), 2)
		require.ErrorContains(t, err, "stream id 5 out of range for tenant tenant with 2 streams")
	})

	t.Run("returns an error when the section yields fewer streams than it has rows", func(t *testing.T) {
		_, err := newSortedStreams(seqOf(stream(1, "a", 0, 0), stream(3, "b", 0, 0)), "tenant", schema, streams.DefaultShardLayout(), 3)
		require.ErrorContains(t, err, "the streams section of tenant tenant has 3 rows, but it holds 2 streams")
	})

	t.Run("returns an error when two streams have the same old ID", func(t *testing.T) {
		_, err := newSortedStreams(seqOf(stream(1, "a", 0, 0), stream(1, "b", 0, 0)), "tenant", schema, streams.DefaultShardLayout(), 2)
		require.ErrorContains(t, err, "duplicate stream id for tenant tenant: old id 1")
	})
}

func TestSortKeysByOldID(t *testing.T) {
	t.Run("indexes the sort keys by old ID and leaves index 0 empty", func(t *testing.T) {
		sortedStreams := []sortedStream{
			{oldID: 2, key: streams.SortKey{Hash: 20}},
			{oldID: 3, key: streams.SortKey{Hash: 30}},
			{oldID: 1, key: streams.SortKey{Hash: 10}},
		}

		require.Equal(t, []streams.SortKey{{}, {Hash: 10}, {Hash: 20}, {Hash: 30}}, sortKeysByOldID(sortedStreams))
	})
}

func TestBuilder_FlushAndSortParallelism(t *testing.T) {
	const (
		numRows      = 3000
		smallSection = 32 << 10
		hugeSection  = 1 << 29
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
		const tenant = "tenant-a"
		builder, err := NewBuilder(tenant, cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
		require.NoError(t, err)

		now := time.Date(2026, time.October, 19, 0, 0, 0, 0, time.UTC)
		for i := range numRows {
			n := i - i%9*(i%2)
			ts := now.Add(time.Duration(n%97) * time.Second)
			require.NoError(t, builder.Append(logproto.Stream{
				Labels: sh.labels(n),
				Entries: []push.Entry{{
					Timestamp:          ts,
					Line:               sh.line(tenant, n),
					StructuredMetadata: push.LabelsAdapter{{Name: "trace_id", Value: fmt.Sprintf("%08x", n*2654435761)}},
				}},
			}, ts))
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
	}{
		{
			name:      "gives the records and stream stats of a single range for many small streams with the default sort schema",
			shape:     manySmallStreams,
			overrides: NoTenantOverrides{},
		},
		{
			name:      "gives the records and stream stats of a single range for many small streams with a custom sort schema",
			shape:     manySmallStreams,
			overrides: tenantOverrides{"tenant-a": {"label:pod"}},
		},
		{
			name:           "drops the duplicates of a single range for many small streams",
			shape:          manySmallStreams,
			overrides:      NoTenantOverrides{},
			dropDuplicates: true,
		},
		{
			name:      "gives the records and stream stats of a single range for few streams that are larger than a section",
			shape:     fewStreamsLargerThanASection,
			overrides: NoTenantOverrides{},
		},
		{
			name:           "drops the duplicates of a single range for few streams that are larger than a section",
			shape:          fewStreamsLargerThanASection,
			overrides:      NoTenantOverrides{},
			dropDuplicates: true,
		},
		{
			name:      "gives the records and stream stats of a single range for a tenant with one stream",
			shape:     oneStream,
			overrides: NoTenantOverrides{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A section size above the size of all records puts every stream in
			// one range, which is a plain merge of the sections.
			oracle := flushAndSort(t, tc.shape, tc.overrides, tc.dropDuplicates, 1, hugeSection)
			require.Equal(t, 1, oracle.Sections().Count(logs.CheckSection), "the oracle must have one section")

			wantRecords := canonicalTies(objectRecords(t, oracle))
			if tc.dropDuplicates {
				require.Less(t, len(wantRecords), numRows, "the input must hold duplicates")
			} else {
				require.Len(t, wantRecords, numRows)
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
				require.Greater(t, got.Sections().Count(logs.CheckSection), 2, "the sort with %s must cut the logs into several sections", name)
			}
		})
	}
}

func TestBuilder_WriteLogsSections(t *testing.T) {
	const (
		numStreams    = 20
		rowsPerStream = 10
	)

	// newRun builds an object with numStreams streams of rowsPerStream records,
	// and cuts its streams into five ranges of four streams.
	newRun := func(t *testing.T, parallelism int) (sorter *Builder, sections []*dataobj.Section, plan *sortPlan) {
		t.Helper()
		builder, err := NewBuilder("tenant", testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
		require.NoError(t, err)
		now := time.Date(2026, time.October, 19, 0, 0, 0, 0, time.UTC)
		for i := range numStreams * rowsPerStream {
			require.NoError(t, builder.Append(logproto.Stream{
				Labels:  fmt.Sprintf(`{pod="pod-%d"}`, i%numStreams),
				Entries: []push.Entry{{Timestamp: now.Add(time.Duration(i) * time.Second), Line: "line"}},
			}, now))
		}
		src, closer, err := builder.flushWithoutLayout()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		sections, rawStreamsSection, err := tenantSections(src, "tenant")
		require.NoError(t, err)
		streamsSection, err := streams.Open(t.Context(), rawStreamsSection)
		require.NoError(t, err)
		sortedStreams, err := newSortedStreams(streamsSectionIter(t.Context(), streamsSection), "tenant", nil, streams.DefaultShardLayout(), streamsSection.NumRows())
		require.NoError(t, err)
		require.Len(t, sortedStreams, numStreams)
		plan = &sortPlan{streams: sortedStreams}
		for start := 0; start < numStreams; start += 4 {
			plan.ranges = append(plan.ranges, sortedStreams[start:start+4])
		}

		cfg := testBuilderConfig
		cfg.SortParallelism = parallelism
		sorter, err = NewBuilder("tenant", cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
		require.NoError(t, err)
		return sorter, sections, plan
	}

	t.Run("adds the records of every range to the counters of their streams", func(t *testing.T) {
		sorter, sections, plan := newRun(t, 2)

		require.NoError(t, sorter.writeLogsSections(t.Context(), "tenant", sections, plan))
		for _, s := range plan.streams {
			require.Equal(t, rowsPerStream, s.Rows, "rows of stream %d", s.ID)
		}
	})

	t.Run("returns the error of a range, with the range and the tenant, when more ranges than goroutines fail", func(t *testing.T) {
		sorter, sections, plan := newRun(t, 2)
		for i := 1; i < len(plan.ranges); i++ {
			plan.ranges[i] = nil
		}

		err := sorter.writeLogsSections(t.Context(), "tenant", sections, plan)
		require.ErrorContains(t, err, "no stream IDs to keep")
		require.ErrorContains(t, err, "sorting stream range")
		require.ErrorContains(t, err, "of tenant tenant")
	})

	t.Run("passes the sections to release after each flush and returns what release keeps", func(t *testing.T) {
		sorter, sections, plan := newRun(t, 1)
		sorter.cfg.TargetSectionSize = 1
		lb := logs.NewBuilder(sorter.sortLogsMetrics, logs.BuilderOptions{
			PageSizeHint:   int(sorter.cfg.TargetPageSize),
			AppendStrategy: logs.AppendOrdered,
			SortOrder:      logs.SortSchemaASC,
		})
		lb.SetTenant("tenant")
		var released []int

		kept, _, err := sorter.sortRange(t.Context(), "tenant", sections, sortKeysByOldID(plan.streams), plan.ranges[0], lb,
			func(pending []*encodedSection) ([]*encodedSection, error) {
				released = append(released, len(pending))
				return nil, nil
			})

		require.NoError(t, err)
		require.Empty(t, kept)
		total := 0
		for _, n := range released {
			require.LessOrEqual(t, n, 1, "release must see each section once, because it keeps none")
			total += n
		}
		require.Greater(t, total, 1, "a section size of 1 byte must cut the range into several sections")
	})

	t.Run("returns an error when a logs row belongs to a stream that no range lists", func(t *testing.T) {
		sorter, sections, plan := newRun(t, 2)
		plan.ranges = plan.ranges[:len(plan.ranges)-1]

		err := sorter.writeLogsSections(t.Context(), "tenant", sections, plan)

		require.ErrorContains(t, err, "the logs sections of tenant tenant hold 200 rows, but the sort read 160")
	})

	t.Run("returns the context error when the context is canceled", func(t *testing.T) {
		sorter, sections, plan := newRun(t, 2)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		require.ErrorIs(t, sorter.writeLogsSections(ctx, "tenant", sections, plan), context.Canceled)
	})

	t.Run("returns an error and leaves the counters at zero when there are no ranges but the logs sections hold rows", func(t *testing.T) {
		sorter, sections, plan := newRun(t, 2)
		plan.ranges = nil

		err := sorter.writeLogsSections(t.Context(), "tenant", sections, plan)

		require.ErrorContains(t, err, "hold 200 rows, but the sort read 0")
		for _, s := range plan.streams {
			require.Zero(t, s.Rows)
		}
	})
}

// BenchmarkBuilder_FlushAndSortParallelism sorts an object with compressible
// logfmt lines, many streams and structured metadata.
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
			builder, err := NewBuilder(testTenant, cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
			require.NoError(b, err)
			b.SetBytes(uncompressed)
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				for _, r := range rows {
					require.NoError(b, builder.Append(logproto.Stream{Labels: r.labels, Entries: []push.Entry{r.entry}}, r.entry.Timestamp))
				}
				b.StartTimer()
				sortedStreams, closer, err := builder.FlushAndSort(b.Context())
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(sortedStreams.Sections().Count(logs.CheckSection)), "sections")
				_ = closer.Close()
			}
		})
	}
}

// objectRecords returns every log record of obj in the order of the object,
// with the labels of its stream instead of the stream ID.
func objectRecords(t *testing.T, obj *dataobj.Object) []string {
	t.Helper()

	var records []string
	for _, sec := range obj.Sections().Filter(logs.CheckSection) {
		streamLabels := tenantStreamLabels(t, obj, sec.Tenant)
		logsSection, err := logs.Open(t.Context(), sec)
		require.NoError(t, err)
		for res := range logs.IterSection(t.Context(), logsSection) {
			rec, err := res.Value()
			require.NoError(t, err)
			ls, ok := streamLabels[rec.StreamID]
			require.True(t, ok, "record of tenant %s refers to unknown stream %d", sec.Tenant, rec.StreamID)
			records = append(records, fmt.Sprintf("%s|%s|%d|%s|%s", sec.Tenant, ls, rec.Timestamp.UnixNano(), rec.Metadata, rec.Line))
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
