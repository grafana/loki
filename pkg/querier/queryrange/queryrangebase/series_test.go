package queryrangebase

import (
	"testing"

	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

func Test_ResponseToSamples(t *testing.T) {
	input := &PrometheusResponse{
		Data: PrometheusData{
			ResultType: string(parser.ValueTypeMatrix),
			Result: []SampleStream{
				{
					Labels: []logproto.LabelAdapter{
						{Name: "a", Value: "a1"},
						{Name: "b", Value: "b1"},
					},
					Samples: []logproto.Sample{
						{
							Value:     1,
							Timestamp: 1,
						},
						{
							Value:     2,
							Timestamp: 2,
						},
					},
				},
				{
					Labels: []logproto.LabelAdapter{
						{Name: "a", Value: "a1"},
						{Name: "b", Value: "b1"},
					},
					Samples: []logproto.Sample{
						{
							Value:     8,
							Timestamp: 1,
						},
						{
							Value:     9,
							Timestamp: 2,
						},
					},
				},
			},
		},
	}

	stream, err := ResponseToSamples(input)
	require.Nil(t, err)
	set := NewSeriesSet(stream)

	setCt := 0

	for set.Next() {
		iter := set.At().Iterator()
		require.Nil(t, set.Err())

		sampleCt := 0
		for iter.Next() {
			ts, v := iter.At()
			require.Equal(t, input.Data.Result[setCt].Samples[sampleCt].Timestamp, ts)
			require.Equal(t, input.Data.Result[setCt].Samples[sampleCt].Value, v)
			sampleCt++
		}
		require.Equal(t, len(input.Data.Result[setCt].Samples), sampleCt)
		setCt++
	}

	require.Equal(t, len(input.Data.Result), setCt)

}
