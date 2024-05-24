package iter

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestMerge(t *testing.T) {
	t.Run("merging patterns", func(t *testing.T) {
		tests := []struct {
			name      string
			iterators []Iterator
			expected  []patternSample
		}{
			{
				name:      "Empty iterators",
				iterators: []Iterator{},
				expected:  nil,
			},
			{
				name: "Merge single iterator",
				iterators: []Iterator{
					NewPatternSlice("a", []logproto.PatternSample{
						{Timestamp: 10, Value: 2}, {Timestamp: 20, Value: 4}, {Timestamp: 30, Value: 6},
					}),
				},
				expected: []patternSample{
					{"a", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 10, Value: 2}},
					{"a", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 20, Value: 4}},
					{"a", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 30, Value: 6}},
				},
			},
			{
				name: "Merge multiple iterators",
				iterators: []Iterator{
					NewPatternSlice("a", []logproto.PatternSample{{Timestamp: 10, Value: 2}, {Timestamp: 30, Value: 6}}),
					NewPatternSlice("b", []logproto.PatternSample{{Timestamp: 20, Value: 4}, {Timestamp: 40, Value: 8}}),
				},
				expected: []patternSample{
					{"a", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 10, Value: 2}},
					{"b", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 20, Value: 4}},
					{"a", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 30, Value: 6}},
					{"b", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 40, Value: 8}},
				},
			},
			{
				name: "Merge multiple iterators with similar samples",
				iterators: []Iterator{
					NewPatternSlice("a", []logproto.PatternSample{{Timestamp: 10, Value: 2}, {Timestamp: 30, Value: 6}}),
					NewPatternSlice("a", []logproto.PatternSample{{Timestamp: 10, Value: 2}, {Timestamp: 30, Value: 6}}),
					NewPatternSlice("b", []logproto.PatternSample{{Timestamp: 20, Value: 4}, {Timestamp: 40, Value: 8}}),
				},
				expected: []patternSample{
					{"a", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 10, Value: 4}},
					{"b", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 20, Value: 4}},
					{"a", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 30, Value: 12}},
					{"b", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 40, Value: 8}},
				},
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				it := NewMerge(tt.iterators...)
				defer it.Close()

				var result []patternSample
				for it.Next() {
					result = append(result, patternSample{it.Pattern(), it.Labels(), it.At()})
				}

				require.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("merging label samples", func(t *testing.T) {
		stream1 := labels.Labels{labels.Label{Name: "foo", Value: "bar"}, labels.Label{Name: "ying", Value: "yang"}}
		stream2 := labels.Labels{labels.Label{Name: "foo", Value: "baz"}, labels.Label{Name: "ying", Value: "yang"}}
		tests := []struct {
			name      string
			iterators []Iterator
			expected  []patternSample
		}{
			{
				name:      "Empty iterators",
				iterators: []Iterator{},
				expected:  nil,
			},
			{
				name: "Merge single iterator",
				iterators: []Iterator{
					NewLabelsSlice(stream1, []logproto.PatternSample{
						{Timestamp: 10, Value: 2}, {Timestamp: 20, Value: 4}, {Timestamp: 30, Value: 6},
					}),
				},
				expected: []patternSample{
					{"", stream1, logproto.PatternSample{Timestamp: 10, Value: 2}},
					{"", stream1, logproto.PatternSample{Timestamp: 20, Value: 4}},
					{"", stream1, logproto.PatternSample{Timestamp: 30, Value: 6}},
				},
			},
			{
				name: "Merge multiple iterators",
				iterators: []Iterator{
					NewLabelsSlice(stream1, []logproto.PatternSample{{Timestamp: 10, Value: 2}, {Timestamp: 30, Value: 6}}),
					NewLabelsSlice(stream2, []logproto.PatternSample{{Timestamp: 20, Value: 4}, {Timestamp: 40, Value: 8}}),
				},
				expected: []patternSample{
					{"", stream1, logproto.PatternSample{Timestamp: 10, Value: 2}},
					{"", stream2, logproto.PatternSample{Timestamp: 20, Value: 4}},
					{"", stream1, logproto.PatternSample{Timestamp: 30, Value: 6}},
					{"", stream2, logproto.PatternSample{Timestamp: 40, Value: 8}},
				},
			},
			{
				name: "Merge multiple iterators with similar samples",
				iterators: []Iterator{
					NewLabelsSlice(stream1, []logproto.PatternSample{{Timestamp: 10, Value: 2}, {Timestamp: 30, Value: 6}}),
					NewLabelsSlice(stream1, []logproto.PatternSample{{Timestamp: 10, Value: 2}, {Timestamp: 30, Value: 6}}),
					NewLabelsSlice(stream2, []logproto.PatternSample{{Timestamp: 20, Value: 4}, {Timestamp: 40, Value: 8}}),
				},
				expected: []patternSample{
					{"", stream1, logproto.PatternSample{Timestamp: 10, Value: 4}},
					{"", stream2, logproto.PatternSample{Timestamp: 20, Value: 4}},
					{"", stream1, logproto.PatternSample{Timestamp: 30, Value: 12}},
					{"", stream2, logproto.PatternSample{Timestamp: 40, Value: 8}},
				},
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				it := NewMerge(tt.iterators...)
				defer it.Close()

				var result []patternSample
				for it.Next() {
					result = append(result, patternSample{it.Pattern(), it.Labels(), it.At()})
				}

				require.Equal(t, tt.expected, result)
			})
		}
	})
}
