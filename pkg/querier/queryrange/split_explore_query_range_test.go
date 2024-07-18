package queryrange

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase/definitions"
	"github.com/stretchr/testify/require"
)

func Test_mergeResponses(t *testing.T) {
	log := log.NewNopLogger()

	lblAdapt := push.LabelsAdapter{
		{
			Name:  "foo",
			Value: "bar",
		},
		{
			Name:  "container",
			Value: "box",
		},
	}

	lbls := logproto.FromLabelAdaptersToLabels(lblAdapt)

	lokiPromResponse := func(samples []logproto.LegacySample) LokiPromResponse {
		return LokiPromResponse{
			Response: &queryrangebase.PrometheusResponse{
				Status: "success",
				Data: queryrangebase.PrometheusData{
					ResultType: "matrix",
					Result: []queryrangebase.SampleStream{
						{
							Labels:  lblAdapt,
							Samples: samples,
						},
					},
				},
				Headers: []*definitions.PrometheusResponseHeader{
					{
						Name: "Content-Type",
						Values: []string{
							"application/json",
						},
					},
				},
				Warnings: []string{
					"warning",
				},
			},
		}
	}

	querySamplesResponse := func(samples []logproto.Sample) QuerySamplesResponse {
		return QuerySamplesResponse{
			Response: &logproto.QuerySamplesResponse{
				Series: []logproto.Series{
					{
						Labels:     lbls.String(),
						Samples:    samples,
						StreamHash: lbls.Hash(),
					},
				},
				Warnings: []string{
					"warning",
				},
			},
			Headers: []definitions.PrometheusResponseHeader{
				{
					Name: "Content-Type",
					Values: []string{
						"application/json",
					},
				},
			},
		}
	}

	t.Run("it merges a single response", func(t *testing.T) {
		lresp := lokiPromResponse(
			[]logproto.LegacySample{
				{
					Value:       1,
					TimestampMs: 1e3,
				},
				{
					Value:       2,
					TimestampMs: 2e3,
				},
			},
		)

		resp, err := mergeResponses(log, &lresp)
		require.NoError(t, err)

		merged, ok := resp.(*LokiPromResponse)
		require.True(t, ok)

		require.Equal(t, "matrix", merged.Response.Data.ResultType)

		require.Len(t, merged.Response.Data.Result, 1)

		result := merged.Response.Data.Result[0]
		require.Len(t, result.Labels, 2)

		require.Equal(t, push.LabelAdapter{
			Name:  "foo",
			Value: "bar",
		}, result.Labels[0])
		require.Equal(t, push.LabelAdapter{
			Name:  "container",
			Value: "box",
		}, result.Labels[1])
		require.Len(t, result.Samples, 2)
		require.Equal(t, int64(1e3), result.Samples[0].TimestampMs)
		require.Equal(t, float64(1), result.Samples[0].Value)
		require.Equal(t, int64(2e3), result.Samples[1].TimestampMs)
		require.Equal(t, float64(2), result.Samples[1].Value)

		qsResponse := querySamplesResponse(
			[]logproto.Sample{
				{
					Timestamp: 1e9,
					Value:     1,
				},
				{
					Timestamp: 2e9,
					Value:     2,
				},
			},
		)

		resp, err = mergeResponses(log, &qsResponse)
		require.NoError(t, err)

		mergedQs, ok := resp.(*QuerySamplesResponse)
		require.True(t, ok)

		require.Len(t, mergedQs.Response.Series, 1)
		series := mergedQs.Response.Series[0]
		require.Equal(t, lbls.String(), series.Labels)

		require.Len(t, series.Samples, 2)
		require.Equal(t, int64(1e9), series.Samples[0].Timestamp)
		require.Equal(t, float64(1), series.Samples[0].Value)
		require.Equal(t, int64(2e9), series.Samples[1].Timestamp)
		require.Equal(t, float64(2), series.Samples[1].Value)
	})

	t.Run("it merges a single response from each query path", func(t *testing.T) {
		lresp := lokiPromResponse([]logproto.LegacySample{
			{
				Value:       1,
				TimestampMs: 1e3,
			},
			{
				Value:       2,
				TimestampMs: 2e3,
			},
		})

		qsResponse := querySamplesResponse([]logproto.Sample{
			{
				Timestamp: 3e9,
				Value:     3,
			},
			{
				Timestamp: 4e9,
				Value:     4,
			},
		})

		resp, err := mergeResponses(log, &lresp, &qsResponse)
		require.NoError(t, err)

		mergedQs, ok := resp.(*QuerySamplesResponse)
		require.True(t, ok)

		require.Len(t, mergedQs.Response.Series, 1)
		series := mergedQs.Response.Series[0]
		require.Equal(t, lbls.String(), series.Labels)

		require.Len(t, series.Samples, 4)
		require.Equal(t, int64(1e9), series.Samples[0].Timestamp)
		require.Equal(t, float64(1), series.Samples[0].Value)
		require.Equal(t, int64(2e9), series.Samples[1].Timestamp)
		require.Equal(t, float64(2), series.Samples[1].Value)
		require.Equal(t, int64(3e9), series.Samples[2].Timestamp)
		require.Equal(t, float64(3), series.Samples[2].Value)
		require.Equal(t, int64(4e9), series.Samples[3].Timestamp)
		require.Equal(t, float64(4), series.Samples[3].Value)
	})

	t.Run("it merges multiples responses from each query path", func(t *testing.T) {
		lresp := lokiPromResponse([]logproto.LegacySample{
			{
				Value:       1,
				TimestampMs: 1e3,
			},
			{
				Value:       2,
				TimestampMs: 2e3,
			},
		})

		qsResponse := querySamplesResponse([]logproto.Sample{
			{
				Timestamp: 3e9,
				Value:     3,
			},
			{
				Timestamp: 4e9,
				Value:     4,
			},
		})

		lres2 := lokiPromResponse([]logproto.LegacySample{
			{
				Value:       5,
				TimestampMs: 5e3,
			},
			{
				Value:       6,
				TimestampMs: 6e3,
			},
		})

		qsResponse2 := querySamplesResponse([]logproto.Sample{
			{
				Timestamp: 7e9,
				Value:     7,
			},
			{
				Timestamp: 8e9,
				Value:     8,
			},
		})

		resp, err := mergeResponses(log, &lresp, &qsResponse, &lres2, &qsResponse2)
		require.NoError(t, err)

		mergedQs, ok := resp.(*QuerySamplesResponse)
		require.True(t, ok)

		require.Len(t, mergedQs.Response.Series, 1)
		series := mergedQs.Response.Series[0]
		require.Equal(t, lbls.String(), series.Labels)

		require.Len(t, series.Samples, 8)
		require.Equal(t, int64(1e9), series.Samples[0].Timestamp)
		require.Equal(t, float64(1), series.Samples[0].Value)
		require.Equal(t, int64(2e9), series.Samples[1].Timestamp)
		require.Equal(t, float64(2), series.Samples[1].Value)
		require.Equal(t, int64(3e9), series.Samples[2].Timestamp)
		require.Equal(t, float64(3), series.Samples[2].Value)
		require.Equal(t, int64(4e9), series.Samples[3].Timestamp)
		require.Equal(t, float64(4), series.Samples[3].Value)
		require.Equal(t, int64(5e9), series.Samples[4].Timestamp)
		require.Equal(t, float64(5), series.Samples[4].Value)
		require.Equal(t, int64(6e9), series.Samples[5].Timestamp)
		require.Equal(t, float64(6), series.Samples[5].Value)
		require.Equal(t, int64(7e9), series.Samples[6].Timestamp)
		require.Equal(t, float64(7), series.Samples[6].Value)
		require.Equal(t, int64(8e9), series.Samples[7].Timestamp)
		require.Equal(t, float64(8), series.Samples[7].Value)
	})

	t.Run("it removes overlapping samples", func(t *testing.T) {
		lresp := lokiPromResponse([]logproto.LegacySample{
			{
				Value:       1,
				TimestampMs: 1e3,
			},
			{
				Value:       2,
				TimestampMs: 2e3,
			},
		})

		qsResponse := querySamplesResponse([]logproto.Sample{
			{
				Timestamp: 2e9,
				Value:     2,
			},
			{
				Timestamp: 3e9,
				Value:     3,
			},
		})

		lres2 := lokiPromResponse([]logproto.LegacySample{
			{
				Value:       1,
				TimestampMs: 1e3,
			},
			{
				Value:       3,
				TimestampMs: 3e3,
			},
		})

		qsResponse2 := querySamplesResponse([]logproto.Sample{
			{
				Timestamp: 2e9,
				Value:     2,
			},
			{
				Timestamp: 4e9,
				Value:     4,
			},
		})

		resp, err := mergeResponses(log, &lresp, &qsResponse, &lres2, &qsResponse2)
		require.NoError(t, err)

		mergedQs, ok := resp.(*QuerySamplesResponse)
		require.True(t, ok)

		require.Len(t, mergedQs.Response.Series, 1)
		series := mergedQs.Response.Series[0]
		require.Equal(t, lbls.String(), series.Labels)

		require.Len(t, series.Samples, 4)
		require.Equal(t, int64(1e9), series.Samples[0].Timestamp)
		require.Equal(t, float64(1), series.Samples[0].Value)
		require.Equal(t, int64(2e9), series.Samples[1].Timestamp)
		require.Equal(t, float64(2), series.Samples[1].Value)
		require.Equal(t, int64(3e9), series.Samples[2].Timestamp)
		require.Equal(t, float64(3), series.Samples[2].Value)
		require.Equal(t, int64(4e9), series.Samples[3].Timestamp)
		require.Equal(t, float64(4), series.Samples[3].Value)
	})

	t.Run("it merges multiple response with multiple series", func(t *testing.T) {
		lblAdapt2 := push.LabelsAdapter{
			{
				Name:  "foo",
				Value: "bar",
			},
			{
				Name:  "container",
				Value: "jar",
			},
		}

		lbls2 := logproto.FromLabelAdaptersToLabels(lblAdapt2)

		lblAdapt3 := push.LabelsAdapter{
			{
				Name:  "foo",
				Value: "baz",
			},
		}

		lbls3 := logproto.FromLabelAdaptersToLabels(lblAdapt3)

		lresp := LokiPromResponse{
			Response: &queryrangebase.PrometheusResponse{
				Status: "success",
				Data: queryrangebase.PrometheusData{
					ResultType: "matrix",
					Result: []queryrangebase.SampleStream{
						{
							Labels: lblAdapt,
							Samples: []logproto.LegacySample{
								{
									Value:       1,
									TimestampMs: 1e3,
								},
							},
						},
						{
							Labels: lblAdapt2,
							Samples: []logproto.LegacySample{
								{
									Value:       1,
									TimestampMs: 1e3,
								},
							},
						},
						{
							Labels: lblAdapt3,
							Samples: []logproto.LegacySample{
								{
									Value:       1,
									TimestampMs: 1e3,
								},
							},
						},
					},
				},
				Headers: []*definitions.PrometheusResponseHeader{
					{
						Name: "Content-Type",
						Values: []string{
							"application/json",
						},
					},
				},
				Warnings: []string{
					"warning",
				},
			},
		}

		qsResponse := QuerySamplesResponse{
			Response: &logproto.QuerySamplesResponse{
				Series: []logproto.Series{
					{
						Labels: lbls.String(),
						Samples: []logproto.Sample{
							{
								Timestamp: 2e9,
								Value:     2,
							},
						},
						StreamHash: lbls.Hash(),
					},
					{
						Labels: lbls2.String(),
						Samples: []logproto.Sample{
							{
								Timestamp: 2e9,
								Value:     2,
							},
						},
						StreamHash: lbls.Hash(),
					},
					{
						Labels: lbls3.String(),
						Samples: []logproto.Sample{
							{
								Timestamp: 2e9,
								Value:     2,
							},
						},
						StreamHash: lbls.Hash(),
					},
				},
				Warnings: []string{
					"warning",
				},
			},
			Headers: []definitions.PrometheusResponseHeader{
				{
					Name: "Content-Type",
					Values: []string{
						"application/json",
					},
				},
			},
		}

		resp, err := mergeResponses(log, &lresp, &qsResponse)
		require.NoError(t, err)

		mergedQs, ok := resp.(*QuerySamplesResponse)
		require.True(t, ok)

		require.Len(t, mergedQs.Response.Series, 3)

		series1 := mergedQs.Response.Series[0]
		require.Equal(t, lbls.String(), series1.Labels)

		require.Len(t, series1.Samples, 2)
		require.Equal(t, int64(1e9), series1.Samples[0].Timestamp)
		require.Equal(t, float64(1), series1.Samples[0].Value)
		require.Equal(t, int64(2e9), series1.Samples[1].Timestamp)
		require.Equal(t, float64(2), series1.Samples[1].Value)

		series2 := mergedQs.Response.Series[1]
		require.Equal(t, lbls2.String(), series2.Labels)

		require.Len(t, series2.Samples, 2)
		require.Equal(t, int64(1e9), series2.Samples[0].Timestamp)
		require.Equal(t, float64(1), series2.Samples[0].Value)
		require.Equal(t, int64(2e9), series2.Samples[1].Timestamp)
		require.Equal(t, float64(2), series2.Samples[1].Value)

		series3 := mergedQs.Response.Series[2]
		require.Equal(t, lbls3.String(), series3.Labels)

		require.Len(t, series3.Samples, 2)
		require.Equal(t, int64(1e9), series3.Samples[0].Timestamp)
		require.Equal(t, float64(1), series3.Samples[0].Value)
		require.Equal(t, int64(2e9), series3.Samples[1].Timestamp)
		require.Equal(t, float64(2), series3.Samples[1].Value)
	})
}
