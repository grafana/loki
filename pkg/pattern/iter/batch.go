package iter

import (
	"math"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func ReadMetricsBatch(it Iterator, batchSize int) (*logproto.QueryPatternsResponse, error) {
	var (
		series   = map[string][]*logproto.PatternSample{}
		respSize int
	)

	for ; respSize < batchSize && it.Next(); respSize++ {
		labels := it.Labels()
		sample := it.At()
		series[labels.String()] = append(series[labels.String()], &sample)
	}
	result := logproto.QueryPatternsResponse{
		Series: make([]*logproto.PatternSeries, 0, len(series)),
	}
	for id, samples := range series {
		result.Series = append(
			result.Series,
			logproto.NewPatternSeriesWithLabels(id, samples),
		)
	}
	return &result, it.Error()
}

func ReadPatternsBatch(
	it Iterator,
	batchSize int,
) (*logproto.QueryPatternsResponse, error) {
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
	for id, samples := range series {
		result.Series = append(
			result.Series,
			logproto.NewPatternSeriesWithPattern(id, samples),
		)
	}
	return &result, it.Error()
}

func ReadAllWithPatterns(it Iterator) (*logproto.QueryPatternsResponse, error) {
	return ReadPatternsBatch(it, math.MaxInt32)
}

func ReadAllWithLabels(it Iterator) (*logproto.QueryPatternsResponse, error) {
	return ReadMetricsBatch(it, math.MaxInt32)
}
