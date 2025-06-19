package bench

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util/topk"
	"github.com/parquet-go/parquet-go"
	"github.com/prometheus/prometheus/model/labels"
)

var (
	recordsPool = sync.Pool{
		New: func() interface{} {
			records := make([]logs.Record, 1024)
			return &records
		},
	}
	samplesPool = sync.Pool{
		New: func() interface{} {
			samples := make([]logproto.Sample, 0, 1024)
			return &samples
		},
	}
	entryWithLabelsPool = sync.Pool{
		New: func() interface{} {
			entries := make([]entryWithLabels, 0, 1024)
			return &entries
		},
	}
)

type entryWithLabels struct {
	Labels     string
	StreamHash uint64
	Entry      logproto.Entry
}

func buildFilters(expr syntax.Expr) ([]ColumnFilter, []string) {
	filters := []ColumnFilter{}
	projectionColumns := []string{}

	expr.Walk(func(expr syntax.Expr) bool {
		switch expr := expr.(type) {
		case *syntax.PipelineExpr:
			for _, matcher := range expr.Matchers() {
				projectionColumns = append(projectionColumns, getColumnName(matcher.Name))
				filters = append(filters, CreateStringFilter(matcher.Name, matcher.Value))
			}
			for _, pipe := range expr.MultiStages {
				pipe.Walk(func(expr syntax.Expr) bool {
					switch expr := expr.(type) {
					case *syntax.LabelFilterExpr:
						stage, err := expr.Stage()
						if err != nil {
							panic(err)
						}
						switch stage := stage.(type) {
						case *log.LineFilterLabelFilter:
							projectionColumns = append(projectionColumns, getColumnName(stage.Name))
							if stage.Type == labels.MatchEqual {
								filters = append(filters, CreateStringFilter(stage.Name, stage.Value))
							} else if stage.Type == labels.MatchRegexp {
								filters = append(filters, CreateRegexFilter(stage.Name, stage.Value))
							}
						default:
							fmt.Printf("Unknown stage: %T\n", stage)
						}
					}
					return true
				})
			}
			return false
		case *syntax.MatchersExpr:
			for _, matcher := range expr.Matchers() {
				projectionColumns = append(projectionColumns, getColumnName(matcher.Name))
				filters = append(filters, CreateStringFilter(matcher.Name, matcher.Value))
			}
			return true
		case *syntax.VectorAggregationExpr:
			for _, group := range expr.Grouping.Groups {
				projectionColumns = append(projectionColumns, getColumnName(group))
			}
			return true
			/* 		case syntax.SampleExpr:
			return true */
		case *syntax.RangeAggregationExpr:
			return true
		case syntax.LogSelectorExpr:
			for _, matcher := range expr.Matchers() {
				projectionColumns = append(projectionColumns, getColumnName(matcher.Name))
				filters = append(filters, CreateStringFilter(matcher.Name, matcher.Value))
			}
			return true
		case *syntax.LogRangeExpr:
			return true
		}
		return false
	})

	return filters, projectionColumns
}

// newEntryIterator creates a new EntryIterator for the given context, streams, and reader.
// It reads records from the reader and adds them to the topk heap based on the direction.
// The topk heap is used to maintain the top k entries based on the direction.
// The final result is returned as a slice of entries.
func newEntryIterator(ctx context.Context,
	reader *parquet.File,
	req logql.SelectLogParams,
) (iter.EntryIterator, error) {
	selector, err := req.LogSelector()
	if err != nil {
		return nil, err
	}
	pipeline, err := selector.Pipeline()
	if err != nil {
		return nil, err
	}

	var (
		streamExtractor log.StreamPipeline
		streamHash      uint64
		top             = topk.Heap[entryWithLabels]{
			Limit: int(req.Limit),
			Less:  lessFn(req.Direction),
		}

		statistics = stats.FromContext(ctx)
	)

	parquetReader, err := NewEfficientParquetReader(ctx, reader)
	if err != nil {
		return nil, err
	}
	defer parquetReader.Close()

	filters := []ColumnFilter{
		CreateTimestampFilter(reader, req.Start.UnixNano(), req.End.UnixNano()),
	}
	logFilters, _ := buildFilters(req.Plan.AST)
	filters = append(filters, logFilters...)

	filterResult, err := parquetReader.FilterWithMultipleColumns(filters)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Found %d rows matching all filters using %s bytes\n", len(filterResult.Records), humanize.Bytes(uint64(parquetReader.ctx.Store().Chunk.DecompressedBytes)))
	if len(filterResult.Records) == 0 {
		return heapIterator(&top), nil
	}

	// Read only the needed columns for the valid rows
	columnNames := []string{"timestamp", "line"}
	for lb := range labelColumns {
		columnNames = append(columnNames, lb)
	}
	for md := range mdColumns {
		columnNames = append(columnNames, md)
	}
	records, err := parquetReader.ReadRemainingColumns(filterResult, columnNames)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Read %d complete records using %s bytes\n", len(records), humanize.Bytes(uint64(parquetReader.ctx.Store().Chunk.DecompressedBytes)))

	// Process the filtered records
	for _, record := range records {
		streamLbls := labels.FromMap(record.Labels)

		streamExtractor = pipeline.ForStream(streamLbls)
		streamHash = streamExtractor.BaseLabels().Hash()

		mdLbls := labels.FromMap(record.Metadata)
		line, parsedLabels, ok := streamExtractor.Process(record.Timestamp, []byte(record.Line), mdLbls)
		if !ok {
			continue
		}
		statistics.AddPostFilterRows(1)

		top.Push(entryWithLabels{
			Labels:     parsedLabels.String(),
			StreamHash: streamHash,
			Entry: logproto.Entry{
				Timestamp:          time.Unix(record.Timestamp, 0),
				Line:               string(line),
				StructuredMetadata: logproto.FromLabelsToLabelAdapters(parsedLabels.StructuredMetadata()),
				Parsed:             logproto.FromLabelsToLabelAdapters(parsedLabels.Parsed()),
			},
		})
	}

	return heapIterator(&top), nil
}

func lessFn(direction logproto.Direction) func(a, b entryWithLabels) bool {
	switch direction {
	case logproto.FORWARD:
		return func(a, b entryWithLabels) bool {
			if a.Entry.Timestamp.Equal(b.Entry.Timestamp) {
				return a.Labels < b.Labels
			}
			return a.Entry.Timestamp.After(b.Entry.Timestamp)
		}
	case logproto.BACKWARD:
		return func(a, b entryWithLabels) bool {
			if a.Entry.Timestamp.Equal(b.Entry.Timestamp) {
				return a.Labels < b.Labels
			}
			return a.Entry.Timestamp.Before(b.Entry.Timestamp)
		}
	default:
		panic("invalid direction")
	}
}

// heapIterator creates a new EntryIterator for the given topk heap. After
// calling heapIterator, h is emptied.
func heapIterator(h *topk.Heap[entryWithLabels]) iter.EntryIterator {
	elems := h.PopAll()

	// We need to reverse the order of the entries in the slice to maintain the order of logs we
	// want to return:
	//
	// For FORWARD direction, we want smallest timestamps first (but the heap is
	// ordered by largest timestamps first due to lessFn).
	//
	// For BACKWARD direction, we want largest timestamps first (but the heap is
	// ordered by smallest timestamps first due to lessFn).
	slices.Reverse(elems)

	return &sliceIterator{entries: elems}
}

type sliceIterator struct {
	entries []entryWithLabels
	curr    entryWithLabels
}

func (s *sliceIterator) Next() bool {
	if len(s.entries) == 0 {
		return false
	}
	s.curr = s.entries[0]
	s.entries = s.entries[1:]
	return true
}

func (s *sliceIterator) At() logproto.Entry {
	return s.curr.Entry
}

func (s *sliceIterator) Err() error {
	return nil
}

func (s *sliceIterator) Labels() string {
	return s.curr.Labels
}

func (s *sliceIterator) StreamHash() uint64 {
	return s.curr.StreamHash
}

func (s *sliceIterator) Close() error {
	clear(s.entries)
	entryWithLabelsPool.Put(&s.entries)
	return nil
}

func getColumnName(fieldName string) string {
	if _, ok := labelColumns[fieldName]; ok {
		return fmt.Sprintf("label$%s", fieldName)
	} else if _, ok := mdColumns[fieldName]; ok {
		return fmt.Sprintf("md$%s", fieldName)
	}
	return ""
}

func newSampleIterator(ctx context.Context,
	expr syntax.SampleExpr,
	reader *parquet.File,
	start, end time.Time,
) (iter.SampleIterator, error) {

	var (
		iterators       []iter.SampleIterator
		streamExtractor log.StreamSampleExtractor
		series          = map[string]*logproto.Series{}
		streamHash      uint64
	)

	statistics := stats.FromContext(ctx)
	// For dataobjs, this maps to sections downloaded
	statistics.AddChunksDownloaded(1)

	parquetReader, err := NewEfficientParquetReader(ctx, reader)
	if err != nil {
		return nil, err
	}
	defer parquetReader.Close()

	filters := []ColumnFilter{
		CreateTimestampFilter(reader, start.UnixNano(), end.UnixNano()),
	}
	logFilters, projectionColumns := buildFilters(expr)
	filters = append(filters, logFilters...)

	filterResult, err := parquetReader.FilterWithMultipleColumns(filters)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Found %d rows matching all %d filters using %s bytes (cumulative)\n", len(filterResult.Records), len(filters), humanize.Bytes(uint64(parquetReader.ctx.Store().Chunk.DecompressedBytes)))
	if len(filterResult.Records) == 0 {
		return iter.NoopSampleIterator, nil
	}

	// Read only the needed columns for the valid rows
	columnNames := []string{"timestamp"}
	if len(projectionColumns) > 0 {
		columnNames = append(columnNames, projectionColumns...)
	} else {
		columnNames = append(columnNames, "line")
		for lb := range labelColumns {
			columnNames = append(columnNames, getColumnName(lb))
		}
		for md := range mdColumns {
			columnNames = append(columnNames, getColumnName(md))
		}
	}

	records, err := parquetReader.ReadRemainingColumns(filterResult, columnNames)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Read %d complete (%d columns) records using %s bytes\n", len(records), len(columnNames), humanize.Bytes(uint64(parquetReader.ctx.Store().Chunk.DecompressedBytes)))

	extractors, err := expr.Extractors()
	if err != nil {
		return nil, err
	}

	// Process the filtered records
	for _, record := range records {
		streamLabels := labels.FromMap(record.Labels)

		for _, extractor := range extractors {
			// Handle stream transition
			iterators = appendIteratorFromSeries(iterators, series)
			clear(series)
			streamExtractor = extractor.ForStream(streamLabels)
			streamHash = streamExtractor.BaseLabels().Hash()

			// Process the record
			timestamp := record.Timestamp

			statistics.AddDecompressedLines(1)
			mdLbls := labels.FromMap(record.Metadata)
			samples, ok := streamExtractor.Process(timestamp, []byte(record.Line), mdLbls)
			if !ok {
				continue
			}
			statistics.AddPostFilterLines(1)

			for _, sample := range samples {

				parsedLabels := sample.Labels
				value := sample.Value

				// Get or create series for the parsed labels
				labelString := parsedLabels.String()
				s, exists := series[labelString]
				if !exists {
					s = createNewSeries(labelString, streamHash)
					series[labelString] = s
				}

				// Add sample to the series
				s.Samples = append(s.Samples, logproto.Sample{
					Timestamp: timestamp,
					Value:     value,
					Hash:      0,
				})
			}
		}
	}

	if len(iterators) == 0 {
		return iter.NoopSampleIterator, nil
	}

	return iter.NewSortSampleIterator(iterators), nil
}

// createNewSeries creates a new Series for the given labels and stream hash
func createNewSeries(labels string, streamHash uint64) *logproto.Series {
	samplesPtr := samplesPool.Get().(*[]logproto.Sample)
	samples := *samplesPtr
	return &logproto.Series{
		Labels:     labels,
		Samples:    samples[:0],
		StreamHash: streamHash,
	}
}

// appendIteratorFromSeries appends a new SampleIterator to the given list of iterators
func appendIteratorFromSeries(iterators []iter.SampleIterator, series map[string]*logproto.Series) []iter.SampleIterator {
	if len(series) == 0 {
		return iterators
	}

	seriesResult := make([]logproto.Series, 0, len(series))
	for _, s := range series {
		seriesResult = append(seriesResult, *s)
	}

	return append(iterators, iter.SampleIteratorWithClose(
		iter.NewMultiSeriesIterator(seriesResult),
		func() error {
			for _, s := range seriesResult {
				samplesPool.Put(&s.Samples)
			}
			return nil
		},
	))
}
