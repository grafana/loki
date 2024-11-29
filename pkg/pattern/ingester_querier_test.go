package pattern

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestPrunePatterns(t *testing.T) {
	metrics := newIngesterQuerierMetrics(prometheus.NewRegistry(), "test")
	testCases := []struct {
		name             string
		inputSeries      []*logproto.PatternSeries
		minClusterSize   int64
		expectedSeries   []*logproto.PatternSeries
		expectedPruned   int
		expectedRetained int
	}{
		{
			name: "No pruning needed",
			inputSeries: []*logproto.PatternSeries{
				{Pattern: `{app="test1"}`, Samples: []*logproto.PatternSample{{Value: 40}}},
				{Pattern: `{app="test2"}`, Samples: []*logproto.PatternSample{{Value: 35}}},
			},
			minClusterSize: 20,
			expectedSeries: []*logproto.PatternSeries{
				{Pattern: `{app="test1"}`, Samples: []*logproto.PatternSample{{Value: 40}}},
				{Pattern: `{app="test2"}`, Samples: []*logproto.PatternSample{{Value: 35}}},
			},
			expectedPruned:   0,
			expectedRetained: 2,
		},
		{
			name: "Pruning some patterns",
			inputSeries: []*logproto.PatternSeries{
				{Pattern: `{app="test1"}`, Samples: []*logproto.PatternSample{{Value: 10}}},
				{Pattern: `{app="test2"}`, Samples: []*logproto.PatternSample{{Value: 5}}},
				{Pattern: `{app="test3"}`, Samples: []*logproto.PatternSample{{Value: 50}}},
			},
			minClusterSize: 20,
			expectedSeries: []*logproto.PatternSeries{
				{Pattern: `{app="test3"}`, Samples: []*logproto.PatternSample{{Value: 50}}},
			},
			expectedPruned:   2,
			expectedRetained: 1,
		},
		{
			name: "Limit patterns to maxPatterns",
			inputSeries: func() []*logproto.PatternSeries {
				series := make([]*logproto.PatternSeries, maxPatterns+10)
				for i := 0; i < maxPatterns+10; i++ {
					series[i] = &logproto.PatternSeries{
						Pattern: `{app="test"}`,
						Samples: []*logproto.PatternSample{{Value: int64(maxPatterns + 10 - i)}},
					}
				}
				return series
			}(),
			minClusterSize: 0,
			expectedSeries: func() []*logproto.PatternSeries {
				series := make([]*logproto.PatternSeries, maxPatterns)
				for i := 0; i < maxPatterns; i++ {
					series[i] = &logproto.PatternSeries{
						Pattern: `{app="test"}`,
						Samples: []*logproto.PatternSample{{Value: int64(maxPatterns + 10 - i)}},
					}
				}
				return series
			}(),
			expectedPruned:   10,
			expectedRetained: maxPatterns,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &logproto.QueryPatternsResponse{
				Series: tc.inputSeries,
			}
			result := prunePatterns(resp, tc.minClusterSize, metrics)

			require.Equal(t, len(tc.expectedSeries), len(result.Series))
			require.Equal(t, tc.expectedSeries, result.Series)
		})
	}
}
