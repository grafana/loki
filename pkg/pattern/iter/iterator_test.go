package iter

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestSliceIterator(t *testing.T) {
	t.Run("samples with pattern", func(t *testing.T) {
		tests := []struct {
			name    string
			pattern string
			samples []logproto.PatternSample
			want    []patternSample
		}{
			{
				name:    "1 samples",
				pattern: "foo",
				samples: []logproto.PatternSample{
					{Timestamp: 10, Value: 2},
				},
				want: []patternSample{
					{"foo", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 10, Value: 2}},
				},
			},
			{
				name:    "3 samples",
				pattern: "foo",
				samples: []logproto.PatternSample{
					{Timestamp: 10, Value: 2},
					{Timestamp: 20, Value: 4},
					{Timestamp: 30, Value: 6},
				},
				want: []patternSample{
					{"foo", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 10, Value: 2}},
					{"foo", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 20, Value: 4}},
					{"foo", labels.EmptyLabels(), logproto.PatternSample{Timestamp: 30, Value: 6}},
				},
			},
			{
				name:    "empty",
				pattern: "foo",
				samples: nil,
				want:    nil,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				got := slice(NewPatternSlice(tt.pattern, tt.samples))
				require.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("samples with labels", func(t *testing.T) {
		stream := labels.Labels{
			{Name: "test", Value: "test"},
		}
		tests := []struct {
			name    string
			labels  labels.Labels
			samples []logproto.PatternSample
			want    []patternSample
		}{
			{
				name:   "1 samples",
				labels: stream,
				samples: []logproto.PatternSample{
					{Timestamp: 10, Value: 2},
				},
				want: []patternSample{
					{"", stream, logproto.PatternSample{Timestamp: 10, Value: 2}},
				},
			},
			{
				name:   "3 samples",
				labels: stream,
				samples: []logproto.PatternSample{
					{Timestamp: 10, Value: 2},
					{Timestamp: 20, Value: 4},
					{Timestamp: 30, Value: 6},
				},
				want: []patternSample{
					{"", stream, logproto.PatternSample{Timestamp: 10, Value: 2}},
					{"", stream, logproto.PatternSample{Timestamp: 20, Value: 4}},
					{"", stream, logproto.PatternSample{Timestamp: 30, Value: 6}},
				},
			},
			{
				name:    "empty",
				labels:  labels.EmptyLabels(),
				samples: nil,
				want:    nil,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				got := slice(NewLabelsSlice(tt.labels, tt.samples))
				require.Equal(t, tt.want, got)
			})
		}
	})
}

func slice(it Iterator) []patternSample {
	var samples []patternSample
	defer it.Close()
	for it.Next() {
		samples = append(samples, patternSample{
			pattern: it.Pattern(),
			sample:  it.At(),
			labels:  it.Labels(),
		})
	}
	if it.Error() != nil {
		panic(it.Error())
	}
	return samples
}
