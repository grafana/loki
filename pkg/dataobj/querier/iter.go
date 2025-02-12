package querier

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

var (
	recordsPool = sync.Pool{
		New: func() interface{} {
			records := make([]dataobj.Record, 0, 1024)
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
	start, end time.Time, // todo: use as predicate.
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
		if n == 0 {
			break
		}
		for _, record := range buf[:n] {
			stream, ok := streams[record.StreamID]
			if !ok {
				continue
			}
			// if stream is different from the previous stream, we need to create a new iterator
			// since records are sorted by streams first then by timestamp
			if prevStreamID != record.StreamID {
				if len(series) > 0 {
					// build the iterator and append it.
					seriesRes := make([]logproto.Series, 0, len(series))
					for _, s := range series {
						seriesRes = append(seriesRes, *s)
					}
					iterators = append(iterators, iter.SampleIteratorWithClose(iter.NewMultiSeriesIterator(seriesRes), func() error {
						for _, s := range series {
							samplesPool.Put(&s.Samples)
						}
						return nil
					}))
				}
				clear(series)
				streamExtractor = extractor.ForStream(stream.Labels)
				streamHash = streamExtractor.BaseLabels().Hash()
				prevStreamID = record.StreamID
			}
			ts := record.Timestamp.UnixNano()
			value, parsedLabels, ok := streamExtractor.ProcessString(ts, record.Line, record.Metadata...)
			if !ok {
				continue
			}
			var (
				found bool
				s     *logproto.Series
			)
			lbs := parsedLabels.String()
			if s, found = series[lbs]; !found {
				samplesPtr := samplesPool.Get().(*[]logproto.Sample)
				samples := *samplesPtr
				s = &logproto.Series{
					Labels:     lbs,
					Samples:    samples[:0],
					StreamHash: streamHash,
				}
				series[lbs] = s
			}
			s.Samples = append(s.Samples, logproto.Sample{
				Timestamp: ts,
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
