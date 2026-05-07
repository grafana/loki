package index

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

// BenchmarkFullPostingsPipeline drives both labelPostingsCalculation and
// columnValuesCalculation through a full Prepare → ProcessBatch → calc.Flush →
// builder.Flush cycle against a synthetic logs section.
//
// The workload exercises both the label posting path (per stream label per
// record) and the bloom posting path (per metadata column per record), which
// together account for nearly all postings-pipeline allocations during
// indexing.
func BenchmarkFullPostingsPipeline(b *testing.B) {
	cases := []struct {
		records         int
		streams         int
		streamLabels    int // number of stream labels per stream
		metaCols        int // number of metadata columns
		metaCardinality int // distinct values per metadata column
	}{
		{records: 10_000, streams: 100, streamLabels: 3, metaCols: 4, metaCardinality: 10},
		{records: 10_000, streams: 100, streamLabels: 3, metaCols: 20, metaCardinality: 100},
		{records: 100_000, streams: 1_000, streamLabels: 4, metaCols: 10, metaCardinality: 100},
		{records: 100_000, streams: 1_000, streamLabels: 4, metaCols: 20, metaCardinality: 1_000},
	}

	for _, tc := range cases {
		name := fmt.Sprintf("records=%d/streams=%d/labels=%d/metaCols=%d/metaCard=%d",
			tc.records, tc.streams, tc.streamLabels, tc.metaCols, tc.metaCardinality)
		b.Run(name, func(b *testing.B) {
			// Build inputs once outside the timing loop. ProcessBatch reads but
			// does not mutate them.
			streamLabelsMap := makePipelineBenchStreamLabels(tc.streams, tc.streamLabels)
			metaCols := makePipelineBenchMetadataColumnNames(tc.metaCols)
			batch := makePipelineBenchBatch(tc.records, tc.streams, metaCols, tc.metaCardinality)
			stats := makePipelineBenchStats(metaCols, tc.metaCardinality)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				builder, err := indexobj.NewBuilder(testCalculatorConfig, nil)
				if err != nil {
					b.Fatal(err)
				}
				calcCtx := &logsCalculationContext{
					tenantID:     "bench-tenant",
					objectPath:   "bench/path",
					sectionIdx:   0,
					streamLabels: streamLabelsMap,
					builder:      builder,
				}

				labelCalc := &labelPostingsCalculation{}
				colCalc := &columnValuesCalculation{}

				if err := labelCalc.Prepare(context.Background(), calcCtx, nil, stats); err != nil {
					b.Fatal(err)
				}
				if err := colCalc.Prepare(context.Background(), calcCtx, nil, stats); err != nil {
					b.Fatal(err)
				}

				if err := labelCalc.ProcessBatch(context.Background(), calcCtx, batch); err != nil {
					b.Fatal(err)
				}
				if err := colCalc.ProcessBatch(context.Background(), calcCtx, batch); err != nil {
					b.Fatal(err)
				}

				if err := labelCalc.Flush(context.Background(), calcCtx); err != nil {
					b.Fatal(err)
				}
				if err := colCalc.Flush(context.Background(), calcCtx); err != nil {
					b.Fatal(err)
				}

				obj, closer, err := builder.Flush()
				if err != nil {
					b.Fatal(err)
				}
				_ = obj
				_ = closer.Close()
			}
		})
	}
}

// makePipelineBenchStreamLabels builds a stream → labels map for the
// benchmark. Labels are drawn from a fixed set so that values overlap across
// streams (realistic) but each stream has a unique combination.
func makePipelineBenchStreamLabels(streams, labelsPerStream int) map[int64]labels.Labels {
	result := make(map[int64]labels.Labels, streams)
	baseLabelKeys := []string{"service_name", "cluster", "namespace", "region"}
	mods := []int{50, 5, 10, 3}
	for i := 0; i < streams; i++ {
		pairs := make([]string, 0, labelsPerStream*2)
		for j := 0; j < labelsPerStream && j < len(baseLabelKeys); j++ {
			pairs = append(pairs, baseLabelKeys[j], fmt.Sprintf("%s-%d", baseLabelKeys[j], i%mods[j]))
		}
		result[int64(i)] = labels.FromStrings(pairs...)
	}
	return result
}

// makePipelineBenchMetadataColumnNames returns n synthetic metadata column
// names of the form "meta_col_<i>".
func makePipelineBenchMetadataColumnNames(n int) []string {
	cols := make([]string, n)
	for i := 0; i < n; i++ {
		cols[i] = fmt.Sprintf("meta_col_%d", i)
	}
	return cols
}

// makePipelineBenchBatch builds a synthetic batch of log records. Each record
// has every metadata column present and rotates through metaCardinality
// distinct values per column, giving columnValuesCalculation a non-trivial
// bloom filter to populate.
func makePipelineBenchBatch(records, streams int, metaCols []string, metaCardinality int) []logs.Record {
	batch := make([]logs.Record, records)
	for i := range batch {
		metaPairs := make([]string, 0, len(metaCols)*2)
		for j, col := range metaCols {
			metaPairs = append(metaPairs, col, fmt.Sprintf("v-%d", (i+j)%metaCardinality))
		}
		batch[i] = logs.Record{
			StreamID:  int64(i % streams),
			Timestamp: time.Unix(int64(i), 0).UTC(),
			Line:      []byte("log line payload for benchmarking"),
			Metadata:  labels.FromStrings(metaPairs...),
		}
	}
	return batch
}

// makePipelineBenchStats builds a logs.Stats describing the metadata columns
// in the synthetic batch, as columnValuesCalculation.Prepare expects.
func makePipelineBenchStats(metaCols []string, cardinality int) logs.Stats {
	var s logs.Stats
	s.Columns = append(s.Columns, logs.ColumnStats{
		Name:        "stream_id",
		Type:        "stream_id",
		ColumnIndex: 0,
		Cardinality: 1000,
	})
	s.Columns = append(s.Columns, logs.ColumnStats{
		Name:        "timestamp",
		Type:        "timestamp",
		ColumnIndex: 1,
		Cardinality: 100,
	})
	for i, col := range metaCols {
		s.Columns = append(s.Columns, logs.ColumnStats{
			Name:        col,
			Type:        "metadata",
			ColumnIndex: int64(2 + i),
			Cardinality: uint64(cardinality),
		})
	}
	s.Columns = append(s.Columns, logs.ColumnStats{
		Name:        "message",
		Type:        "message",
		ColumnIndex: int64(2 + len(metaCols)),
		Cardinality: 1000,
	})
	return s
}
