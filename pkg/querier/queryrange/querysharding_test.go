package queryrange

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
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
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var didShard bool

			splitter := &shardSplitter{
				shardingware: queryrange.HandlerFunc(func(ctx context.Context, req queryrange.Request) (queryrange.Response, error) {
					didShard = true
					return mockHandler(lokiResps[0], nil).Do(ctx, req)
				}),
				next:                mockHandler(lokiResps[1], nil),
				now:                 func() time.Time { return end },
				MinShardingLookback: tc.lookback,
			}

			resp, err := splitter.Do(context.Background(), req)
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
		queryrange.ShardingConfigs{
			chunk.PeriodConfig{
				RowShards: 2,
			},
		},
		handler,
		log.NewNopLogger(),
		nilShardingMetrics,
	)

	resp, err := mware.Do(context.Background(), defaultReq().WithQuery(`{food="bar"}`))
	require.Nil(t, err)

	expected, err := lokiCodec.MergeResponse(lokiResps...)
	sort.Sort(logproto.Streams(expected.(*LokiResponse).Data.Result))
	require.Nil(t, err)
	require.Equal(t, called, 2)
	require.Equal(t, expected.(*LokiResponse).Data, resp.(*LokiResponse).Data)

}

func Test_hasShards(t *testing.T) {
	for i, tc := range []struct {
		input    queryrange.ShardingConfigs
		expected bool
	}{
		{
			input: queryrange.ShardingConfigs{
				{},
			},
			expected: false,
		},
		{
			input: queryrange.ShardingConfigs{
				{RowShards: 16},
			},
			expected: true,
		},
		{
			input: queryrange.ShardingConfigs{
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
