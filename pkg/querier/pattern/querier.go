package pattern

import (
	"context"
	"sort"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type PatterQuerier interface {
	Patterns(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error)
}

func MergePatternResponses(responses []*logproto.QueryPatternsResponse) *logproto.QueryPatternsResponse {
	if len(responses) == 0 {
		return &logproto.QueryPatternsResponse{
			Series: []*logproto.PatternSeries{},
		}
	}

	if len(responses) == 1 {
		return responses[0]
	}

	// Merge patterns by pattern string
	patternMap := make(map[string]*logproto.PatternSeries)

	for _, resp := range responses {
		if resp == nil {
			continue
		}

		for _, series := range resp.Series {
			existing, exists := patternMap[series.Pattern]
			if !exists {
				patternMap[series.Pattern] = series
				continue
			}

			// Merge samples
			existing.Samples = append(existing.Samples, series.Samples...)
		}
	}

	// Sort samples within each series by timestamp
	result := &logproto.QueryPatternsResponse{
		Series: make([]*logproto.PatternSeries, 0, len(patternMap)),
	}

	for _, series := range patternMap {
		// Sort samples by timestamp
		sort.Slice(series.Samples, func(i, j int) bool {
			return series.Samples[i].Timestamp < series.Samples[j].Timestamp
		})
		result.Series = append(result.Series, series)
	}

	return result
}
