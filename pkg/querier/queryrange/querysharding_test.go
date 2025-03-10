package queryrange

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase/definitions"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

var (
	nilShardingMetrics = logql.NewShardMapperMetrics(nil)
	defaultReq         = func() *LokiRequest {
		return &LokiRequest{
			Step:      1000,
			Limit:     100,
			StartTs:   start,
			EndTs:     end,
			Direction: logproto.BACKWARD,
			Path:      "/loki/api/v1/query_range",
		}
	}
	lokiResps = []queryrangebase.Response{
		&LokiResponse{
			Status:    loghttp.QueryStatusSuccess,
			Direction: logproto.BACKWARD,
			Limit:     defaultReq().Limit,
			Version:   1,
			Headers: []definitions.PrometheusResponseHeader{
				{Name: "Header", Values: []string{"value"}},
			},
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result: []logproto.Stream{
					{
						Labels: `{foo="bar", level="debug"}`,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(0, 6), Line: "6"},
							{Timestamp: time.Unix(0, 5), Line: "5"},
						},
					},
				},
			},
		},
		&LokiResponse{
			Status:    loghttp.QueryStatusSuccess,
			Direction: logproto.BACKWARD,
			Limit:     100,
			Version:   1,
			Headers: []definitions.PrometheusResponseHeader{
				{Name: "Header", Values: []string{"value"}},
			},
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result: []logproto.Stream{
					{
						Labels: `{foo="bar", level="error"}`,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(0, 2), Line: "2"},
							{Timestamp: time.Unix(0, 1), Line: "1"},
						},
					},
				},
			},
		},
	}
)

func Test_shardSplitter(t *testing.T) {
	req := defaultReq().WithStartEnd(start, end)

	for _, tc := range []struct {
		desc        string
		lookback    time.Duration
		shouldShard bool
	}{
		{
			desc:        "older than lookback",
			lookback:    -time.Minute, // a negative lookback will ensure the entire query doesn't cross the sharding boundary & can safely be sharded.
			shouldShard: true,
		},
		{
			desc:        "overlaps lookback",
			lookback:    end.Sub(start) / 2, // intersect the request causing it to avoid sharding
			shouldShard: false,
		},
		{
			desc:        "newer than lookback",
			lookback:    end.Sub(start) + 1, // the entire query is in the ingester range and should avoid sharding.
			shouldShard: false,
		},
		{
			desc:        "default",
			lookback:    0,
			shouldShard: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var didShard bool
			splitter := &shardSplitter{
				shardingware: queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
					didShard = true
					return mockHandler(lokiResps[0], nil).Do(ctx, req)
				}),
				next: mockHandler(lokiResps[1], nil),
				now:  func() time.Time { return end },
				limits: fakeLimits{
					minShardingLookback: tc.lookback,
					queryTimeout:        time.Minute,
					maxQueryParallelism: 1,
				},
			}

			resp, err := splitter.Do(user.InjectOrgID(context.Background(), "1"), req)
			require.Nil(t, err)

			require.Equal(t, tc.shouldShard, didShard)
			require.Nil(t, err)

			if tc.shouldShard {
				require.Equal(t, lokiResps[0], resp)
			} else {
				require.Equal(t, lokiResps[1], resp)
			}
		})
	}
}

func Test_astMapper(t *testing.T) {
	var lock sync.Mutex
	called := 0

	handler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		lock.Lock()
		defer lock.Unlock()
		resp := lokiResps[called]
		called++
		return resp, nil
	})

	mware := newASTMapperware(
		ShardingConfigs{
			config.PeriodConfig{
				RowShards: 2,
			},
		},
		testEngineOpts,
		handler,
		handler,
		nil,
		log.NewNopLogger(),
		nilShardingMetrics,
		fakeLimits{maxSeries: math.MaxInt32, maxQueryParallelism: 1, queryTimeout: time.Second},
		0,
		[]string{},
	)

	req := defaultReq()
	req.Query = `{foo="bar"}`
	req.Plan = &plan.QueryPlan{
		AST: syntax.MustParseExpr(req.Query),
	}
	resp, err := mware.Do(user.InjectOrgID(context.Background(), "1"), req)
	require.Nil(t, err)

	require.Equal(t, []*definitions.PrometheusResponseHeader{
		{Name: "Header", Values: []string{"value"}},
	}, resp.GetHeaders())

	expected, err := DefaultCodec.MergeResponse(lokiResps...)
	sort.Sort(logproto.Streams(expected.(*LokiResponse).Data.Result))
	require.Nil(t, err)
	require.Equal(t, called, 2)
	require.Equal(t, expected.(*LokiResponse).Data, resp.(*LokiResponse).Data)
}

func Test_astMapper_QuerySizeLimits(t *testing.T) {
	noErr := ""
	for _, tc := range []struct {
		desc                string
		query               string
		maxQuerierBytesSize int

		err                      string
		expectedStatsHandlerHits int
	}{
		{
			desc:                "Non shardable query",
			query:               `sum_over_time({app="foo"} |= "foo" | unwrap foo [1h])`,
			maxQuerierBytesSize: 100,

			err:                      noErr,
			expectedStatsHandlerHits: 1,
		},
		{
			desc:                     "Non shardable query too big",
			query:                    `avg_over_time({job="foo"} | json busy="utilization" | unwrap busy [5m])`,
			maxQuerierBytesSize:      10,
			err:                      fmt.Sprintf(limErrQuerierTooManyBytesUnshardableTmpl, "100 B", "10 B"),
			expectedStatsHandlerHits: 1,
		},
		{
			desc:                "Shardable query",
			query:               `count_over_time({app="foo"} |= "foo" [1h])`,
			maxQuerierBytesSize: 100,

			err:                      noErr,
			expectedStatsHandlerHits: 1,
		},
		{
			desc:                "Shardable query too big",
			query:               `count_over_time({app="foo"} |= "foo" [1h])`,
			maxQuerierBytesSize: 10,

			err:                      fmt.Sprintf(limErrQuerierTooManyBytesShardableTmpl, "100 B", "10 B"),
			expectedStatsHandlerHits: 1,
		},
		{
			desc:                "Partially Shardable query fitting",
			query:               `count_over_time({app="foo"} |= "foo" [1h]) - sum_over_time({app="foo"} |= "foo" | unwrap foo [1h])`,
			maxQuerierBytesSize: 100,

			err:                      noErr,
			expectedStatsHandlerHits: 2,
		},
		{
			desc:                "Partially Shardable LHS too big",
			query:               `count_over_time({app="bar"} |= "bar" [1h]) - sum_over_time({app="foo"} |= "foo" | unwrap foo [1h])`,
			maxQuerierBytesSize: 100,

			err:                      fmt.Sprintf(limErrQuerierTooManyBytesShardableTmpl, "500 B", "100 B"),
			expectedStatsHandlerHits: 2,
		},
		{
			desc:                "Partially Shardable RHS too big",
			query:               `count_over_time({app="foo"} |= "foo" [1h]) - sum_over_time({app="bar"} |= "bar" | unwrap foo [1h])`,
			maxQuerierBytesSize: 100,

			err:                      fmt.Sprintf(limErrQuerierTooManyBytesShardableTmpl, "500 B", "100 B"),
			expectedStatsHandlerHits: 2,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			statsCalled := 0
			handler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				if casted, ok := req.(*logproto.IndexStatsRequest); ok {
					statsCalled++

					var bytes uint64
					if strings.Contains(casted.Matchers, `app="foo"`) {
						bytes = 100
					}
					if strings.Contains(casted.Matchers, `app="bar"`) {
						bytes = 500
					}

					return &IndexStatsResponse{
						Response: &logproto.IndexStatsResponse{
							Bytes: bytes,
						},
					}, nil
				}
				if _, ok := req.(*LokiRequest); ok {
					return &LokiPromResponse{Response: &queryrangebase.PrometheusResponse{
						Data: queryrangebase.PrometheusData{
							ResultType: loghttp.ResultTypeVector,
							Result: []queryrangebase.SampleStream{
								{
									Labels:  []logproto.LabelAdapter{{Name: "foo", Value: "bar"}},
									Samples: []logproto.LegacySample{{Value: 10, TimestampMs: 10}},
								},
							},
						},
					}}, nil
				}

				return nil, nil
			})

			mware := newASTMapperware(
				ShardingConfigs{
					config.PeriodConfig{
						RowShards: 2,
						IndexType: types.TSDBType,
					},
				},
				testEngineOpts,
				handler,
				handler,
				nil,
				log.NewNopLogger(),
				nilShardingMetrics,
				fakeLimits{
					maxSeries:               math.MaxInt32,
					maxQueryParallelism:     1,
					tsdbMaxQueryParallelism: 1,
					queryTimeout:            time.Minute,
					maxQuerierBytesRead:     tc.maxQuerierBytesSize,
				},
				0,
				[]string{},
			)

			req := defaultReq()
			req.Query = tc.query
			req.Plan = &plan.QueryPlan{
				AST: syntax.MustParseExpr(tc.query),
			}
			_, err := mware.Do(user.InjectOrgID(context.Background(), "1"), req)
			if err != nil {
				require.ErrorContains(t, err, tc.err)
			}

			require.Equal(t, tc.expectedStatsHandlerHits, statsCalled)
		})
	}
}

func Test_ShardingByPass(t *testing.T) {
	called := 0
	handler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		called++
		return nil, nil
	})

	mware := newASTMapperware(
		ShardingConfigs{
			config.PeriodConfig{
				RowShards: 2,
			},
		},
		testEngineOpts,
		handler,
		handler,
		nil,
		log.NewNopLogger(),
		nilShardingMetrics,
		fakeLimits{maxSeries: math.MaxInt32, maxQueryParallelism: 1},
		0,
		[]string{},
	)

	req := defaultReq()
	req.Query = `1+1`
	req.Plan = &plan.QueryPlan{
		AST: syntax.MustParseExpr(req.Query),
	}

	_, err := mware.Do(user.InjectOrgID(context.Background(), "1"), req)
	require.Nil(t, err)
	require.Equal(t, called, 1)
}

func Test_hasShards(t *testing.T) {
	for i, tc := range []struct {
		input    ShardingConfigs
		expected bool
	}{
		{
			input: ShardingConfigs{
				{},
			},
			expected: false,
		},
		{
			input: ShardingConfigs{
				{RowShards: 16},
			},
			expected: true,
		},
		{
			input: ShardingConfigs{
				{},
				{RowShards: 16},
				{},
			},
			expected: true,
		},
		{
			input:    nil,
			expected: false,
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			require.Equal(t, tc.expected, hasShards(tc.input))
		})
	}
}

// astmapper successful stream & prom conversion

func mockHandler(resp queryrangebase.Response, err error) queryrangebase.Handler {
	return queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		if expired := ctx.Err(); expired != nil {
			return nil, expired
		}

		return resp, err
	})
}

func Test_InstantSharding(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")

	var lock sync.Mutex
	called := 0
	shards := []string{}

	cpyPeriodConf := testSchemas[0]
	cpyPeriodConf.RowShards = 3
	sharding := NewQueryShardMiddleware(log.NewNopLogger(), ShardingConfigs{
		cpyPeriodConf,
	}, testEngineOpts, queryrangebase.NewInstrumentMiddlewareMetrics(nil, constants.Loki),
		nilShardingMetrics,
		fakeLimits{
			maxSeries:           math.MaxInt32,
			maxQueryParallelism: 10,
			queryTimeout:        time.Second,
		},
		0,
		nil,
		nil,
		[]string{},
	)
	response, err := sharding.Wrap(queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		lock.Lock()
		defer lock.Unlock()
		called++
		shards = append(shards, r.(*LokiInstantRequest).Shards...)
		return &LokiPromResponse{Response: &queryrangebase.PrometheusResponse{
			Data: queryrangebase.PrometheusData{
				ResultType: loghttp.ResultTypeVector,
				Result: []queryrangebase.SampleStream{
					{
						Labels:  []logproto.LabelAdapter{{Name: "foo", Value: "bar"}},
						Samples: []logproto.LegacySample{{Value: 10, TimestampMs: 10}},
					},
				},
			},
		}}, nil
	})).Do(ctx, &LokiInstantRequest{
		Query:  `rate({app="foo"}[1m])`,
		TimeTs: util.TimeFromMillis(10),
		Path:   "/v1/query",
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(`rate({app="foo"}[1m])`),
		},
	})
	require.NoError(t, err)
	require.Equal(t, 3, called, "expected 3 calls but got {}", called)
	require.Len(t, response.(*LokiPromResponse).Response.Data.Result, 3)
	require.ElementsMatch(t, []string{"0_of_3", "1_of_3", "2_of_3"}, shards)
	require.Equal(t, queryrangebase.PrometheusData{
		ResultType: loghttp.ResultTypeVector,
		Result: []queryrangebase.SampleStream{
			{
				Labels:  []logproto.LabelAdapter{{Name: "foo", Value: "bar"}},
				Samples: []logproto.LegacySample{{Value: 10, TimestampMs: 10}},
			},
			{
				Labels:  []logproto.LabelAdapter{{Name: "foo", Value: "bar"}},
				Samples: []logproto.LegacySample{{Value: 10, TimestampMs: 10}},
			},
			{
				Labels:  []logproto.LabelAdapter{{Name: "foo", Value: "bar"}},
				Samples: []logproto.LegacySample{{Value: 10, TimestampMs: 10}},
			},
		},
	}, response.(*LokiPromResponse).Response.Data)
	require.Equal(t, loghttp.QueryStatusSuccess, response.(*LokiPromResponse).Response.Status)
}

func Test_SeriesShardingHandler(t *testing.T) {
	sharding := NewSeriesQueryShardMiddleware(log.NewNopLogger(), ShardingConfigs{
		config.PeriodConfig{
			RowShards: 3,
		},
	},
		queryrangebase.NewInstrumentMiddlewareMetrics(nil, constants.Loki),
		nilShardingMetrics,
		fakeLimits{
			maxQueryParallelism: 10,
		},
		DefaultCodec,
	)
	ctx := user.InjectOrgID(context.Background(), "1")

	response, err := sharding.Wrap(queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		req, ok := r.(*LokiSeriesRequest)
		if !ok {
			return nil, errors.New("not a series call")
		}
		return &LokiSeriesResponse{
			Status:  "success",
			Version: 1,
			Data: []logproto.SeriesIdentifier{
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "foo", Value: "bar"},
					},
				},
				{
					Labels: []logproto.SeriesIdentifier_LabelsEntry{
						{Key: "shard", Value: req.Shards[0]},
					},
				},
			},
		}, nil
	})).Do(ctx, &LokiSeriesRequest{
		Match:   []string{"foo", "bar"},
		StartTs: time.Unix(0, 1),
		EndTs:   time.Unix(0, 10),
		Path:    "foo",
	})

	expected := &LokiSeriesResponse{
		Statistics: stats.Result{Summary: stats.Summary{Splits: 3}},
		Status:     "success",
		Version:    1,
		Data: []logproto.SeriesIdentifier{
			{
				Labels: []logproto.SeriesIdentifier_LabelsEntry{
					{Key: "foo", Value: "bar"},
				},
			},
			{
				Labels: []logproto.SeriesIdentifier_LabelsEntry{
					{Key: "shard", Value: "0_of_3"},
				},
			},
			{
				Labels: []logproto.SeriesIdentifier_LabelsEntry{
					{Key: "shard", Value: "1_of_3"},
				},
			},
			{
				Labels: []logproto.SeriesIdentifier_LabelsEntry{
					{Key: "shard", Value: "2_of_3"},
				},
			},
		},
	}
	actual := response.(*LokiSeriesResponse)
	require.NoError(t, err)
	require.Equal(t, expected.Status, actual.Status)
	require.ElementsMatch(t, expected.Data, actual.Data)
}

func TestShardingAcrossConfigs_ASTMapper(t *testing.T) {
	now := model.Now()
	confs := ShardingConfigs{
		{
			From:      config.DayTime{Time: now.Add(-30 * 24 * time.Hour)},
			RowShards: 2,
		},
		{
			From:      config.DayTime{Time: now.Add(-24 * time.Hour)},
			RowShards: 3,
		},
	}

	for _, tc := range []struct {
		name              string
		req               queryrangebase.Request
		resp              queryrangebase.Response
		numExpectedShards int
	}{
		{
			name: "logs query touching just the active schema config",
			req:  defaultReq().WithStartEnd(now.Add(-time.Hour).Time(), now.Time()).WithQuery(`{foo="bar"}`),
			resp: &LokiResponse{
				Status: loghttp.QueryStatusSuccess,
				Headers: []definitions.PrometheusResponseHeader{
					{Name: "Header", Values: []string{"value"}},
				},
			},
			numExpectedShards: 3,
		},
		{
			name: "logs query touching just the prev schema config",
			req:  defaultReq().WithStartEnd(confs[0].From.Time.Time(), confs[0].From.Time.Add(time.Hour).Time()).WithQuery(`{foo="bar"}`),
			resp: &LokiResponse{
				Status: loghttp.QueryStatusSuccess,
				Headers: []definitions.PrometheusResponseHeader{
					{Name: "Header", Values: []string{"value"}},
				},
			},
			numExpectedShards: 2,
		},
		{
			name: "metric query touching just the active schema config",
			req:  defaultReq().WithStartEnd(confs[1].From.Time.Add(5*time.Minute).Time(), confs[1].From.Time.Add(time.Hour).Time()).WithQuery(`rate({foo="bar"}[1m])`),
			resp: &LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Status: loghttp.QueryStatusSuccess,
					Data: queryrangebase.PrometheusData{
						ResultType: "",
						Result:     []queryrangebase.SampleStream{},
					},
					Headers: []*definitions.PrometheusResponseHeader{
						{Name: "Header", Values: []string{"value"}},
					},
				},
			},
			numExpectedShards: 3,
		},
		{
			name: "metric query touching just the prev schema config",
			req:  defaultReq().WithStartEnd(confs[0].From.Time.Add(time.Hour).Time(), confs[0].From.Time.Add(2*time.Hour).Time()).WithQuery(`rate({foo="bar"}[1m])`),
			resp: &LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Status: loghttp.QueryStatusSuccess,
					Data: queryrangebase.PrometheusData{
						ResultType: "",
						Result:     []queryrangebase.SampleStream{},
					},
					Headers: []*definitions.PrometheusResponseHeader{
						{Name: "Header", Values: []string{"value"}},
					},
				},
			},
			numExpectedShards: 2,
		},
		{
			name: "logs query covering both schemas",
			req:  defaultReq().WithStartEnd(confs[0].From.Time.Time(), now.Time()).WithQuery(`{foo="bar"}`),
			resp: &LokiResponse{
				Status: loghttp.QueryStatusSuccess,
				Headers: []definitions.PrometheusResponseHeader{
					{Name: "Header", Values: []string{"value"}},
				},
			},
			numExpectedShards: 1,
		},
		{
			name: "metric query covering both schemas",
			req:  defaultReq().WithStartEnd(confs[0].From.Time.Time(), now.Time()).WithQuery(`rate({foo="bar"}[1m])`),
			resp: &LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Status: loghttp.QueryStatusSuccess,
					Data: queryrangebase.PrometheusData{
						ResultType: "",
						Result:     []queryrangebase.SampleStream{},
					},
					Headers: []*definitions.PrometheusResponseHeader{
						{Name: "Header", Values: []string{"value"}},
					},
				},
			},
			numExpectedShards: 1,
		},
		{
			name: "metric query with start/end within first schema but with large enough range to cover previous schema too",
			req:  defaultReq().WithStartEnd(confs[1].From.Time.Add(5*time.Minute).Time(), confs[1].From.Time.Add(time.Hour).Time()).WithQuery(`rate({foo="bar"}[24h])`),
			resp: &LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Status: loghttp.QueryStatusSuccess,
					Data: queryrangebase.PrometheusData{
						ResultType: "",
						Result:     []queryrangebase.SampleStream{},
					},
					Headers: []*definitions.PrometheusResponseHeader{
						{Name: "Header", Values: []string{"value"}},
					},
				},
			},
			numExpectedShards: 1,
		},
		{
			name: "metric query with start/end within first schema but with large enough offset to shift it to previous schema",
			req:  defaultReq().WithStartEnd(confs[1].From.Time.Add(5*time.Minute).Time(), now.Time()).WithQuery(`rate({foo="bar"}[1m] offset 12h)`),
			resp: &LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Status: loghttp.QueryStatusSuccess,
					Data: queryrangebase.PrometheusData{
						ResultType: "",
						Result:     []queryrangebase.SampleStream{},
					},
					Headers: []*definitions.PrometheusResponseHeader{
						{Name: "Header", Values: []string{"value"}},
					},
				},
			},
			numExpectedShards: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var lock sync.Mutex
			called := 0

			handler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				lock.Lock()
				defer lock.Unlock()
				called++
				return tc.resp, nil
			})

			mware := newASTMapperware(
				confs,
				testEngineOpts,
				handler,
				handler,
				nil,
				log.NewNopLogger(),
				nilShardingMetrics,
				fakeLimits{maxSeries: math.MaxInt32, maxQueryParallelism: 1, queryTimeout: time.Second},
				0,
				[]string{},
			)

			// currently all the tests call `defaultReq()` which creates an instance of the type LokiRequest
			// if in the future that isn't true, we need another way to access the Plan field of an arbitrary query type
			// or we should set the Plan in calls to `GetExpression` if the Plan is nil by calling `ParseExpr` or similar
			tc.req.(*LokiRequest).Plan = &plan.QueryPlan{
				AST: syntax.MustParseExpr(tc.req.GetQuery()),
			}

			resp, err := mware.Do(user.InjectOrgID(context.Background(), "1"), tc.req)
			require.Nil(t, err)

			require.Equal(t, []*definitions.PrometheusResponseHeader{
				{Name: "Header", Values: []string{"value"}},
			}, resp.GetHeaders())

			require.Equal(t, tc.numExpectedShards, called)
		})
	}
}

func TestShardingAcrossConfigs_SeriesSharding(t *testing.T) {
	now := model.Now()
	confs := ShardingConfigs{
		{
			From:      config.DayTime{Time: now.Add(-30 * 24 * time.Hour)},
			RowShards: 2,
		},
		{
			From:      config.DayTime{Time: now.Add(-24 * time.Hour)},
			RowShards: 3,
		},
	}

	for _, tc := range []struct {
		name              string
		req               *LokiSeriesRequest
		numExpectedShards int
	}{
		{
			name: "series query touching just the active schema config",
			req: &LokiSeriesRequest{
				Match:   []string{"foo", "bar"},
				StartTs: confs[1].From.Time.Add(5 * time.Minute).Time(),
				EndTs:   now.Time(),
				Path:    "foo",
			},
			numExpectedShards: 3,
		},
		{
			name: "series query touching just the prev schema config",
			req: &LokiSeriesRequest{
				Match:   []string{"foo", "bar"},
				StartTs: confs[0].From.Time.Time(),
				EndTs:   confs[0].From.Time.Add(time.Hour).Time(),
				Path:    "foo",
			},
			numExpectedShards: 2,
		},
		{
			name: "series query covering both schemas",
			req: &LokiSeriesRequest{
				Match:   []string{"foo", "bar"},
				StartTs: confs[0].From.Time.Time(),
				EndTs:   now.Time(),
				Path:    "foo",
			}, numExpectedShards: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := user.InjectOrgID(context.Background(), "1")
			var lock sync.Mutex
			called := 0

			mware := NewSeriesQueryShardMiddleware(
				log.NewNopLogger(),
				confs,
				queryrangebase.NewInstrumentMiddlewareMetrics(nil, constants.Loki),
				nilShardingMetrics,
				fakeLimits{
					maxQueryParallelism: 10,
				},
				DefaultCodec,
			)

			_, err := mware.Wrap(queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
				_, ok := r.(*LokiSeriesRequest)
				if !ok {
					return nil, errors.New("not a series call")
				}
				lock.Lock()
				defer lock.Unlock()
				called++
				return &LokiSeriesResponse{
					Status:  "success",
					Version: 1,
					Data:    []logproto.SeriesIdentifier{},
				}, nil
			})).Do(ctx, tc.req)
			require.NoError(t, err)

			require.Equal(t, tc.numExpectedShards, called)
		})
	}
}

func Test_ASTMapper_MaxLookBackPeriod(t *testing.T) {
	engineOpts := testEngineOpts
	engineOpts.MaxLookBackPeriod = 1 * time.Hour

	queryHandler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return &LokiResponse{}, nil
	})

	statsHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		// This is the actual check that we're testing.
		require.Equal(t, testTime.Add(-engineOpts.MaxLookBackPeriod).UnixMilli(), req.GetStart().UnixMilli())

		return &IndexStatsResponse{
			Response: &logproto.IndexStatsResponse{
				Bytes: 1 << 10,
			},
		}, nil
	})

	mware := newASTMapperware(
		testSchemasTSDB,
		engineOpts,
		queryHandler,
		queryHandler,
		statsHandler,
		log.NewNopLogger(),
		nilShardingMetrics,
		fakeLimits{maxSeries: math.MaxInt32, tsdbMaxQueryParallelism: 1, queryTimeout: time.Second},
		0,
		[]string{},
	)

	q := `{cluster="dev-us-central-0"}`
	lokiReq := &LokiInstantRequest{
		Query:     q,
		Limit:     1000,
		TimeTs:    testTime,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query",
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(q),
		},
	}

	ctx := user.InjectOrgID(context.Background(), "foo")
	_, err := mware.Do(ctx, lokiReq)
	require.NoError(t, err)
}

func Test_ConstantShardingDefaultIndexType(t *testing.T) {
	engineOpts := testEngineOpts

	queryHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		req.(*LokiInstantRequest).Plan.AST = syntax.MustParseExpr(`{cluster="dev-us-central-0"}`)
		shards, _, err := logql.ParseShards(req.(*LokiInstantRequest).Shards)
		require.NoError(t, err)
		require.Equal(t, 1, len(shards))
		require.Equal(t, logql.PowerOfTwoVersion, shards[0].Variant())
		require.Equal(t, uint32(32), shards[0].PowerOfTwo.Of)
		return &LokiResponse{}, nil
	})

	statsHandler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		// This is the actual check that we're testing.
		require.Equal(t, testTime.Add(-engineOpts.MaxLookBackPeriod).UnixMilli(), req.GetStart().UnixMilli())

		return &IndexStatsResponse{
			Response: &logproto.IndexStatsResponse{
				Bytes: 1 << 10,
			},
		}, nil
	})
	mware := newASTMapperware(
		ShardingConfigs{
			{
				From:      config.DayTime{Time: model.Now().Add(-2 * 24 * time.Hour)},
				RowShards: 2,
				IndexType: "tsdb",
			},
			{
				From:      config.DayTime{Time: model.Now().Add(-1 * 24 * time.Hour)},
				RowShards: 32,
			},
		},
		engineOpts,
		queryHandler,
		queryHandler,
		statsHandler,
		log.NewNopLogger(),
		nilShardingMetrics,
		fakeLimits{maxSeries: math.MaxInt32, tsdbMaxQueryParallelism: 1, queryTimeout: time.Second},
		0,
		[]string{},
	)

	q := `{cluster="dev-us-central-0"}`
	lokiReq := &LokiInstantRequest{
		Query:     q,
		Limit:     1000,
		TimeTs:    model.Now().Add(-1 * time.Hour).Time(),
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query",
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(q),
		},
	}

	ctx := user.InjectOrgID(context.Background(), "foo")
	_, err := mware.Do(ctx, lokiReq)
	require.NoError(t, err)
}
