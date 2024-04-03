package drain

import (
	"reflect"
	"testing"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
)

func TestForRange(t *testing.T) {
	testCases := []struct {
		name     string
		volume   *Volume
		start    model.Time
		end      model.Time
		expected *Volume
	}{
		{
			name:     "Empty Volume",
			volume:   &Volume{},
			start:    1,
			end:      10,
			expected: &Volume{},
		},
		{
			name: "No Overlap",
			volume: &Volume{Values: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start:    10,
			end:      20,
			expected: &Volume{},
		},
		{
			name: "Complete Overlap",
			volume: &Volume{Values: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start: 0,
			end:   10,
			expected: &Volume{Values: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
		},
		{
			name: "Partial Overlap",
			volume: &Volume{Values: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start:    2,
			end:      4,
			expected: &Volume{Values: []logproto.PatternSample{{Timestamp: 3, Value: 4}}},
		},
		{
			name: "Single Element in Range",
			volume: &Volume{Values: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start:    3,
			end:      4,
			expected: &Volume{Values: []logproto.PatternSample{{Timestamp: 3, Value: 4}}},
		},
		{
			name: "Start Before First Element",
			volume: &Volume{Values: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start: 0,
			end:   4,
			expected: &Volume{Values: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
			}},
		},
		{
			name: "End After Last Element",
			volume: &Volume{Values: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start: 4,
			end:   10,
			expected: &Volume{Values: []logproto.PatternSample{
				{Timestamp: 5, Value: 6},
			}},
		},
		{
			name: "Start and End Before First Element",
			volume: &Volume{Values: []logproto.PatternSample{
				{Timestamp: 1, Value: 2},
				{Timestamp: 3, Value: 4},
				{Timestamp: 5, Value: 6},
			}},
			start:    0,
			end:      1,
			expected: &Volume{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.volume.ForRange(tc.start, tc.end)
			if !reflect.DeepEqual(result, tc.expected) {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	tests := []struct {
		x        Volume
		samples  []*logproto.PatternSample
		expected []logproto.PatternSample
	}{
		{
			x: Volume{
				Values: []logproto.PatternSample{
					{Value: 10, Timestamp: 1},
					{Value: 20, Timestamp: 2},
					{Value: 30, Timestamp: 4},
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
			x: Volume{
				Values: []logproto.PatternSample{
					{Value: 5, Timestamp: 1},
					{Value: 15, Timestamp: 3},
					{Value: 25, Timestamp: 4},
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
			x: Volume{
				Values: []logproto.PatternSample{
					{Value: 10, Timestamp: 1},
					{Value: 20, Timestamp: 2},
					{Value: 30, Timestamp: 4},
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
