package queryrange

import (
	"testing"
	"time"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/stretchr/testify/require"
)

const forRangeQuery = false
const forInstantQuery = true
const aggregateBySeries = true
const aggregateByLabels = false

func Test_toPrometheusResponse(t *testing.T) {
	t2 := time.Now()
	t1 := t2.Add(-1 * time.Minute)

	setup := func(instant bool, nameOne, nameTwo string) chan *bucketedVolumeResponse {
		collector := make(chan *bucketedVolumeResponse, 2)
		defer close(collector)

		collector <- &bucketedVolumeResponse{
			t1, &VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{
							Name:   nameOne,
							Volume: 100,
						},
						{
							Name:   nameTwo,
							Volume: 50,
						},
					},
					Limit: 10,
				},
			},
		}

		if !instant {
			collector <- &bucketedVolumeResponse{
				t2, &VolumeResponse{
					Response: &logproto.VolumeResponse{
						Volumes: []logproto.Volume{
							{
								Name:   nameOne,
								Volume: 50,
							},
							{
								Name:   nameTwo,
								Volume: 25,
							},
						},
						Limit: 10,
					},
				},
			}
		}

		return collector
	}

	setupSeries := func(instant bool) chan *bucketedVolumeResponse {
		return setup(instant, `{foo="bar", fizz="buzz"}`, `{foo="baz"}`)
	}

	setupLabels := func(instant bool) chan *bucketedVolumeResponse {
		return setup(instant, `foo`, `fizz`)
	}

	t.Run("it converts series volumes with multiple timestamps into a prometheus timeseries matix response", func(t *testing.T) {
		collector := setupSeries(forRangeQuery)
		promResp := ToPrometheusResponse(collector, aggregateBySeries)
		require.Equal(t, queryrangebase.PrometheusData{
			ResultType: loghttp.ResultTypeMatrix,
			Result: []queryrangebase.SampleStream{
				{
					Labels: []push.LabelAdapter{
						{
							Name:  "fizz",
							Value: "buzz",
						},
						{
							Name:  "foo",
							Value: "bar",
						},
					},
					Samples: []logproto.LegacySample{
						{
							Value:       100,
							TimestampMs: t1.UnixNano() / 1e6,
						},
						{
							Value:       50,
							TimestampMs: t2.UnixNano() / 1e6,
						},
					},
				},
				{
					Labels: []push.LabelAdapter{
						{
							Name:  "foo",
							Value: "baz",
						},
					},
					Samples: []logproto.LegacySample{
						{
							Value:       50,
							TimestampMs: t1.UnixNano() / 1e6,
						},
						{
							Value:       25,
							TimestampMs: t2.UnixNano() / 1e6,
						},
					},
				},
			},
		}, promResp.Response.Data)
	})

	t.Run("it converts series volumes with a single timestamp into a prometheus timeseries vector response", func(t *testing.T) {
		collector := setupSeries(forInstantQuery)
		promResp := ToPrometheusResponse(collector, aggregateBySeries)
		require.Equal(t, queryrangebase.PrometheusData{
			ResultType: loghttp.ResultTypeVector,
			Result: []queryrangebase.SampleStream{
				{
					Labels: []push.LabelAdapter{
						{
							Name:  "fizz",
							Value: "buzz",
						},
						{
							Name:  "foo",
							Value: "bar",
						},
					},
					Samples: []logproto.LegacySample{
						{
							Value:       100,
							TimestampMs: t1.UnixNano() / 1e6,
						},
					},
				},
				{
					Labels: []push.LabelAdapter{
						{
							Name:  "foo",
							Value: "baz",
						},
					},
					Samples: []logproto.LegacySample{
						{
							Value:       50,
							TimestampMs: t1.UnixNano() / 1e6,
						},
					},
				},
			},
		}, promResp.Response.Data)
	})

	t.Run("it converts label volumes with multiple timestamps into a prometheus timeseries matrix response", func(t *testing.T) {
		collector := setupLabels(forRangeQuery)
		promResp := ToPrometheusResponse(collector, aggregateByLabels)
		require.Equal(t, queryrangebase.PrometheusData{
			ResultType: loghttp.ResultTypeMatrix,
			Result: []queryrangebase.SampleStream{
				{
					Labels: []push.LabelAdapter{
						{
							Name:  "fizz",
							Value: "",
						},
					},
					Samples: []logproto.LegacySample{
						{
							Value:       50,
							TimestampMs: t1.UnixNano() / 1e6,
						},
						{
							Value:       25,
							TimestampMs: t2.UnixNano() / 1e6,
						},
					},
				},
				{
					Labels: []push.LabelAdapter{
						{
							Name:  "foo",
							Value: "",
						},
					},
					Samples: []logproto.LegacySample{
						{
							Value:       100,
							TimestampMs: t1.UnixNano() / 1e6,
						},
						{
							Value:       50,
							TimestampMs: t2.UnixNano() / 1e6,
						},
					},
				},
			},
		}, promResp.Response.Data)
	})

	t.Run("it converts label volumes with a single timestamp into a prometheus timeseries vector response", func(t *testing.T) {
		collector := setupLabels(forInstantQuery)
		promResp := ToPrometheusResponse(collector, aggregateByLabels)
		require.Equal(t, queryrangebase.PrometheusData{
			ResultType: loghttp.ResultTypeVector,
			Result: []queryrangebase.SampleStream{
				{
					Labels: []push.LabelAdapter{
						{
							Name:  "fizz",
							Value: "",
						},
					},
					Samples: []logproto.LegacySample{
						{
							Value:       50,
							TimestampMs: t1.UnixNano() / 1e6,
						},
					},
				},
				{
					Labels: []push.LabelAdapter{
						{
							Name:  "foo",
							Value: "",
						},
					},
					Samples: []logproto.LegacySample{
						{
							Value:       100,
							TimestampMs: t1.UnixNano() / 1e6,
						},
					},
				},
			},
		}, promResp.Response.Data)
	})
}
