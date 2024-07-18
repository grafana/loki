package iter

import (
	"sort"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	loki_iter "github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestReadBatch(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		samples   []logproto.PatternSample
		batchSize int
		expected  *logproto.QueryPatternsResponse
	}{
		{
			name:      "ReadBatch empty iterator",
			pattern:   "foo",
			samples:   []logproto.PatternSample{},
			batchSize: 2,
			expected: &logproto.QueryPatternsResponse{
				Series: []*logproto.PatternSeries{},
			},
		},
		{
			name:      "ReadBatch less than batchSize",
			pattern:   "foo",
			samples:   []logproto.PatternSample{{Timestamp: 10, Value: 2}, {Timestamp: 20, Value: 4}, {Timestamp: 30, Value: 6}},
			batchSize: 2,
			expected: &logproto.QueryPatternsResponse{
				Series: []*logproto.PatternSeries{
					logproto.NewPatternSeries(
						"foo",
						[]*logproto.PatternSample{
							{Timestamp: 10, Value: 2},
							{Timestamp: 20, Value: 4},
						},
					),
				},
			},
		},
		{
			name:    "ReadBatch more than batchSize",
			pattern: "foo",
			samples: []logproto.PatternSample{
				{Timestamp: 10, Value: 2},
				{Timestamp: 20, Value: 4},
				{Timestamp: 30, Value: 6},
			},
			batchSize: 4,
			expected: &logproto.QueryPatternsResponse{
				Series: []*logproto.PatternSeries{
					logproto.NewPatternSeries(
						"foo",
						[]*logproto.PatternSample{
							{Timestamp: 10, Value: 2},
							{Timestamp: 20, Value: 4},
							{Timestamp: 30, Value: 6},
						},
					),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it := NewSlice(tt.pattern, tt.samples)
			got, err := ReadBatch(it, tt.batchSize)
			require.NoError(t, err)
			require.Equal(t, tt.expected, got)
		})
	}
}

func singleSeriesIterator(series logproto.Series) []loki_iter.SampleIterator {
	return []loki_iter.SampleIterator{
		loki_iter.NewSeriesIterator(series),
	}
}

func TestReadMetricsBatch(t *testing.T) {
	tests := []struct {
		name       string
		pattern    string
		seriesIter []loki_iter.SampleIterator
		expected   *logproto.QuerySamplesResponse
	}{
		{
			name: "ReadMetrics empty iterator",
			seriesIter: singleSeriesIterator(logproto.Series{
				Labels:  "",
				Samples: []logproto.Sample{},
			}),
			expected: &logproto.QuerySamplesResponse{
				Series: []logproto.Series{},
			},
		},
		{
			name: "ReadMetrics reads all",
			seriesIter: singleSeriesIterator(logproto.Series{
				Labels: `{foo="bar"}`,
				Samples: []logproto.Sample{
					{Timestamp: 10, Value: 2},
					{Timestamp: 20, Value: 4},
					{Timestamp: 30, Value: 6},
				},
			}),
			expected: &logproto.QuerySamplesResponse{
				Series: []logproto.Series{
					{
						Labels: `{foo="bar"}`,
						Samples: []logproto.Sample{
							{Timestamp: 10, Value: 2},
							{Timestamp: 20, Value: 4},
							{Timestamp: 30, Value: 6},
						},
					},
				},
			},
		},
		{
			name: "ReadMetrics multiple series",
			seriesIter: []loki_iter.SampleIterator{
				loki_iter.NewSeriesIterator(logproto.Series{
					Labels: `{foo="bar"}`,
					Samples: []logproto.Sample{
						{Timestamp: 10, Value: 2},
						{Timestamp: 20, Value: 4},
						{Timestamp: 30, Value: 6},
					},
					StreamHash: labels.StableHash([]labels.Label{{Name: "foo", Value: "bar"}}),
				}),
				loki_iter.NewSeriesIterator(logproto.Series{
					Labels: `{fizz="buzz"}`,
					Samples: []logproto.Sample{
						{Timestamp: 10, Value: 3},
						{Timestamp: 20, Value: 5},
						{Timestamp: 30, Value: 7},
					},
					StreamHash: labels.StableHash([]labels.Label{{Name: "fizz", Value: "buzz"}}),
				}),
				loki_iter.NewSeriesIterator(logproto.Series{
					Labels: `{foo="bar"}`,
					Samples: []logproto.Sample{
						{Timestamp: 20, Value: 2},
					},
					StreamHash: labels.StableHash([]labels.Label{{Name: "foo", Value: "bar"}}),
				}),
			},
			expected: &logproto.QuerySamplesResponse{
				Series: []logproto.Series{
					{
						Labels: `{foo="bar"}`,
						Samples: []logproto.Sample{
							{Timestamp: 10, Value: 2},
							{Timestamp: 20, Value: 6}, // from the second series
							{Timestamp: 30, Value: 6},
						},
						StreamHash: labels.StableHash([]labels.Label{{Name: "foo", Value: "bar"}}),
					},
					{
						Labels: `{fizz="buzz"}`,
						Samples: []logproto.Sample{
							{Timestamp: 10, Value: 3},
							{Timestamp: 20, Value: 5}, // from the second series
							{Timestamp: 30, Value: 7},
						},
						StreamHash: labels.StableHash([]labels.Label{{Name: "fizz", Value: "buzz"}}),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it := NewSumMergeSampleIterator(tt.seriesIter)
			got, err := ReadMetrics(it, log.NewNopLogger())
			require.NoError(t, err)
			sort.Slice(tt.expected.Series, func(i, j int) bool {
				return tt.expected.Series[i].Labels < tt.expected.Series[j].Labels
			})
			sort.Slice(got.Series, func(i, j int) bool {
				return got.Series[i].Labels < got.Series[j].Labels
			})
			require.Equal(t, tt.expected.Series, got.Series)
		})
	}
}
