package queryrange

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/pkg/push"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/loghttp"
	loghttp_push "github.com/grafana/loki/v3/pkg/loghttp/push"
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

func Test_aggregatedMetricQuery(t *testing.T) {
	now := time.Now()
	serviceMatcher, err := labels.NewMatcher(
		labels.MatchEqual,
		loghttp_push.LabelServiceName,
		"foo",
	)
	require.NoError(t, err)
	fruitMatcher, err := labels.NewMatcher(labels.MatchNotEqual, "fruit", "apple")
	require.NoError(t, err)
	colorMatcher, err := labels.NewMatcher(labels.MatchRegexp, "color", "green")
	require.NoError(t, err)

	t.Run("it uses service name if present in original matcher", func(t *testing.T) {
		query := &aggregatedMetricQuery{
			matchers: []*labels.Matcher{serviceMatcher},
			start:    now.Add(-1 * time.Hour),
			end:      now,
		}

		expected := fmt.Sprintf(
			`sum by (service_name) (sum_over_time({%s="foo"} | logfmt | unwrap bytes(bytes) | __error__=""[1h0m0s]))`,
			loghttp_push.AggregatedMetricLabel,
		)
		require.Equal(t, expected, query.BuildQuery())
	})

	t.Run("it includes additional matchers as line filters", func(t *testing.T) {
		query := &aggregatedMetricQuery{
			matchers: []*labels.Matcher{serviceMatcher, fruitMatcher},
			start:    now.Add(-30 * time.Minute),
			end:      now,
		}

		expected := fmt.Sprintf(
			`sum by (service_name) (sum_over_time({%s="foo"} | logfmt | fruit!="apple" | unwrap bytes(bytes) | __error__=""[30m0s]))`,
			loghttp_push.AggregatedMetricLabel,
		)
		require.Equal(t, expected, query.BuildQuery())
	})

	t.Run("preserves the matcher type for service name", func(t *testing.T) {
		serviceMatcher, err := labels.NewMatcher(
			labels.MatchRegexp,
			loghttp_push.LabelServiceName,
			"foo",
		)
		require.NoError(t, err)
		query := &aggregatedMetricQuery{
			matchers: []*labels.Matcher{serviceMatcher},
			start:    now.Add(-1 * time.Hour),
			end:      now,
		}

		expected := fmt.Sprintf(
			`sum by (service_name) (sum_over_time({%s=~"foo"} | logfmt | unwrap bytes(bytes) | __error__=""[1h0m0s]))`,
			loghttp_push.AggregatedMetricLabel,
		)
		require.Equal(t, expected, query.BuildQuery())
	})

	t.Run("preserves the matcher type for additional matchers", func(t *testing.T) {
		query := &aggregatedMetricQuery{
			matchers: []*labels.Matcher{serviceMatcher, fruitMatcher, colorMatcher},
			start:    now.Add(-5 * time.Minute),
			end:      now,
		}

		expected := fmt.Sprintf(
			`sum by (service_name) (sum_over_time({%s="foo"} | logfmt | fruit!="apple" | color=~"green" | unwrap bytes(bytes) | __error__=""[5m0s]))`,
			loghttp_push.AggregatedMetricLabel,
		)
		require.Equal(t, expected, query.BuildQuery())
	})

	t.Run(
		"if service_name is not present, it uses a wildcard matcher and aggregated by the first matcher",
		func(t *testing.T) {
			query := &aggregatedMetricQuery{
				matchers: []*labels.Matcher{fruitMatcher, colorMatcher},
				start:    now.Add(-30 * time.Minute).Add(-5 * time.Second),
				end:      now,
			}

			expected := fmt.Sprintf(
				`sum by (fruit) (sum_over_time({%s=~".+"} | logfmt | fruit!="apple" | color=~"green" | unwrap bytes(bytes) | __error__=""[30m5s]))`,
				loghttp_push.AggregatedMetricLabel,
			)
			require.Equal(t, expected, query.BuildQuery())
		},
	)

	t.Run("it aggregates by aggregate_by when provided", func(t *testing.T) {
		query := &aggregatedMetricQuery{
			matchers:    []*labels.Matcher{fruitMatcher, colorMatcher},
			start:       now.Add(-30 * time.Minute),
			end:         now,
			aggregateBy: "color,fruit",
		}

		expected := fmt.Sprintf(
			`sum by (color,fruit) (sum_over_time({%s=~".+"} | logfmt | fruit!="apple" | color=~"green" | unwrap bytes(bytes) | __error__=""[30m0s]))`,
			loghttp_push.AggregatedMetricLabel,
		)
		require.Equal(t, expected, query.BuildQuery())

		query = &aggregatedMetricQuery{
			matchers:    []*labels.Matcher{serviceMatcher},
			start:       now.Add(-5 * time.Second),
			end:         now,
			aggregateBy: "fruit",
		}

		expected = fmt.Sprintf(
			`sum by (fruit) (sum_over_time({%s="foo"} | logfmt | unwrap bytes(bytes) | __error__=""[5s]))`,
			loghttp_push.AggregatedMetricLabel,
		)
		require.Equal(t, expected, query.BuildQuery())
	})
}
