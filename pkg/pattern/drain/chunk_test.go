package drain

import (
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestAdd(t *testing.T) {
	cks := Chunks{}
	cks.Add(TimeResolution + 1)
	cks.Add(TimeResolution + 2)
	cks.Add(2*TimeResolution + 1)
	require.Equal(t, 1, len(cks))
	require.Equal(t, 2, len(cks[0].Samples))
	cks.Add(model.TimeFromUnixNano(time.Hour.Nanoseconds()) + TimeResolution + 1)
	require.Equal(t, 2, len(cks))
	require.Equal(t, 1, len(cks[1].Samples))
	cks.Add(model.TimeFromUnixNano(time.Hour.Nanoseconds()) - TimeResolution)
	require.Equal(t, 2, len(cks))
	require.Equalf(t, 1, len(cks[1].Samples), "Older samples should not be added if they arrive out of order")
}

func TestIterator(t *testing.T) {
	cks := Chunks{}
	cks.Add(TimeResolution + 1)
	cks.Add(TimeResolution + 2)
	cks.Add(2*TimeResolution + 1)
	cks.Add(model.TimeFromUnixNano(time.Hour.Nanoseconds()) + TimeResolution + 1)

	it := cks.Iterator("test", model.Time(0), model.Time(time.Hour.Nanoseconds()), TimeResolution)
	require.NotNil(t, it)

	var samples []logproto.PatternSample
	for it.Next() {
		samples = append(samples, it.At())
	}
	require.NoError(t, it.Close())
	require.Equal(t, 3, len(samples))
	require.Equal(t, []logproto.PatternSample{
		{Timestamp: 10000, Value: 2},
		{Timestamp: 20000, Value: 1},
		{Timestamp: 3610000, Value: 1},
	}, samples)
}

func TestForRange(t *testing.T) {
	testCases := []struct {
		name     string
		c        *Chunk
		start    model.Time
		end      model.Time
		step     model.Time
		expected []logproto.PatternSample
	}{
		{
			name:     "Empty Volume",
			c:        &Chunk{},
			start:    1,
			end:      10,
			expected: nil,
		},
		{
			name: "No Overlap",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 2, Value: 2},
				{Timestamp: 4, Value: 4},
				{Timestamp: 6, Value: 6},
			}},
			start:    10,
			end:      20,
			expected: nil,
		},
		{
			name: "Complete Overlap",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 2, Value: 2},
				{Timestamp: 4, Value: 4},
				{Timestamp: 6, Value: 6},
			}},
			start: 0,
			end:   10,
			expected: []logproto.PatternSample{
				{Timestamp: 2, Value: 2},
				{Timestamp: 4, Value: 4},
				{Timestamp: 6, Value: 6},
			},
		},
		{
			name: "Partial Overlap",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 2, Value: 2},
				{Timestamp: 4, Value: 4},
				{Timestamp: 6, Value: 6},
			}},
			start:    3,
			end:      5,
			expected: []logproto.PatternSample{{Timestamp: 4, Value: 4}},
		},
		{
			name: "Single Element in Range",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 2, Value: 2},
				{Timestamp: 4, Value: 4},
				{Timestamp: 6, Value: 6},
			}},
			start:    4,
			end:      5,
			expected: []logproto.PatternSample{{Timestamp: 4, Value: 4}},
		},
		{
			name: "Start Before First Element",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 2, Value: 2},
				{Timestamp: 4, Value: 4},
				{Timestamp: 6, Value: 6},
			}},
			start: 0,
			end:   5,
			expected: []logproto.PatternSample{
				{Timestamp: 2, Value: 2},
				{Timestamp: 4, Value: 4},
			},
		},
		{
			name: "End After Last Element",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 2, Value: 2},
				{Timestamp: 4, Value: 4},
				{Timestamp: 6, Value: 6},
			}},
			start: 5,
			end:   10,
			expected: []logproto.PatternSample{
				{Timestamp: 6, Value: 6},
			},
		},
		{
			name: "Start and End Before First Element",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 2, Value: 2},
				{Timestamp: 4, Value: 4},
				{Timestamp: 6, Value: 6},
			}},
			start:    0,
			end:      2,
			expected: nil,
		},
		{
			name: "Higher resolution samples down-sampled to preceding step bucket",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 2, Value: 4},
				{Timestamp: 3, Value: 6},
				{Timestamp: 4, Value: 8},
				{Timestamp: 5, Value: 10},
				{Timestamp: 6, Value: 12},
			}},
			start: 1,
			end:   6,
			expected: []logproto.PatternSample{
				{Timestamp: 0, Value: 2},
				{Timestamp: 2, Value: 10},
				{Timestamp: 4, Value: 18},
				{Timestamp: 6, Value: 12},
			},
		},
		{
			name: "Low resolution samples insert 0 values for empty steps",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 5, Value: 10},
			}},
			start: 1,
			end:   6,
			expected: []logproto.PatternSample{
				{Timestamp: 0, Value: 2},
				{Timestamp: 2, Value: 0},
				{Timestamp: 4, Value: 10},
			},
		},
		{
			name: "Out-of-order samples generate nil result",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 5, Value: 2},
				{Timestamp: 3, Value: 2},
			}},
			start:    4,
			end:      6,
			expected: nil,
		},
		{
			name: "Internally out-of-order samples generate nil result",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 5, Value: 2},
				{Timestamp: 3, Value: 2},
				{Timestamp: 7, Value: 2},
			}},
			start:    2,
			end:      6,
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.c.ForRange(tc.start, tc.end, model.Time(2))
			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
			require.Equal(t, len(result), cap(result), "Returned slice wasn't created at the correct capacity")
		})
	}
}

func TestMerge(t *testing.T) {
	tests := []struct {
		x        Chunks
		samples  []*logproto.PatternSample
		expected []logproto.PatternSample
	}{
		{
			x: Chunks{
				Chunk{
					Samples: []logproto.PatternSample{
						{Value: 10, Timestamp: 1},
						{Value: 20, Timestamp: 2},
						{Value: 30, Timestamp: 4},
					},
				},
			},
			samples: []*logproto.PatternSample{
				{Value: 5, Timestamp: 1},
				{Value: 15, Timestamp: 3},
				{Value: 25, Timestamp: 4},
			},
			expected: []logproto.PatternSample{
				{Value: 15, Timestamp: 1},
				{Value: 20, Timestamp: 2},
				{Value: 15, Timestamp: 3},
				{Value: 55, Timestamp: 4},
			},
		},
		{
			x: Chunks{
				Chunk{
					Samples: []logproto.PatternSample{
						{Value: 5, Timestamp: 1},
						{Value: 15, Timestamp: 3},
						{Value: 25, Timestamp: 4},
					},
				},
			},
			samples: []*logproto.PatternSample{
				{Value: 10, Timestamp: 1},
				{Value: 20, Timestamp: 2},
				{Value: 30, Timestamp: 4},
			},
			expected: []logproto.PatternSample{
				{Value: 15, Timestamp: 1},
				{Value: 20, Timestamp: 2},
				{Value: 15, Timestamp: 3},
				{Value: 55, Timestamp: 4},
			},
		},
		{
			x: Chunks{
				Chunk{
					Samples: []logproto.PatternSample{
						{Value: 10, Timestamp: 1},
						{Value: 20, Timestamp: 2},
						{Value: 30, Timestamp: 4},
					},
				},
			},
			samples: []*logproto.PatternSample{},
			expected: []logproto.PatternSample{
				{Value: 10, Timestamp: 1},
				{Value: 20, Timestamp: 2},
				{Value: 30, Timestamp: 4},
			},
		},
	}

	for _, test := range tests {
		result := test.x.merge(test.samples)
		if !reflect.DeepEqual(result, test.expected) {
			t.Errorf("Expected: %v, Got: %v", test.expected, result)
		}
	}
}

func TestPrune(t *testing.T) {
	olderThan := time.Hour * 3

	t.Run("Empty Chunks", func(t *testing.T) {
		cks := Chunks{}
		cks.prune(olderThan)
		require.Empty(t, cks)
	})

	t.Run("No Pruning", func(t *testing.T) {
		cks := Chunks{
			Chunk{
				Samples: []logproto.PatternSample{
					{Timestamp: model.TimeFromUnixNano(time.Now().UnixNano() - (olderThan.Nanoseconds()) + (1 * time.Minute).Nanoseconds())},
					{Timestamp: model.TimeFromUnixNano(time.Now().UnixNano() - (olderThan.Nanoseconds()) + (2 * time.Minute).Nanoseconds())},
				},
			},
		}
		cks.prune(olderThan)
		require.Len(t, cks, 1)
	})

	now := time.Now()
	t.Run("Pruning", func(t *testing.T) {
		cks := Chunks{
			Chunk{
				Samples: []logproto.PatternSample{
					{Timestamp: model.TimeFromUnixNano(now.UnixNano() - (olderThan.Nanoseconds()) - (1 * time.Minute).Nanoseconds())},
					{Timestamp: model.TimeFromUnixNano(now.UnixNano() - (olderThan.Nanoseconds()) - (2 * time.Minute).Nanoseconds())},
				},
			},
			Chunk{
				Samples: []logproto.PatternSample{
					{Timestamp: model.TimeFromUnixNano(now.UnixNano() - (olderThan.Nanoseconds()) - (1 * time.Minute).Nanoseconds())},
					{Timestamp: model.TimeFromUnixNano(now.UnixNano() - (olderThan.Nanoseconds()) - (2 * time.Minute).Nanoseconds())},
				},
			},
			Chunk{
				Samples: []logproto.PatternSample{
					{Timestamp: model.TimeFromUnixNano(now.UnixNano() - (olderThan.Nanoseconds()) + (1 * time.Minute).Nanoseconds())},
					{Timestamp: model.TimeFromUnixNano(now.UnixNano() - (olderThan.Nanoseconds()) + (2 * time.Minute).Nanoseconds())},
				},
			},
			Chunk{
				Samples: []logproto.PatternSample{
					{Timestamp: model.TimeFromUnixNano(now.UnixNano() - (olderThan.Nanoseconds()) - (1 * time.Minute).Nanoseconds())},
					{Timestamp: model.TimeFromUnixNano(now.UnixNano() - (olderThan.Nanoseconds()) - (2 * time.Minute).Nanoseconds())},
				},
			},
		}
		cks.prune(olderThan)
		require.Len(t, cks, 1)
		require.Equal(t, []logproto.PatternSample{
			{Timestamp: model.TimeFromUnixNano(now.UnixNano() - (olderThan.Nanoseconds()) + (1 * time.Minute).Nanoseconds())},
			{Timestamp: model.TimeFromUnixNano(now.UnixNano() - (olderThan.Nanoseconds()) + (2 * time.Minute).Nanoseconds())},
		}, cks[0].Samples)
	})
}
