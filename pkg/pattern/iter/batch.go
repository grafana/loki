package iter

import (
	"fmt"
	"math"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func ReadBatch(it Iterator, batchSize int) (*logproto.QueryPatternsResponse, error) {
	var (
		series   = map[string][]*logproto.PatternSample{}
		respSize int
	)

	for ; respSize < batchSize && it.Next(); respSize++ {
		pattern := it.Pattern()
		sample := it.At()
		series[pattern] = append(series[pattern], &sample)
	}
	result := logproto.QueryPatternsResponse{
		Series: make([]*logproto.PatternSeries, 0, len(series)),
	}
	for pattern, samples := range series {
		result.Series = append(result.Series, &logproto.PatternSeries{
			Pattern: pattern,
			Samples: samples,
		})
	}
	return &result, it.Error()
}

func ReadAll(it Iterator) (*logproto.QueryPatternsResponse, error) {
	return ReadBatch(it, math.MaxInt32)
}

func ReadMetricsBatch(it iter.SampleIterator, batchSize int, logger log.Logger) (*logproto.QuerySamplesResponse, error) {
	var (
		series   = map[uint64]*logproto.Series{}
		respSize int
	)

	for ; respSize < batchSize && it.Next(); respSize++ {
		hash := it.StreamHash()
		s, ok := series[hash]
		if !ok {
			s = &logproto.Series{
				Labels:     it.Labels(),
				Samples:    []logproto.Sample{},
				StreamHash: hash,
			}
			series[hash] = s
		}

		s.Samples = append(s.Samples, it.Sample())
	}

	result := logproto.QuerySamplesResponse{
		Series: make([]logproto.Series, 0, len(series)),
	}
	for _, s := range series {
		level.Debug(logger).Log("msg", "appending series", "s", fmt.Sprintf("%v", s))
		result.Series = append(result.Series, *s)
	}
	return &result, it.Error()
}

// ReadAllSamples reads all samples from the given iterator. It is only used in tests.
func ReadAllSamples(it iter.SampleIterator) (*logproto.QuerySamplesResponse, error) {
	return ReadMetricsBatch(it, math.MaxInt32, log.NewNopLogger())
}
