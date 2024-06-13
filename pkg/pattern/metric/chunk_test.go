package metric

import (
	"reflect"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
)

func TestForTypeAndRange(t *testing.T) {
	testCases := []struct {
		name       string
		c          *Chunk
		metricType Type
		start      model.Time
		end        model.Time
		expected   []logproto.Sample
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
			c: &Chunk{Samples: Samples{
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
			c: &Chunk{Samples: Samples{
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
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      0,
			end:        10,
			expected: []logproto.Sample{
				{Timestamp: 2 * 1e6, Value: 2},
				{Timestamp: 4 * 1e6, Value: 4},
				{Timestamp: 6 * 1e6, Value: 6},
			},
		},
		{
			name: "Complete Overlap -- bytes",
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      0,
			end:        10,
			expected: []logproto.Sample{
				{Timestamp: 2 * 1e6, Value: 2},
				{Timestamp: 4 * 1e6, Value: 4},
				{Timestamp: 6 * 1e6, Value: 6},
			},
		},
		{
			name: "Partial Overlap -- count",
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      2,
			end:        5,
			expected: []logproto.Sample{
				{Timestamp: 2 * 1e6, Value: 2},
				{Timestamp: 4 * 1e6, Value: 4},
			},
		},
		{
			name: "Partial Overlap -- bytes",
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      2,
			end:        5,
			expected: []logproto.Sample{
				{Timestamp: 2 * 1e6, Value: 2},
				{Timestamp: 4 * 1e6, Value: 4},
			},
		},
		{
			name: "Single Element in Range -- count",
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      4,
			end:        5,
			expected:   []logproto.Sample{{Timestamp: 4 * 1e6, Value: 4}},
		},
		{
			name: "Single Element in Range -- bytes",
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      4,
			end:        5,
			expected:   []logproto.Sample{{Timestamp: 4 * 1e6, Value: 4}},
		},
		{
			name: "Start Before First Element -- count",
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      0,
			end:        5,
			expected: []logproto.Sample{
				{Timestamp: 2 * 1e6, Value: 2},
				{Timestamp: 4 * 1e6, Value: 4},
			},
		},
		{
			name: "Start Before First Element -- bytes",
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      0,
			end:        5,
			expected: []logproto.Sample{
				{Timestamp: 2 * 1e6, Value: 2},
				{Timestamp: 4 * 1e6, Value: 4},
			},
		},
		{
			name: "End After Last Element -- count",
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      5,
			end:        10,
			expected: []logproto.Sample{
				{Timestamp: 6 * 1e6, Value: 6},
			},
		},
		{
			name: "End After Last Element -- bytes",
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      5,
			end:        10,
			expected: []logproto.Sample{
				{Timestamp: 6 * 1e6, Value: 6},
			},
		},
		{
			name: "End Exclusive -- count",
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      4,
			end:        6,
			expected: []logproto.Sample{
				{Timestamp: 4 * 1e6, Value: 4},
			},
		},
		{
			name: "End Exclusive -- bytes",
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      4,
			end:        6,
			expected: []logproto.Sample{
				{Timestamp: 4 * 1e6, Value: 4},
			},
		},
		{
			name: "Start before First and End Inclusive of First Element -- count",
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Count: 2},
				{Timestamp: 4, Count: 4},
				{Timestamp: 6, Count: 6},
			}},
			metricType: Count,
			start:      0,
			end:        3,
			expected:   []logproto.Sample{{Timestamp: 2 * 1e6, Value: 2}},
		},
		{
			name: "Start before First and End Inclusive of First Element -- bytes",
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      0,
			end:        3,
			expected:   []logproto.Sample{{Timestamp: 2 * 1e6, Value: 2}},
		},
		{
			name: "Start and End before First Element -- count",
			c: &Chunk{Samples: Samples{
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
			c: &Chunk{Samples: Samples{
				{Timestamp: 2, Bytes: 2},
				{Timestamp: 4, Bytes: 4},
				{Timestamp: 6, Bytes: 6},
			}},
			metricType: Bytes,
			start:      0,
			end:        1,
			expected:   nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tc.c.ForTypeAndRange(tc.metricType, tc.start, tc.end)
			require.NoError(t, err)
			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
			require.Equal(t, len(result), cap(result), "Returned slice wasn't created at the correct capacity")
		})
	}
}

// TODO(twhitney): test the maximum steps logic
func Test_Chunks_Iterator(t *testing.T) {
	lbls := labels.Labels{
		labels.Label{Name: "foo", Value: "bar"},
		labels.Label{Name: "container", Value: "jar"},
	}
	chunks := NewChunks(lbls, NewChunkMetrics(nil, "test"), log.NewNopLogger())
	chunks.chunks = []*Chunk{
		{
			Samples: []Sample{
				{Timestamp: 2, Bytes: 2, Count: 1},
				{Timestamp: 4, Bytes: 4, Count: 3},
				{Timestamp: 6, Bytes: 6, Count: 5},
			},
			mint: 2,
			maxt: 6,
		},
	}

	t.Run("without grouping", func(t *testing.T) {
		it, err := chunks.Iterator(Bytes, nil, 0, 10, 2)
		require.NoError(t, err)

		res, err := iter.ReadAllSamples(it)
		require.NoError(t, err)

		require.Equal(t, 1, len(res.Series))
		require.Equal(t, lbls.String(), res.Series[0].GetLabels())

		it, err = chunks.Iterator(Count, nil, 0, 10, 2)
		require.NoError(t, err)

		res, err = iter.ReadAllSamples(it)
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

		it, err := chunks.Iterator(Bytes, grouping, 0, 10, 2)
		require.NoError(t, err)

		res, err := iter.ReadAllSamples(it)
		require.NoError(t, err)

		require.Equal(t, 1, len(res.Series))
		require.Equal(t, expectedLabels.String(), res.Series[0].GetLabels())

		it, err = chunks.Iterator(Count, grouping, 0, 10, 2)
		require.NoError(t, err)

		res, err = iter.ReadAllSamples(it)
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

		it, err := chunks.Iterator(Bytes, grouping, 0, 10, 2)
		require.NoError(t, err)

		res, err := iter.ReadAllSamples(it)
		require.NoError(t, err)

		require.Equal(t, 1, len(res.Series))
		require.Equal(t, expectedLabels.String(), res.Series[0].GetLabels())

		it, err = chunks.Iterator(Count, grouping, 0, 10, 2)
		require.NoError(t, err)

		res, err = iter.ReadAllSamples(it)
		require.NoError(t, err)

		require.Equal(t, 1, len(res.Series))
		require.Equal(t, expectedLabels.String(), res.Series[0].GetLabels())
	})

	t.Run("handle slice capacity out of range", func(t *testing.T) {
		chunks := NewChunks(lbls, NewChunkMetrics(nil, "test"), log.NewNopLogger())
		chunks.chunks = []*Chunk{
			{
				Samples: []Sample{},
			},
		}

		it, err := chunks.Iterator(Bytes, nil, 5e4, 0, 1e4)
		require.NoError(t, err)

		res, err := iter.ReadAllSamples(it)
		require.NoError(t, err)

		require.Equal(t, 0, len(res.Series))

		it, err = chunks.Iterator(Count, nil, 5e4, 0, 1e4)
		require.NoError(t, err)

		res, err = iter.ReadAllSamples(it)
		require.NoError(t, err)

		require.Equal(t, 0, len(res.Series))
	})
}
