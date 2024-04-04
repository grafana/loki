package drain

import (
	"reflect"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestAdd(t *testing.T) {
	cks := Chunks{}
	cks.Add(model.Time(timeResolution + 1))
	cks.Add(model.Time(timeResolution + 2))
	cks.Add(model.Time(2*timeResolution + 1))
	require.Equal(t, 1, len(cks))
	require.Equal(t, 2, len(cks[0].Samples))
	cks.Add(model.TimeFromUnixNano(time.Hour.Nanoseconds()) + model.Time(timeResolution+1))
	require.Equal(t, 2, len(cks))
	require.Equal(t, 1, len(cks[1].Samples))
}

func TestIterator(t *testing.T) {
	cks := Chunks{}
	cks.Add(model.Time(timeResolution + 1))
	cks.Add(model.Time(timeResolution + 2))
	cks.Add(model.Time(2*timeResolution + 1))
	cks.Add(model.TimeFromUnixNano(time.Hour.Nanoseconds()) + model.Time(timeResolution+1))

	it := cks.Iterator("test", model.Time(0), model.Time(time.Hour.Nanoseconds()))
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
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start:    10,
			end:      20,
			expected: nil,
		},
		{
			name: "Complete Overlap",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start: 0,
			end:   10,
			expected: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			},
		},
		{
			name: "Partial Overlap",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start:    2,
			end:      4,
			expected: []logproto.PatternSample{{Timestamp: 3, Value: 4}},
		},
		{
			name: "Single Element in Range",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start:    3,
			end:      4,
			expected: []logproto.PatternSample{{Timestamp: 3, Value: 4}},
		},
		{
			name: "Start Before First Element",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start: 0,
			end:   4,
			expected: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
			},
		},
		{
			name: "End After Last Element",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start: 4,
			end:   10,
			expected: []logproto.PatternSample{
				{Timestamp: 5, Value: 6},
			},
		},
		{
			name: "Start and End Before First Element",
			c: &Chunk{Samples: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start:    0,
			end:      1,
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.c.ForRange(tc.start, tc.end)
			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
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
