package iter

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

func TestSliceIterator(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		level   string
		samples []logproto.PatternSample
		want    []patternSample
	}{
		{
			name:    "1 samples",
			pattern: "foo",
			level:   constants.LogLevelInfo,
			samples: []logproto.PatternSample{
				{Timestamp: 10, Value: 2},
			},
			want: []patternSample{
				{"foo", constants.LogLevelInfo, logproto.PatternSample{Timestamp: 10, Value: 2}},
			},
		},
		{
			name:    "3 samples",
			pattern: "foo",
			level:   constants.LogLevelInfo,
			samples: []logproto.PatternSample{
				{Timestamp: 10, Value: 2},
				{Timestamp: 20, Value: 4},
				{Timestamp: 30, Value: 6},
			},
			want: []patternSample{
				{"foo", constants.LogLevelInfo, logproto.PatternSample{Timestamp: 10, Value: 2}},
				{"foo", constants.LogLevelInfo, logproto.PatternSample{Timestamp: 20, Value: 4}},
				{"foo", constants.LogLevelInfo, logproto.PatternSample{Timestamp: 30, Value: 6}},
			},
		},
		{
			name:    "empty",
			pattern: "foo",
			level:   constants.LogLevelInfo,
			samples: nil,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slice(NewSlice(tt.pattern, tt.level, tt.samples))
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
			level:   it.Level(),
			sample:  it.At(),
		})
	}
	if it.Err() != nil {
		panic(it.Err())
	}
	return samples
}
