package queryrange

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/storage/chunk"
)

var (
	nilShardingMetrics = logql.NewShardingMetrics(nil)
	defaultReq         = func() *LokiRequest {
		return &LokiRequest{
			Limit:     100,
			StartTs:   start,
			EndTs:     end,
			Direction: logproto.BACKWARD,
			Path:      "/loki/api/v1/query_range",
		}
	}
	lokiResps = []queryrange.Response{
		&LokiResponse{
			Status:    loghttp.QueryStatusSuccess,
			Direction: logproto.BACKWARD,
			Limit:     defaultReq().Limit,
			Version:   1,
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
	req := defaultReq().WithStartEnd(
		util.TimeToMillis(start),
		util.TimeToMillis(end),
	)

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
				shardingware: queryrange.HandlerFunc(func(ctx context.Context, req queryrange.Request) (queryrange.Response, error) {
					didShard = true
					return mockHandler(lokiResps[0], nil).Do(ctx, req)
				}),
				next: mockHandler(lokiResps[1], nil),
				now:  func() time.Time { return end },
				limits: fakeLimits{
					minShardingLookback: tc.lookback,
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

	handler := queryrange.HandlerFunc(func(ctx context.Context, req queryrange.Request) (queryrange.Response, error) {
		lock.Lock()
		defer lock.Unlock()
		resp := lokiResps[called]
		called++
		return resp, nil
	})

	mware := newASTMapperware(
		ShardingConfigs{
			chunk.PeriodConfig{
				RowShards: 2,
			},
		},
		handler,
		log.NewNopLogger(),
		nilShardingMetrics,
		fakeLimits{maxSeries: math.MaxInt32},
	)

	resp, err := mware.Do(context.Background(), defaultReq().WithQuery(`{food="bar"}`))
	require.Nil(t, err)

	expected, err := LokiCodec.MergeResponse(lokiResps...)
	sort.Sort(logproto.Streams(expected.(*LokiResponse).Data.Result))
	require.Nil(t, err)
	require.Equal(t, called, 2)
	require.Equal(t, expected.(*LokiResponse).Data, resp.(*LokiResponse).Data)
}

func Test_ShardingByPass(t *testing.T) {
	called := 0
	handler := queryrange.HandlerFunc(func(ctx context.Context, req queryrange.Request) (queryrange.Response, error) {
		called++
		return nil, nil
	})

	mware := newASTMapperware(
		ShardingConfigs{
			chunk.PeriodConfig{
				RowShards: 2,
			},
		},
		handler,
		log.NewNopLogger(),
		nilShardingMetrics,
		fakeLimits{maxSeries: math.MaxInt32},
	)

	_, err := mware.Do(context.Background(), defaultReq().WithQuery(`1+1`))
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

func mockHandler(resp queryrange.Response, err error) queryrange.Handler {
	return queryrange.HandlerFunc(func(ctx context.Context, req queryrange.Request) (queryrange.Response, error) {
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

	sharding := NewQueryShardMiddleware(log.NewNopLogger(), ShardingConfigs{
		chunk.PeriodConfig{
			RowShards: 3,
		},
	}, queryrange.NewInstrumentMiddlewareMetrics(nil),
		nilShardingMetrics,
		fakeLimits{
			maxSeries:           math.MaxInt32,
			maxQueryParallelism: 10,
		})
	response, err := sharding.Wrap(queryrange.HandlerFunc(func(c context.Context, r queryrange.Request) (queryrange.Response, error) {
		lock.Lock()
		defer lock.Unlock()
		called++
		shards = append(shards, r.(*LokiInstantRequest).Shards...)
		return &LokiPromResponse{Response: &queryrange.PrometheusResponse{
			Data: queryrange.PrometheusData{
				ResultType: loghttp.ResultTypeVector,
				Result: []queryrange.SampleStream{
					{
						Labels:  []cortexpb.LabelAdapter{{Name: "foo", Value: "bar"}},
						Samples: []cortexpb.Sample{{Value: 10, TimestampMs: 10}},
					},
				},
			},
		}}, nil
	})).Do(ctx, &LokiInstantRequest{
		Query:  `rate({app="foo"}[1m])`,
		TimeTs: util.TimeFromMillis(10),
		Path:   "/v1/query",
	})
	require.NoError(t, err)
	require.Equal(t, 3, called, "expected 3 calls but got {}", called)
	require.Len(t, response.(*LokiPromResponse).Response.Data.Result, 3)
	require.ElementsMatch(t, []string{"0_of_3", "1_of_3", "2_of_3"}, shards)
	require.Equal(t, queryrange.PrometheusData{
		ResultType: loghttp.ResultTypeVector,
		Result: []queryrange.SampleStream{
			{
				Labels:  []cortexpb.LabelAdapter{{Name: "foo", Value: "bar"}},
				Samples: []cortexpb.Sample{{Value: 10, TimestampMs: 10}},
			},
			{
				Labels:  []cortexpb.LabelAdapter{{Name: "foo", Value: "bar"}},
				Samples: []cortexpb.Sample{{Value: 10, TimestampMs: 10}},
			},
			{
				Labels:  []cortexpb.LabelAdapter{{Name: "foo", Value: "bar"}},
				Samples: []cortexpb.Sample{{Value: 10, TimestampMs: 10}},
			},
		},
	}, response.(*LokiPromResponse).Response.Data)
	require.Equal(t, loghttp.QueryStatusSuccess, response.(*LokiPromResponse).Response.Status)
}

func Test_SeriesShardingHandler(t *testing.T) {
	sharding := NewSeriesQueryShardMiddleware(log.NewNopLogger(), ShardingConfigs{
		chunk.PeriodConfig{
			RowShards: 3,
		},
	},
		queryrange.NewInstrumentMiddlewareMetrics(nil),
		nilShardingMetrics,
		fakeLimits{
			maxQueryParallelism: 10,
		},
		LokiCodec,
	)
	ctx := user.InjectOrgID(context.Background(), "1")

	response, err := sharding.Wrap(queryrange.HandlerFunc(func(c context.Context, r queryrange.Request) (queryrange.Response, error) {
		req, ok := r.(*LokiSeriesRequest)
		if !ok {
			return nil, errors.New("not a series call")
		}
		return &LokiSeriesResponse{
			Status:  "success",
			Version: 1,
			Data: []logproto.SeriesIdentifier{
				{
					Labels: map[string]string{
						"foo": "bar",
					},
				},
				{
					Labels: map[string]string{
						"shard": req.Shards[0],
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
		Status:  "success",
		Version: 1,
		Data: []logproto.SeriesIdentifier{
			{
				Labels: map[string]string{
					"foo": "bar",
				},
			},
			{
				Labels: map[string]string{
					"shard": "0_of_3",
				},
			},
			{
				Labels: map[string]string{
					"shard": "1_of_3",
				},
			},
			{
				Labels: map[string]string{
					"shard": "2_of_3",
				},
			},
		},
	}
	sort.Slice(expected.Data, func(i, j int) bool {
		return expected.Data[i].Labels["shard"] > expected.Data[j].Labels["shard"]
	})
	actual := response.(*LokiSeriesResponse)
	sort.Slice(actual.Data, func(i, j int) bool {
		return actual.Data[i].Labels["shard"] > actual.Data[j].Labels["shard"]
	})
	require.NoError(t, err)
	require.Equal(t, expected, actual)
}
