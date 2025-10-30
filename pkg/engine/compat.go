package engine

import (
	"sort"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
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
		data:        make(logqlmodel.Streams, 0),
		streams:     make(map[string]int),
		rowBuilders: nil,
	}
}

type streamsResultBuilder struct {
	streams map[string]int
	data    logqlmodel.Streams
	count   int

	// buffer for rows
	rowBuilders []rowBuilder
}

type rowBuilder struct {
	timestamp       time.Time
	line            string
	lbsBuilder      *labels.Builder
	metadataBuilder *labels.Builder
	parsedBuilder   *labels.Builder
}

func (b *streamsResultBuilder) CollectRecord(rec arrow.Record) {
	numRows := int(rec.NumRows())
	if numRows == 0 {
		return
	}

	// let's say we have the following log entries in rec:
	// - {labelenv="prod-1", metadatatrace="123-1", parsed="v1"} ts1 line 1
	// - {labelenv="prod-2", metadatatrace="123-2", parsed="v2"} ts2 line 2
	// - {labelenv="prod-3", metadatatrace="123-3", parsed="v3"} ts3 line 3
	// we pre-initialize slices to store column values for all the rows, e.g.:
	// rows          |    1    |    2    |    3    | ...
	// ==============+=========+=========+=========+====
	// timestamps    | r1 ts   | r2 ts   | r3 ts   | ...
	// lines         | r1 line | r2 line | r3 line | ...
	// ...
	// We iterate over the columns and convert the values to our format column by column, e.g.,
	// first all the timestamps, then all the log lines, etc.
	// After all the values are collected and converted we transform the columnar representation to a row-based one.

	b.ensureRowBuilders(numRows)

	// Convert arrow values to our format column by column
	for colIdx := range int(rec.NumCols()) {
		col := rec.Column(colIdx)

		field := rec.Schema().Field(colIdx)
		ident, err := semconv.ParseFQN(field.Name)
		if err != nil {
			continue
		}
		shortName := ident.ShortName()

		switch true {

		// Log line
		case ident.Equal(semconv.ColumnIdentMessage):
			lineCol := col.(*array.String)
			forEachNotNullRowColValue(numRows, lineCol, func(rowIdx int) {
				b.rowBuilders[rowIdx].line = lineCol.Value(rowIdx)
			})

		// Timestamp
		case ident.Equal(semconv.ColumnIdentTimestamp):
			tsCol := col.(*array.Timestamp)
			forEachNotNullRowColValue(numRows, tsCol, func(rowIdx int) {
				b.rowBuilders[rowIdx].timestamp = time.Unix(0, int64(tsCol.Value(rowIdx)))
			})

		// One of the label columns
		case ident.ColumnType() == types.ColumnTypeLabel:
			labelCol := col.(*array.String)
			forEachNotNullRowColValue(numRows, labelCol, func(rowIdx int) {
				b.rowBuilders[rowIdx].lbsBuilder.Set(shortName, labelCol.Value(rowIdx))
			})

		// One of the metadata columns
		case ident.ColumnType() == types.ColumnTypeMetadata:
			metadataCol := col.(*array.String)
			forEachNotNullRowColValue(numRows, metadataCol, func(rowIdx int) {
				val := metadataCol.Value(rowIdx)
				b.rowBuilders[rowIdx].metadataBuilder.Set(shortName, val)
				b.rowBuilders[rowIdx].lbsBuilder.Set(shortName, val)
			})

		// One of the parsed columns
		case ident.ColumnType() == types.ColumnTypeParsed:
			parsedCol := col.(*array.String)

			// TODO: keep errors if --strict is set
			// These are reserved column names used to track parsing errors. We are dropping them until
			// we add support for --strict parsing.
			if shortName == types.ColumnNameError || shortName == types.ColumnNameErrorDetails {
				continue
			}

			forEachNotNullRowColValue(numRows, parsedCol, func(rowIdx int) {
				parsedVal := parsedCol.Value(rowIdx)
				if b.rowBuilders[rowIdx].parsedBuilder.Get(shortName) != "" {
					return
				}
				b.rowBuilders[rowIdx].parsedBuilder.Set(shortName, parsedVal)
				b.rowBuilders[rowIdx].lbsBuilder.Set(shortName, parsedVal)
				if b.rowBuilders[rowIdx].metadataBuilder.Get(shortName) != "" {
					b.rowBuilders[rowIdx].metadataBuilder.Del(shortName)
				}
			})
		}
	}

	// Convert columnar representation to a row-based one
	for rowIdx := range numRows {
		lbs := b.rowBuilders[rowIdx].lbsBuilder.Labels()
		ts := b.rowBuilders[rowIdx].timestamp
		line := b.rowBuilders[rowIdx].line
		// Ignore rows that don't have stream labels, log line, or timestamp
		if line == "" || ts.IsZero() || lbs.IsEmpty() {
			b.resetRowBuilder(rowIdx)
			continue
		}

		entry := logproto.Entry{
			Timestamp:          ts,
			Line:               line,
			StructuredMetadata: logproto.FromLabelsToLabelAdapters(b.rowBuilders[rowIdx].metadataBuilder.Labels()),
			Parsed:             logproto.FromLabelsToLabelAdapters(b.rowBuilders[rowIdx].parsedBuilder.Labels()),
		}
		b.resetRowBuilder(rowIdx)

		// Add entry to appropriate stream
		key := lbs.String()
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

func (b *streamsResultBuilder) ensureRowBuilders(newLen int) {
	if newLen == len(b.rowBuilders) {
		return
	}

	if newLen < len(b.rowBuilders) {
		// free not used items at the end of the slices so they can be GC-ed
		clear(b.rowBuilders[newLen:len(b.rowBuilders)])
		b.rowBuilders = b.rowBuilders[:newLen]

		return
	}

	// newLen > buf.len
	numRowsToAdd := newLen - len(b.rowBuilders)
	oldLen := len(b.rowBuilders)
	b.rowBuilders = append(b.rowBuilders, make([]rowBuilder, numRowsToAdd)...)
	for i := oldLen; i < newLen; i++ {
		b.rowBuilders[i] = rowBuilder{
			lbsBuilder:      labels.NewBuilder(labels.EmptyLabels()),
			metadataBuilder: labels.NewBuilder(labels.EmptyLabels()),
			parsedBuilder:   labels.NewBuilder(labels.EmptyLabels()),
		}
	}
}

func (b *streamsResultBuilder) resetRowBuilder(i int) {
	b.rowBuilders[i].timestamp = time.Time{}
	b.rowBuilders[i].line = ""
	b.rowBuilders[i].lbsBuilder.Reset(labels.EmptyLabels())
	b.rowBuilders[i].metadataBuilder.Reset(labels.EmptyLabels())
	b.rowBuilders[i].parsedBuilder.Reset(labels.EmptyLabels())
}

func forEachNotNullRowColValue(numRows int, col arrow.Array, f func(rowIdx int)) {
	for rowIdx := 0; rowIdx < numRows; rowIdx++ {
		if col.IsNull(rowIdx) {
			continue
		}
		f(rowIdx)
	}
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
		field := rec.Schema().Field(colIdx)
		ident, err := semconv.ParseFQN(field.Name)
		if err != nil {
			return promql.Sample{}, false
		}

		shortName := ident.ShortName()

		// Extract timestamp
		if ident.Equal(semconv.ColumnIdentTimestamp) {
			// Ignore column values that are NULL or invalid
			if col.IsNull(i) || !col.IsValid(i) {
				return promql.Sample{}, false
			}
			// [promql.Sample] expects milliseconds as timestamp unit
			sample.T = int64(col.(*array.Timestamp).Value(i) / 1e6)
			continue
		}

		if ident.Equal(semconv.ColumnIdentValue) {
			// Ignore column values that are NULL or invalid
			if col.IsNull(i) || !col.IsValid(i) {
				return promql.Sample{}, false
			}
			col, ok := col.(*array.Float64)
			if !ok {
				return promql.Sample{}, false
			}
			sample.F = col.Value(i)
			continue
		}

		// allow any string columns
		if ident.DataType() == types.Loki.String {
			builder.Set(shortName, col.(*array.String).Value(i))
		}
	}

	sample.Metric = builder.Labels()
	return sample, true
}
