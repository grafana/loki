package iter

import (
	"math"

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

func ReadMetricsBatch(it iter.SampleIterator, batchSize int) (*logproto.QuerySamplesResponse, error) {
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
		result.Series = append(result.Series, *s)
	}
	return &result, it.Error()
}

func ReadAllSamples(it iter.SampleIterator) (*logproto.QuerySamplesResponse, error) {
	return ReadMetricsBatch(it, math.MaxInt32)
}
