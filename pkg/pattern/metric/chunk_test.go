package metric

import (
	"context"
	"reflect"
	"testing"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestForRangeAndType(t *testing.T) {
	testCases := []struct {
		name       string
		c          *Chunk
		metricType MetricType
		start      model.Time
		end        model.Time
		expected   []logproto.PatternSample
	}{
		{
			name:       "Empty count",
			c:          &Chunk{},
			metricType: Count,
			start:      1,
			end:        10,
			expected:   nil,
		},
		{
			name:       "Empty bytes",
			c:          &Chunk{},
			metricType: Bytes,
			start:      1,
			end:        10,
			expected:   nil,
		},
		{
			name: "No Overlap -- count",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      10,
			end:        20,
			expected:   nil,
		},
		{
			name: "No Overlap -- bytes",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      10,
			end:        20,
			expected:   nil,
		},
		{
			name: "Complete Overlap -- count",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      0,
			end:        10,
			expected: []logproto.PatternSample{
				{Timestamp: 2, Value: 2},
				{Timestamp: 4, Value: 4},
				{Timestamp: 6, Value: 6},
			},
		},
		{
			name: "Complete Overlap -- bytes",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      0,
			end:        10,
			expected: []logproto.PatternSample{
				{Timestamp: 2, Value: 2},
				{Timestamp: 4, Value: 4},
				{Timestamp: 6, Value: 6},
			},
		},
		{
			name: "Partial Overlap -- count",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      3,
			end:        5,
			expected:   []logproto.PatternSample{{Timestamp: 4, Value: 4}},
		},
		{
			name: "Partial Overlap -- bytes",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      3,
			end:        5,
			expected:   []logproto.PatternSample{{Timestamp: 4, Value: 4}},
		},
		{
			name: "Single Element in Range -- count",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      4,
			end:        5,
			expected:   []logproto.PatternSample{{Timestamp: 4, Value: 4}},
		},
		{
			name: "Single Element in Range -- bytes",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      4,
			end:        5,
			expected:   []logproto.PatternSample{{Timestamp: 4, Value: 4}},
		},
		{
			name: "Start Before First Element -- count",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      0,
			end:        5,
			expected: []logproto.PatternSample{
				{Timestamp: 2, Value: 2},
				{Timestamp: 4, Value: 4},
			},
		},
		{
			name: "Start Before First Element -- bytes",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      0,
			end:        5,
			expected: []logproto.PatternSample{
				{Timestamp: 2, Value: 2},
				{Timestamp: 4, Value: 4},
			},
		},
		{
			name: "End After Last Element -- count",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      5,
			end:        10,
			expected: []logproto.PatternSample{
				{Timestamp: 6, Value: 6},
			},
		},
		{
			name: "End After Last Element -- bytes",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      5,
			end:        10,
			expected: []logproto.PatternSample{
				{Timestamp: 6, Value: 6},
			},
		},
		{
			name: "Start before First and End Inclusive of First Element -- count",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      0,
			end:        2,
			expected:   []logproto.PatternSample{{Timestamp: 2, Value: 2}},
		},
		{
			name: "Start before First and End Inclusive of First Element -- bytes",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      0,
			end:        2,
			expected:   []logproto.PatternSample{{Timestamp: 2, Value: 2}},
		},
		{
			name: "Start and End before First Element -- count",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      0,
			end:        1,
			expected:   nil,
		},
		{
			name: "Start and End before First Element -- bytes",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      0,
			end:        1,
			expected:   nil,
		},
		{
			name: "Higher resolution samples down-sampled to preceding step bucket -- count",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 1, Count: 2},
				{Timestamp: 2, Count: 4},
				{Timestamp: 3, Count: 6},
				{Timestamp: 4, Count: 8},
				{Timestamp: 5, Count: 10},
				{Timestamp: 6, Count: 12},
			}},
			metricType: Count,
			start:      1,
			end:        6,
			expected: []logproto.PatternSample{
				{Timestamp: 0, Value: 2},
				{Timestamp: 2, Value: 10},
				{Timestamp: 4, Value: 18},
				{Timestamp: 6, Value: 12},
			},
		},
		{
			name: "Higher resolution samples down-sampled to preceding step bucket -- bytes",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 1, Bytes: 2},
				{Timestamp: 2, Bytes: 4},
				{Timestamp: 3, Bytes: 6},
				{Timestamp: 4, Bytes: 8},
				{Timestamp: 5, Bytes: 10},
				{Timestamp: 6, Bytes: 12},
			}},
			metricType: Bytes,
			start:      1,
			end:        6,
			expected: []logproto.PatternSample{
				{Timestamp: 0, Value: 2},
				{Timestamp: 2, Value: 10},
				{Timestamp: 4, Value: 18},
				{Timestamp: 6, Value: 12},
			},
		},
		{
			name: "Low resolution samples insert 0 values for empty steps -- count",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 1, Count: 2},
				{Timestamp: 5, Count: 10},
			}},
			metricType: Count,
			start:      1,
			end:        6,
			expected: []logproto.PatternSample{
				{Timestamp: 0, Value: 2},
				{Timestamp: 2, Value: 0},
				{Timestamp: 4, Value: 10},
			},
		},
		{
			name: "Low resolution samples insert 0 values for empty steps -- bytes",
			c: &Chunk{Samples: MetricSamples{
				{Timestamp: 1, Bytes: 2},
				{Timestamp: 5, Bytes: 10},
			}},
			metricType: Bytes,
			start:      1,
			end:        6,
			expected: []logproto.PatternSample{
				{Timestamp: 0, Value: 2},
				{Timestamp: 2, Value: 0},
				{Timestamp: 4, Value: 10},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.c.ForRangeAndType(tc.metricType, tc.start, tc.end, model.Time(2))
			require.NoError(t, err)
			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
			require.Equal(t, len(result), cap(result), "Returned slice wasn't created at the correct capacity")
		})
	}
}

func Test_Chunks_Iterator(t *testing.T) {
	ctx := context.Background()
	lbls := labels.Labels{
		labels.Label{Name: "foo", Value: "bar"},
		labels.Label{Name: "container", Value: "jar"},
	}
	chunks := Chunks{
		chunks: []Chunk{
			{
				Samples: []MetricSample{
					{Timestamp: 2, Bytes: 2, Count: 1},
					{Timestamp: 4, Bytes: 4, Count: 3},
					{Timestamp: 6, Bytes: 6, Count: 5},
				},
				mint: 2,
				maxt: 6,
			},
		},
		labels: lbls,
	}

	t.Run("without grouping", func(t *testing.T) {
		it, err := chunks.Iterator(ctx, Bytes, nil, 0, 10, 2)
		require.NoError(t, err)

		res, err := iter.ReadAllWithLabels(it)
		require.NoError(t, err)

		require.Equal(t, 1, len(res.Series))
		require.Equal(t, lbls.String(), res.Series[0].GetLabels())

		it, err = chunks.Iterator(ctx, Count, nil, 0, 10, 2)
		require.NoError(t, err)

		res, err = iter.ReadAllWithLabels(it)
		require.NoError(t, err)

		require.Equal(t, 1, len(res.Series))
		require.Equal(t, lbls.String(), res.Series[0].GetLabels())
	})

	t.Run("grouping", func(t *testing.T) {
		grouping := &syntax.Grouping{
			Groups:  []string{"container"},
			Without: false,
		}

		expectedLabels := labels.Labels{
			labels.Label{
				Name:  "container",
				Value: "jar",
			},
		}

		it, err := chunks.Iterator(ctx, Bytes, grouping, 0, 10, 2)
		require.NoError(t, err)

		res, err := iter.ReadAllWithLabels(it)
		require.NoError(t, err)

		require.Equal(t, 1, len(res.Series))
		require.Equal(t, expectedLabels.String(), res.Series[0].GetLabels())

		it, err = chunks.Iterator(ctx, Count, grouping, 0, 10, 2)
		require.NoError(t, err)

		res, err = iter.ReadAllWithLabels(it)
		require.NoError(t, err)

		require.Equal(t, 1, len(res.Series))
		require.Equal(t, expectedLabels.String(), res.Series[0].GetLabels())
	})

	t.Run("grouping by a missing label", func(t *testing.T) {
		grouping := &syntax.Grouping{
			Groups:  []string{"missing"},
			Without: false,
		}

		expectedLabels := labels.Labels{
			labels.Label{
				Name:  "missing",
				Value: "",
			},
		}

		it, err := chunks.Iterator(ctx, Bytes, grouping, 0, 10, 2)
		require.NoError(t, err)

		res, err := iter.ReadAllWithLabels(it)
		require.NoError(t, err)

		require.Equal(t, 1, len(res.Series))
		require.Equal(t, expectedLabels.String(), res.Series[0].GetLabels())

		it, err = chunks.Iterator(ctx, Count, grouping, 0, 10, 2)
		require.NoError(t, err)

		res, err = iter.ReadAllWithLabels(it)
		require.NoError(t, err)

		require.Equal(t, 1, len(res.Series))
		require.Equal(t, expectedLabels.String(), res.Series[0].GetLabels())
	})
}
