package querier

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util/topk"
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

// newEntryIterator creates a new EntryIterator for the given context, streams, and reader.
// It reads records from the reader and adds them to the topk heap based on the direction.
// The topk heap is used to maintain the top k entries based on the direction.
// The final result is returned as a slice of entries.
func newEntryIterator(ctx context.Context,
	streams map[int64]streams.Stream,
	reader *logs.RowReader,
	req logql.SelectLogParams,
) (iter.EntryIterator, error) {
	bufPtr := recordsPool.Get().(*[]logs.Record)
	defer recordsPool.Put(bufPtr)
	buf := *bufPtr

	selector, err := req.LogSelector()
	if err != nil {
		return nil, err
	}
	pipeline, err := selector.Pipeline()
	if err != nil {
		return nil, err
	}

	var (
		prevStreamID    int64 = -1
		streamExtractor log.StreamPipeline
		streamHash      uint64
		top             = topk.Heap[entryWithLabels]{
			Limit: int(req.Limit),
			Less:  lessFn(req.Direction),
		}

		statistics = stats.FromContext(ctx)
	)

	for {
		n, err := reader.Read(ctx, buf)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read log records: %w", err)
		}

		if n == 0 && err == io.EOF {
			break
		}

		for _, record := range buf[:n] {
			stream, ok := streams[record.StreamID]
			if !ok {
				continue
			}
			if prevStreamID != record.StreamID {
				streamExtractor = pipeline.ForStream(stream.Labels)
				streamHash = streamExtractor.BaseLabels().Hash()
				prevStreamID = record.StreamID
			}

			timestamp := record.Timestamp.UnixNano()
			line, parsedLabels, ok := streamExtractor.Process(timestamp, record.Line, record.Metadata)
			if !ok {
				continue
			}
			statistics.AddPostFilterRows(1)

			top.Push(entryWithLabels{
				Labels:     parsedLabels.String(),
				StreamHash: streamHash,
				Entry: logproto.Entry{
					Timestamp:          record.Timestamp,
					Line:               string(line),
					StructuredMetadata: logproto.FromLabelsToLabelAdapters(parsedLabels.StructuredMetadata()),
					Parsed:             logproto.FromLabelsToLabelAdapters(parsedLabels.Parsed()),
				},
			})
		}
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

func newSampleIterator(ctx context.Context,
	streamsMap map[int64]streams.Stream,
	extractors []syntax.SampleExtractor,
	reader *logs.RowReader,
) (iter.SampleIterator, error) {
	bufPtr := recordsPool.Get().(*[]logs.Record)
	defer recordsPool.Put(bufPtr)
	buf := *bufPtr

	var (
		iterators       []iter.SampleIterator
		prevStreamID    int64 = -1
		streamExtractor log.StreamSampleExtractor
		series          = map[string]*logproto.Series{}
		streamHash      uint64
	)

	statistics := stats.FromContext(ctx)
	// For dataobjs, this maps to sections downloaded
	statistics.AddChunksDownloaded(1)

	for {
		n, err := reader.Read(ctx, buf)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read log records: %w", err)
		}

		// Handle end of stream or empty read
		if n == 0 && err == io.EOF {
			iterators = appendIteratorFromSeries(iterators, series)
			break
		}

		// Process records in the current batch
		for _, record := range buf[:n] {
			stream, ok := streamsMap[record.StreamID]
			if !ok {
				continue
			}

			for _, extractor := range extractors {
				// Handle stream transition
				if prevStreamID != record.StreamID {
					iterators = appendIteratorFromSeries(iterators, series)
					clear(series)
					streamExtractor = extractor.ForStream(stream.Labels)
					streamHash = streamExtractor.BaseLabels().Hash()
					prevStreamID = record.StreamID
				}

				// Process the record
				timestamp := record.Timestamp.UnixNano()

				statistics.AddDecompressedLines(1)
				samples, ok := streamExtractor.Process(timestamp, record.Line, record.Metadata)
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
