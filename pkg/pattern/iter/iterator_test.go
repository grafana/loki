package iter

import (
	"testing"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/require"
)

func TestSliceIterator(t *testing.T) {
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
				{"foo", logproto.PatternSample{Timestamp: 10, Value: 2}},
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
				{"foo", logproto.PatternSample{Timestamp: 10, Value: 2}},
				{"foo", logproto.PatternSample{Timestamp: 20, Value: 4}},
				{"foo", logproto.PatternSample{Timestamp: 30, Value: 6}},
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
			got := slice(NewSlice(tt.pattern, tt.samples))
			require.Equal(t, tt.want, got)
		})
	}
}

func slice(it Iterator) []patternSample {
	var samples []patternSample
	defer it.Close()
	for it.Next() {
		samples = append(samples, patternSample{
			pattern: it.Pattern(),
			sample:  it.At(),
		})
	}
	if it.Error() != nil {
		panic(it.Error())
	}
	return samples
}
