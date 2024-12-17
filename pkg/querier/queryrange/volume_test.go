package queryrange

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
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
		return setup(instant, `{foo="baz"}`, `{foo="bar", fizz="buzz"}`)
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
							Name:  "foo",
							Value: "baz",
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
							Name:  "foo",
							Value: "baz",
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
			},
		}, promResp.Response.Data)
	})
}

func Test_VolumeMiddleware(t *testing.T) {
	makeVolumeRequest := func(req *logproto.VolumeRequest) *queryrangebase.PrometheusResponse {
		nextHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
			return &VolumeResponse{
				Response: &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{
							Name:   `{foo="bar"}`,
							Volume: 42,
						},
					},
				},
			}, nil
		})

		m := NewVolumeMiddleware()
		wrapped := m.Wrap(nextHandler)

		ctx := user.InjectOrgID(context.Background(), "fake")
		resp, err := wrapped.Do(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		return resp.(*LokiPromResponse).Response
	}

	t.Run("it breaks query up into subqueries according to step", func(t *testing.T) {
		volumeReq := &logproto.VolumeRequest{
			From:        10,
			Through:     20,
			Matchers:    `{foo="bar"}`,
			Limit:       seriesvolume.DefaultLimit,
			Step:        1,
			AggregateBy: seriesvolume.Series,
		}
		promResp := makeVolumeRequest(volumeReq)

		require.Equal(t, promResp.Data.ResultType, loghttp.ResultTypeMatrix)
		require.Equal(t, len(promResp.Data.Result), 1)
		require.Equal(t, len(promResp.Data.Result[0].Samples), 10)
	})

	t.Run("only returns one datapoint when step is > than time range", func(t *testing.T) {
		volumeReq := &logproto.VolumeRequest{
			From:        10,
			Through:     20,
			Matchers:    `{foo="bar"}`,
			Limit:       seriesvolume.DefaultLimit,
			Step:        20,
			AggregateBy: seriesvolume.Series,
		}
		promResp := makeVolumeRequest(volumeReq)

		require.Equal(t, promResp.Data.ResultType, loghttp.ResultTypeVector)
		require.Equal(t, len(promResp.Data.Result), 1)
		require.Equal(t, len(promResp.Data.Result[0].Samples), 1)
	})

	t.Run("when requested time range is not evenly divisible by step, an extra datpoint is added", func(t *testing.T) {
		volumeReq := &logproto.VolumeRequest{
			From:        1698830441000, // 2023-11-01T09:20:41Z
			Through:     1698830498000, // 2023-11-01T09:21:38Z, difference is 57s
			Matchers:    `{foo="bar"}`,
			Limit:       seriesvolume.DefaultLimit,
			Step:        60000, // 60s
			AggregateBy: seriesvolume.Series,
		}
		promResp := makeVolumeRequest(volumeReq)

		require.Equal(t, promResp.Data.ResultType, loghttp.ResultTypeMatrix)
		require.Equal(t, 1, len(promResp.Data.Result))
		require.Equal(t, 2, len(promResp.Data.Result[0].Samples))
	})

	t.Run("timestamps are aligned with the end of steps", func(t *testing.T) {
		volumeReq := &logproto.VolumeRequest{
			From:        1000000000000,
			Through:     1000000005000, // 5s range
			Matchers:    `{foo="bar"}`,
			Limit:       seriesvolume.DefaultLimit,
			Step:        1000, // 1s
			AggregateBy: seriesvolume.Series,
		}
		promResp := makeVolumeRequest(volumeReq)

		require.Equal(t, int64(1000000000999),
			promResp.Data.Result[0].Samples[0].TimestampMs,
			"first timestamp should be one millisecond before the end of the first step")
		require.Equal(t,
			int64(1000000005000),
			promResp.Data.Result[0].Samples[4].TimestampMs,
			"last timestamp should be equal to the end of the requested query range")
	})
}
