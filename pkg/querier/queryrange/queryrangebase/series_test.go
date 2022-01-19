package queryrangebase

import (
	"testing"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"
)

func Test_ResponseToSamples(t *testing.T) {
	input := &PrometheusResponse{
		Data: PrometheusData{
			ResultType: string(parser.ValueTypeMatrix),
			Result: []SampleStream{
				{
					Labels: []cortexpb.LabelAdapter{
						{Name: "a", Value: "a1"},
						{Name: "b", Value: "b1"},
					},
					Samples: []cortexpb.Sample{
						{
							Value:       1,
							TimestampMs: 1,
						},
						{
							Value:       2,
							TimestampMs: 2,
						},
					},
				},
				{
					Labels: []cortexpb.LabelAdapter{
						{Name: "a", Value: "a1"},
						{Name: "b", Value: "b1"},
					},
					Samples: []cortexpb.Sample{
						{
							Value:       8,
							TimestampMs: 1,
						},
						{
							Value:       9,
							TimestampMs: 2,
						},
					},
				},
			},
		},
	}

	streams, err := ResponseToSamples(input)
	require.Nil(t, err)
	set := NewSeriesSet(streams)

	setCt := 0

	for set.Next() {
		iter := set.At().Iterator()
		require.Nil(t, set.Err())

		sampleCt := 0
		for iter.Next() {
			ts, v := iter.At()
			require.Equal(t, input.Data.Result[setCt].Samples[sampleCt].TimestampMs, ts)
			require.Equal(t, input.Data.Result[setCt].Samples[sampleCt].Value, v)
			sampleCt++
		}
		require.Equal(t, len(input.Data.Result[setCt].Samples), sampleCt)
		setCt++
	}

	require.Equal(t, len(input.Data.Result), setCt)

}
