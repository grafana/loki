package queryrangebase

import (
	"testing"

	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
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
					Samples: []logproto.LegacySample{
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
					Labels: []logproto.LabelAdapter{
						{Name: "a", Value: "a1"},
						{Name: "b", Value: "b1"},
					},
					Samples: []logproto.LegacySample{
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

	stream, err := ResponseToSamples(input)
	require.Nil(t, err)
	set := NewSeriesSet(stream)

	setCt := 0

	var iter chunkenc.Iterator
	for set.Next() {
		iter = set.At().Iterator(iter)
		require.Nil(t, set.Err())

		sampleCt := 0
		for iter.Next() != chunkenc.ValNone {
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
