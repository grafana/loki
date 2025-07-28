package engine

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"

	"github.com/grafana/loki/pkg/push"
)

type ResultBuilder interface {
	CollectRecord(arrow.Record)
	Build() logqlmodel.Result
	Len() int
	SetStats(stats.Result)
}

var _ ResultBuilder = &streamsResultBuilder{}
var _ ResultBuilder = &vectorResultBuilder{}

func newStreamsResultBuilder() *streamsResultBuilder {
	return &streamsResultBuilder{
		streams: make(map[string]int),
	}
}

type streamsResultBuilder struct {
	streams map[string]int
	data    logqlmodel.Streams
	stats   stats.Result
	count   int
}

func (b *streamsResultBuilder) CollectRecord(rec arrow.Record) {
	for row := range int(rec.NumRows()) {
		stream, entry := b.collectRow(rec, row)

		// Ignore rows that don't have stream labels, log line, or timestamp
		if stream.Len() == 0 || entry.Line == "" || entry.Timestamp.Equal(time.Time{}) {
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
			}
			continue
		}
	}
	entry.StructuredMetadata = logproto.FromLabelsToLabelAdapters(metadata.Labels())

	return lbs.Labels(), entry
}

func (b *streamsResultBuilder) SetStats(s stats.Result) {
	b.stats = s
}

func (b *streamsResultBuilder) Build() logqlmodel.Result {
	return logqlmodel.Result{
		Data:       b.data,
		Statistics: b.stats,
	}
}

func (b *streamsResultBuilder) Len() int {
	return b.count
}

type vectorResultBuilder struct {
	data        promql.Vector
	lblsBuilder *labels.Builder
	stats       stats.Result
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
	var sample promql.Sample
	b.lblsBuilder.Reset(labels.EmptyLabels())

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

			sample.T = int64(col.(*array.Timestamp).Value(i))
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
			if colDataType == datatype.String.String() {
				b.lblsBuilder.Set(colName, col.(*array.String).Value(i))
			}
		}
	}

	sample.Metric = b.lblsBuilder.Labels()
	return sample, true
}

func (b *vectorResultBuilder) Build() logqlmodel.Result {
	return logqlmodel.Result{
		Data:       b.data,
		Statistics: b.stats,
	}
}

func (b *vectorResultBuilder) SetStats(s stats.Result) {
	b.stats = s
}

func (b *vectorResultBuilder) Len() int {
	return len(b.data)
}
