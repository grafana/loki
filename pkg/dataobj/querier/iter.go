package querier

import (
	"context"
	"io"
	"sync"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

var (
	recordsPool = sync.Pool{
		New: func() interface{} {
			records := make([]dataobj.Record, 1024)
			return &records
		},
	}
	samplesPool = sync.Pool{
		New: func() interface{} {
			samples := make([]logproto.Sample, 0, 1024)
			return &samples
		},
	}
)

func newSampleIterator(ctx context.Context,
	streams map[int64]dataobj.Stream,
	extractor syntax.SampleExtractor,
	reader *dataobj.LogsReader,
) (iter.SampleIterator, error) {
	bufPtr := recordsPool.Get().(*[]dataobj.Record)
	defer recordsPool.Put(bufPtr)
	buf := *bufPtr

	var (
		iterators       []iter.SampleIterator
		prevStreamID    int64 = -1
		streamExtractor log.StreamSampleExtractor
		series          = map[string]*logproto.Series{}
		streamHash      uint64
	)

	for {
		n, err := reader.Read(ctx, buf)
		if err != nil && err != io.EOF {
			return nil, err
		}

		// Handle end of stream or empty read
		if n == 0 {
			iterators = appendIteratorFromSeries(iterators, series)
			break
		}

		// Process records in the current batch
		for _, record := range buf[:n] {
			stream, ok := streams[record.StreamID]
			if !ok {
				continue
			}

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
			value, parsedLabels, ok := streamExtractor.ProcessString(timestamp, record.Line, record.Metadata...)
			if !ok {
				continue
			}

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
				Hash:      0, // todo write a test to verify that we should not try to dedupe when we don't have a hash
			})
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
