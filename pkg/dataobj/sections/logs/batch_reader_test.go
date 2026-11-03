package logs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestBatchReader(t *testing.T) {
	section := buildBatchTestSection(t, SortStreamASC)

	start := unixSecond(1_700_000_000)
	timeRange := func(from, to int, includeStart, includeEnd bool) RowPredicate {
		return TimeRangeRowPredicate{
			StartTime:    start.Add(time.Duration(from) * time.Second),
			EndTime:      start.Add(time.Duration(to) * time.Second),
			IncludeStart: includeStart,
			IncludeEnd:   includeEnd,
		}
	}
	contains := func(needle string) RowPredicate {
		keep := func(line []byte) bool { return bytes.Contains(line, []byte(needle)) }
		return LogMessageFilterRowPredicate{Keep: keep, KeepBatch: keepBatchOf(keep)}
	}
	containsRowwise := func(needle string) RowPredicate {
		return LogMessageFilterRowPredicate{Keep: func(line []byte) bool { return bytes.Contains(line, []byte(needle)) }}
	}

	type config struct {
		streams    []int64
		predicates []RowPredicate
		types      []ColumnType
		metadata   []string
	}

	for name, cfg := range map[string]config{
		"no filters": {},
		"one stream": {streams: []int64{3}},
		"streams":    {streams: []int64{2, 5, 7, 11, 999}},
		"time range, start inclusive and end exclusive": {
			predicates: []RowPredicate{timeRange(1000, 9000, true, false)},
		},
		"time range, both exclusive": {
			predicates: []RowPredicate{timeRange(1000, 9000, false, false)},
		},
		"time range, both inclusive": {
			predicates: []RowPredicate{timeRange(1000, 9000, true, true)},
		},
		"streams and time range": {
			streams:    []int64{1, 4, 9},
			predicates: []RowPredicate{timeRange(2000, 4000, true, false)},
		},
		"line filter":                   {predicates: []RowPredicate{contains("error")}},
		"line filter without KeepBatch": {predicates: []RowPredicate{containsRowwise("error")}},
		"selective line filter":         {predicates: []RowPredicate{contains("request=1234 ")}},
		"streams, time range and line filter": {
			streams:    []int64{1, 2, 3, 4, 5, 6},
			predicates: []RowPredicate{timeRange(0, 15_000, true, false), contains("warn")},
		},
		"metadata equality":       {predicates: []RowPredicate{MetadataMatcherRowPredicate{Key: "env", Value: "prod"}}},
		"metadata equality empty": {predicates: []RowPredicate{MetadataMatcherRowPredicate{Key: "trace_id", Value: ""}}},
		"metadata equality on an absent key": {
			predicates: []RowPredicate{MetadataMatcherRowPredicate{Key: "absent", Value: "x"}},
		},
		"metadata filter": {predicates: []RowPredicate{MetadataFilterRowPredicate{
			Key:  "trace_id",
			Keep: func(_, value string) bool { return strings.HasSuffix(value, "7") },
		}}},
		"not metadata filter": {predicates: []RowPredicate{NotRowPredicate{Inner: MetadataFilterRowPredicate{
			Key:  "trace_id",
			Keep: func(_, value string) bool { return value != "" },
		}}}},
		"or of line filters": {predicates: []RowPredicate{OrRowPredicate{
			Left:  contains("error"),
			Right: contains("panic"),
		}}},
		"and of metadata and line filter": {predicates: []RowPredicate{AndRowPredicate{
			Left:  MetadataMatcherRowPredicate{Key: "env", Value: "dev"},
			Right: contains("warn"),
		}}},
		"not line filter": {predicates: []RowPredicate{NotRowPredicate{Inner: contains("error")}}},
		"several predicates": {predicates: []RowPredicate{
			timeRange(500, 18_000, true, false),
			MetadataMatcherRowPredicate{Key: "env", Value: "prod"},
			contains("info"),
		}},
		"projection of ids and time": {
			streams:    []int64{2, 3},
			predicates: []RowPredicate{contains("error")},
			types:      []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp},
		},
		"projection of one metadata key": {
			predicates: []RowPredicate{timeRange(0, 5000, true, false)},
			types:      []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp, ColumnTypeMessage},
			metadata:   []string{"trace_id"},
		},
		"projection of all metadata": {
			predicates: []RowPredicate{contains("error")},
			types:      []ColumnType{ColumnTypeTimestamp, ColumnTypeMetadata},
		},
		"projection without the message but filtering on it": {
			predicates: []RowPredicate{contains("error")},
			types:      []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp},
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, batchSize := range []int{1, 100, 1024, 100_000} {
				want := readRecordsWithRowReader(t, section, cfg.streams, cfg.predicates, cfg.types, cfg.metadata, batchSize)
				got := readRecordsWithBatchReader(t, section, cfg.streams, cfg.predicates, cfg.types, cfg.metadata, batchSize)
				require.Equal(t, want, got, "batch size %d", batchSize)
				if batchSize == 1024 {
					t.Logf("%d records", len(want))
				}
			}
		})
	}

	t.Run("returns an error when read before open", func(t *testing.T) {
		reader := NewBatchReader(section)
		defer reader.Close()

		var alloc memory.Allocator
		_, err := reader.Read(t.Context(), &alloc, 1)
		require.ErrorIs(t, err, errRowReaderNotOpen)
	})

	t.Run("rejects settings after open", func(t *testing.T) {
		reader := NewBatchReader(section)
		defer reader.Close()
		require.NoError(t, reader.Open(t.Context()))

		require.Error(t, reader.MatchStreams(slices.Values([]int64{1})))
		require.Error(t, reader.SetPredicates(nil))
		require.Error(t, reader.SetProjectedColumns([]ColumnType{ColumnTypeMessage}, nil))
	})

	t.Run("fails to open when none of the projected columns exist", func(t *testing.T) {
		reader := NewBatchReader(section)
		defer reader.Close()
		require.NoError(t, reader.SetProjectedColumns(nil, []string{"absent"}))
		require.Error(t, reader.Open(t.Context()))
	})

	t.Run("reads from the start again after reset", func(t *testing.T) {
		reader := NewBatchReader(section)
		defer reader.Close()

		first := drainBatchReader(t, reader, 512)
		reader.Reset(section)
		second := drainBatchReader(t, reader, 512)
		require.Equal(t, first, second)
		require.NotEmpty(t, first)
	})

	t.Run("reads a section sorted by timestamp", func(t *testing.T) {
		byTimestamp := buildBatchTestSection(t, SortTimestampDESC)
		streams := []int64{3, 4}
		predicates := []RowPredicate{contains("error"), timeRange(0, 10_000, true, false)}

		want := readRecordsWithRowReader(t, byTimestamp, streams, predicates, nil, nil, 256)
		got := readRecordsWithBatchReader(t, byTimestamp, streams, predicates, nil, nil, 256)
		require.Equal(t, want, got)
		require.NotEmpty(t, got)
	})
}

// buildBatchTestSection builds a section with several thousand records across
// a few dozen streams. The records have lines with a few shapes, structured
// metadata that is present in some of the records, and increasing timestamps.
func buildBatchTestSection(t *testing.T, order SortOrder) *Section {
	t.Helper()

	var (
		rng     = rand.New(rand.NewSource(7))
		start   = unixSecond(1_700_000_000)
		levels  = []string{"info", "warn", "error", "debug", "panic"}
		envs    = []string{"prod", "dev", "staging"}
		records = make([]Record, 0, 20_000)
	)
	for i := range 20_000 {
		var metadata labels.Labels
		builder := labels.NewScratchBuilder(2)
		if rng.Intn(3) > 0 {
			builder.Add("env", envs[rng.Intn(len(envs))])
		}
		if rng.Intn(2) == 0 {
			builder.Add("trace_id", fmt.Sprintf("t%d", rng.Intn(100)))
		}
		builder.Sort()
		metadata = builder.Labels()

		records = append(records, Record{
			StreamID:  int64(1 + (i/200)%40),
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Metadata:  metadata,
			Line:      fmt.Appendf(nil, "level=%s request=%d msg=%s", levels[rng.Intn(len(levels))], rng.Intn(5000), strings.Repeat("x", rng.Intn(30))),
		})
	}
	return buildSectionFromRecords(t, order, records)
}

func readRecordsWithRowReader(t *testing.T, section *Section, streams []int64, predicates []RowPredicate, types []ColumnType, metadata []string, batchSize int) []string {
	t.Helper()

	reader := NewRowReader(section)
	defer reader.Close()
	if len(streams) > 0 {
		require.NoError(t, reader.MatchStreams(slices.Values(streams)))
	}
	require.NoError(t, reader.SetPredicates(predicates))
	if len(types) > 0 || len(metadata) > 0 {
		require.NoError(t, reader.SetProjectedColumns(types, metadata))
	}
	require.NoError(t, reader.Open(t.Context()))

	var (
		out []string
		buf = make([]Record, batchSize)
	)
	for {
		n, err := reader.Read(t.Context(), buf)
		for i := range buf[:n] {
			out = append(out, recordText(&buf[i]))
		}
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
	}
}

func readRecordsWithBatchReader(t *testing.T, section *Section, streams []int64, predicates []RowPredicate, types []ColumnType, metadata []string, batchSize int) []string {
	t.Helper()

	reader := NewBatchReader(section)
	defer reader.Close()
	if len(streams) > 0 {
		require.NoError(t, reader.MatchStreams(slices.Values(streams)))
	}
	require.NoError(t, reader.SetPredicates(predicates))
	if len(types) > 0 || len(metadata) > 0 {
		require.NoError(t, reader.SetProjectedColumns(types, metadata))
	}
	return drainBatchReader(t, reader, batchSize)
}

func drainBatchReader(t *testing.T, reader *BatchReader, batchSize int) []string {
	t.Helper()
	require.NoError(t, reader.Open(t.Context()))

	var (
		out    []string
		alloc  memory.Allocator
		record Record
	)
	for {
		alloc.Reclaim()
		batch, err := reader.Read(t.Context(), &alloc, batchSize)
		if errors.Is(err, io.EOF) {
			require.Nil(t, batch)
			return out
		}
		require.NoError(t, err)
		require.Greater(t, batch.Rows, 0)
		require.LessOrEqual(t, batch.Rows, batchSize)

		for i := range batch.Rows {
			batch.Record(i, &record)
			out = append(out, recordText(&record))
		}
	}
}

func recordText(r *Record) string {
	return fmt.Sprintf("%d|%d|%s|%s", r.StreamID, r.Timestamp.UnixNano(), r.Metadata.String(), r.Line)
}

func BenchmarkBatchReader(b *testing.B) {
	var (
		rng    = rand.New(rand.NewSource(1))
		start  = unixSecond(1_700_000_000)
		levels = []string{"info", "warn", "error", "debug"}
	)

	const recordCount = 200_000
	records := make([]Record, 0, recordCount)
	for i := range recordCount {
		builder := labels.NewScratchBuilder(3)
		builder.Add("env", []string{"prod", "dev", "staging"}[rng.Intn(3)])
		builder.Add("pod", fmt.Sprintf("pod-%d", rng.Intn(50)))
		if rng.Intn(4) == 0 {
			builder.Add("trace_id", fmt.Sprintf("%032x", rng.Int63()))
		}
		builder.Sort()

		records = append(records, Record{
			StreamID:  int64(1 + i/2000),
			Timestamp: start.Add(time.Duration(i) * time.Second),
			Metadata:  builder.Labels(),
			Line: fmt.Appendf(nil, `level=%s ts=%d method=GET path=/api/v1/items/%d status=%d duration=%dms user=%s msg="handled request"`,
				levels[rng.Intn(len(levels))], i, rng.Intn(10_000), 200+rng.Intn(5)*100, rng.Intn(500), strings.Repeat("u", rng.Intn(24))),
		})
	}

	logsBuilder := NewBuilder(nil, BuilderOptions{StripeMergeLimit: 2, SortOrder: SortStreamASC})
	for _, record := range records {
		logsBuilder.Append(record)
	}
	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(b, objBuilder.Append(logsBuilder))
	obj, closer, err := objBuilder.Flush()
	require.NoError(b, err)
	b.Cleanup(func() { closer.Close() })

	var section *Section
	for _, sec := range obj.Sections() {
		section, err = Open(context.Background(), sec)
		require.NoError(b, err)
	}

	var (
		queryStart = start.Add(10_000 * time.Second)
		queryEnd   = start.Add(190_000 * time.Second)
		window     = TimeRangeRowPredicate{StartTime: queryStart, EndTime: queryEnd, IncludeStart: true}
		contains   = func(needle string) RowPredicate {
			keep := func(line []byte) bool { return bytes.Contains(line, []byte(needle)) }
			return LogMessageFilterRowPredicate{Keep: keep, KeepBatch: keepBatchOf(keep)}
		}
		streamIDs = func(from, to int64) []int64 {
			var ids []int64
			for id := from; id <= to; id++ {
				ids = append(ids, id)
			}
			return ids
		}
		allColumns = []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp, ColumnTypeMessage, ColumnTypeMetadata}
	)

	cases := []struct {
		name       string
		streams    []int64
		predicates []RowPredicate
		types      []ColumnType
	}{
		{name: "stream-selector", streams: streamIDs(10, 20), predicates: []RowPredicate{window}, types: allColumns},
		{name: "stream-selector-no-line", streams: streamIDs(10, 20), predicates: []RowPredicate{window}, types: []ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp, ColumnTypeMetadata}},
		{name: "all-streams", predicates: []RowPredicate{window}, types: allColumns},
		{name: "line-filter-keeps-25-percent", predicates: []RowPredicate{window, contains("level=error")}, types: allColumns},
		{name: "line-filter-keeps-0.1-percent", predicates: []RowPredicate{window, contains("path=/api/v1/items/1234 ")}, types: allColumns},
		{name: "metadata-equality", predicates: []RowPredicate{window, MetadataMatcherRowPredicate{Key: "env", Value: "prod"}}, types: allColumns},
		{name: "streams+line-filter", streams: streamIDs(10, 40), predicates: []RowPredicate{window, contains("level=warn")}, types: allColumns},
	}

	for _, tc := range cases {
		b.Run(tc.name+"/row", func(b *testing.B) {
			b.ReportAllocs()
			buf := make([]Record, 1024)
			for b.Loop() {
				reader := NewRowReader(section)
				require.NoError(b, reader.MatchStreams(slices.Values(tc.streams)))
				require.NoError(b, reader.SetPredicates(tc.predicates))
				require.NoError(b, reader.SetProjectedColumns(tc.types, nil))
				require.NoError(b, reader.Open(b.Context()))

				var total int
				for {
					n, err := reader.Read(b.Context(), buf)
					total += n
					if errors.Is(err, io.EOF) {
						break
					}
					require.NoError(b, err)
				}
				_ = reader.Close()
				b.ReportMetric(float64(total), "rows/op")
			}
		})

		b.Run(tc.name+"/batch", func(b *testing.B) {
			b.ReportAllocs()
			var alloc memory.Allocator
			for b.Loop() {
				reader := NewBatchReader(section)
				require.NoError(b, reader.MatchStreams(slices.Values(tc.streams)))
				require.NoError(b, reader.SetPredicates(tc.predicates))
				require.NoError(b, reader.SetProjectedColumns(tc.types, nil))
				require.NoError(b, reader.Open(b.Context()))

				var total int
				for {
					alloc.Reclaim()
					batch, err := reader.Read(b.Context(), &alloc, 1024)
					if errors.Is(err, io.EOF) {
						break
					}
					require.NoError(b, err)
					total += batch.Rows
				}
				_ = reader.Close()
				b.ReportMetric(float64(total), "rows/op")
			}
		})
	}
}
