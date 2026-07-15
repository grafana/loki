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
			Series: []logproto.PatternSeries{},
		}
	}

	if len(responses) == 1 {
		return responses[0]
	}

	// Merge patterns by pattern string; index into result.Series for in-place update.
	patternIdx := make(map[string]int)

	result := &logproto.QueryPatternsResponse{
		Series: make([]logproto.PatternSeries, 0),
	}

	for _, resp := range responses {
		if resp == nil {
			continue
		}

		for _, series := range resp.Series {
			idx, exists := patternIdx[series.Pattern]
			if !exists {
				patternIdx[series.Pattern] = len(result.Series)
				result.Series = append(result.Series, series)
				continue
			}

			// Merge samples into existing entry.
			result.Series[idx].Samples = append(result.Series[idx].Samples, series.Samples...)
		}
	}

	// Sort samples within each series by timestamp.
	for i := range result.Series {
		sort.Slice(result.Series[i].Samples, func(a, b int) bool {
			return result.Series[i].Samples[a].Timestamp < result.Series[i].Samples[b].Timestamp
		})
	}

	return result
}
