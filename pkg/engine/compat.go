package engine

import (
	"sort"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"

	"github.com/grafana/loki/pkg/push"
)

type ResultBuilder interface {
	CollectRecord(arrow.Record)
	Build(stats.Result, *metadata.Context) logqlmodel.Result
	Len() int
}

var (
	_ ResultBuilder = &streamsResultBuilder{}
	_ ResultBuilder = &vectorResultBuilder{}
	_ ResultBuilder = &matrixResultBuilder{}
)

func newStreamsResultBuilder() *streamsResultBuilder {
	return &streamsResultBuilder{
		data:    make(logqlmodel.Streams, 0),
		streams: make(map[string]int),
	}
}

type streamsResultBuilder struct {
	streams map[string]int
	data    logqlmodel.Streams
	count   int
}

func (b *streamsResultBuilder) CollectRecord(rec arrow.Record) {
	for row := range int(rec.NumRows()) {
		stream, entry := b.collectRow(rec, row)

		// Ignore rows that don't have stream labels, log line, or timestamp
		if stream.IsEmpty() || entry.Line == "" || entry.Timestamp.Equal(time.Time{}) {
			continue
		}

		// Add the entry to the result builder
		key := stream.String()
		idx, ok := b.streams[key]
		if !ok {
			idx = len(b.data)
			b.streams[key] = idx
			b.data = append(b.data, push.Stream{Labels: key})
		}
		b.data[idx].Entries = append(b.data[idx].Entries, entry)
		b.count++
	}
}

func (b *streamsResultBuilder) collectRow(rec arrow.Record, i int) (labels.Labels, logproto.Entry) {
	var entry logproto.Entry
	lbs := labels.NewBuilder(labels.EmptyLabels())
	metadata := labels.NewBuilder(labels.EmptyLabels())
	parsed := labels.NewBuilder(labels.EmptyLabels())

	for colIdx := range int(rec.NumCols()) {
		col := rec.Column(colIdx)
		colName := rec.ColumnName(colIdx)

		// TODO(chaudum): We need to add metadata to columns to identify builtins, labels, metadata, and parsed.
		field := rec.Schema().Field(colIdx)
		colType, ok := field.Metadata.GetValue(types.MetadataKeyColumnType)

		// Ignore column values that are NULL or invalid or don't have a column typ
		if col.IsNull(i) || !col.IsValid(i) || !ok {
			continue
		}

		// Extract line
		if colName == types.ColumnNameBuiltinMessage && colType == types.ColumnTypeBuiltin.String() {
			entry.Line = col.(*array.String).Value(i)
			continue
		}

		// Extract timestamp
		if colName == types.ColumnNameBuiltinTimestamp && colType == types.ColumnTypeBuiltin.String() {
			entry.Timestamp = time.Unix(0, int64(col.(*array.Timestamp).Value(i)))
			continue
		}

		// Extract label
		if colType == types.ColumnTypeLabel.String() {
			switch arr := col.(type) {
			case *array.String:
				lbs.Set(colName, arr.Value(i))
			}
			continue
		}

		// Extract metadata
		if colType == types.ColumnTypeMetadata.String() {
			switch arr := col.(type) {
			case *array.String:
				metadata.Set(colName, arr.Value(i))
				// include structured metadata in stream labels
				lbs.Set(colName, arr.Value(i))
			}
			continue
		}

		// Extract parsed
		if colType == types.ColumnTypeParsed.String() {
			switch arr := col.(type) {
			case *array.String:
				// TODO: keep errors if --strict is set
				// These are reserved column names used to track parsing errors. We are dropping them until
				// we add support for --strict parsing.
				if colName == types.ColumnNameParsedError || colName == types.ColumnNameParsedErrorDetails {
					continue
				}

				if parsed.Get(colName) != "" {
					continue
				}

				parsed.Set(colName, arr.Value(i))
				lbs.Set(colName, arr.Value(i))
				if metadata.Get(colName) != "" {
					metadata.Del(colName)
				}
			}
		}
	}
	entry.StructuredMetadata = logproto.FromLabelsToLabelAdapters(metadata.Labels())
	entry.Parsed = logproto.FromLabelsToLabelAdapters(parsed.Labels())

	return lbs.Labels(), entry
}

func (b *streamsResultBuilder) Build(s stats.Result, md *metadata.Context) logqlmodel.Result {
	sort.Sort(b.data)
	return logqlmodel.Result{
		Data:       b.data,
		Statistics: s,
		Headers:    md.Headers(),
		Warnings:   md.Warnings(),
	}
}

func (b *streamsResultBuilder) Len() int {
	return b.count
}

type vectorResultBuilder struct {
	data        promql.Vector
	lblsBuilder *labels.Builder
}

func newVectorResultBuilder() *vectorResultBuilder {
	return &vectorResultBuilder{
		data:        promql.Vector{},
		lblsBuilder: labels.NewBuilder(labels.EmptyLabels()),
	}
}

func (b *vectorResultBuilder) CollectRecord(rec arrow.Record) {
	for row := range int(rec.NumRows()) {
		sample, ok := b.collectRow(rec, row)
		if !ok {
			continue
		}

		b.data = append(b.data, sample)
	}
}

func (b *vectorResultBuilder) collectRow(rec arrow.Record, i int) (promql.Sample, bool) {
	return collectSamplesFromRow(b.lblsBuilder, rec, i)
}

func (b *vectorResultBuilder) Build(s stats.Result, md *metadata.Context) logqlmodel.Result {
	sort.Slice(b.data, func(i, j int) bool {
		return labels.Compare(b.data[i].Metric, b.data[j].Metric) < 0
	})
	return logqlmodel.Result{
		Data:       b.data,
		Statistics: s,
		Headers:    md.Headers(),
		Warnings:   md.Warnings(),
	}
}

func (b *vectorResultBuilder) Len() int {
	return len(b.data)
}

type matrixResultBuilder struct {
	seriesIndex map[uint64]promql.Series
	lblsBuilder *labels.Builder
}

func newMatrixResultBuilder() *matrixResultBuilder {
	return &matrixResultBuilder{
		seriesIndex: make(map[uint64]promql.Series),
		lblsBuilder: labels.NewBuilder(labels.EmptyLabels()),
	}
}

func (b *matrixResultBuilder) CollectRecord(rec arrow.Record) {
	for row := range int(rec.NumRows()) {
		sample, ok := b.collectRow(rec, row)
		if !ok {
			continue
		}

		// TODO(ashwanth): apply query series limits.

		// Group samples by series (labels hash)
		hash := labels.StableHash(sample.Metric)
		series, exists := b.seriesIndex[hash]

		if !exists {
			// Create new series
			series = promql.Series{
				Metric: sample.Metric,
				Floats: make([]promql.FPoint, 0, 1),
			}
		}

		series.Floats = append(series.Floats, promql.FPoint{
			T: sample.T,
			F: sample.F,
		})

		b.seriesIndex[hash] = series
	}
}

func (b *matrixResultBuilder) collectRow(rec arrow.Record, i int) (promql.Sample, bool) {
	return collectSamplesFromRow(b.lblsBuilder, rec, i)
}

func (b *matrixResultBuilder) Build(s stats.Result, md *metadata.Context) logqlmodel.Result {
	series := make([]promql.Series, 0, len(b.seriesIndex))
	for _, s := range b.seriesIndex {
		series = append(series, s)
	}

	// Create matrix and sort it
	result := promql.Matrix(series)
	sort.Sort(result)

	return logqlmodel.Result{
		Data:       result,
		Statistics: s,
		Headers:    md.Headers(),
		Warnings:   md.Warnings(),
	}
}

func (b *matrixResultBuilder) Len() int {
	total := 0
	for _, series := range b.seriesIndex {
		total += len(series.Floats) + len(series.Histograms)
	}

	return total
}

func collectSamplesFromRow(builder *labels.Builder, rec arrow.Record, i int) (promql.Sample, bool) {
	var sample promql.Sample
	builder.Reset(labels.EmptyLabels())

	// TODO: we add a lot of overhead by reading row by row. Switch to vectorized conversion.
	for colIdx := range int(rec.NumCols()) {
		col := rec.Column(colIdx)
		colName := rec.ColumnName(colIdx)

		field := rec.Schema().Field(colIdx)
		colDataType, ok := field.Metadata.GetValue(types.MetadataKeyColumnDataType)
		if !ok {
			return promql.Sample{}, false
		}

		switch colName {
		case types.ColumnNameBuiltinTimestamp:
			if col.IsNull(i) {
				return promql.Sample{}, false
			}

			// [promql.Sample] expects milliseconds as timestamp unit
			sample.T = int64(col.(*array.Timestamp).Value(i) / 1e6)
		case types.ColumnNameGeneratedValue:
			if col.IsNull(i) {
				return promql.Sample{}, false
			}

			col, ok := col.(*array.Int64)
			if !ok {
				return promql.Sample{}, false
			}
			sample.F = float64(col.Value(i))
		default:
			// allow any string columns
			if colDataType == datatype.Loki.String.String() {
				builder.Set(colName, col.(*array.String).Value(i))
			}
		}
	}

	sample.Metric = builder.Labels()
	return sample, true
}
