package iter

import (
	"math"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func ReadBatch(it Iterator, batchSize int) (*logproto.QueryPatternsResponse, error) {
	var (
		series   = map[string]map[string][]*logproto.PatternSample{}
		respSize int
	)

	for ; respSize < batchSize && it.Next(); respSize++ {
		pattern := it.Pattern()
		lvl := it.Level()
		sample := it.At()

		if _, ok := series[lvl]; !ok {
			series[lvl] = map[string][]*logproto.PatternSample{}
		}
		series[lvl][pattern] = append(series[lvl][pattern], &sample)
	}
	result := logproto.QueryPatternsResponse{
		Series: make([]*logproto.PatternSeries, 0, len(series)),
	}
	for lvl, patterns := range series {
		for pattern, samples := range patterns {
			result.Series = append(result.Series, &logproto.PatternSeries{
				Pattern: pattern,
				Level:   lvl,
				Samples: samples,
			})
		}
	}
	return &result, it.Err()
}

func ReadAll(it Iterator) (*logproto.QueryPatternsResponse, error) {
	return ReadBatch(it, math.MaxInt32)
}
