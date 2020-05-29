package queryrange

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
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

func Test_PartitionRequest(t *testing.T) {
	midpt := time.Unix(0, 0).Add(500 * time.Millisecond)
	cutoff := TimeToMillis(midpt)

	// test split
	req := defaultReq().WithStartEnd(0, cutoff*2)
	before, after := partitionRequest(req, midpt)
	require.Equal(t, req.WithStartEnd(0, cutoff), before)
	require.Equal(t, req.WithStartEnd(cutoff, 2*cutoff), after)

	// test all before cutoff
	before, after = partitionRequest(req, midpt.Add(1000*time.Millisecond))
	require.Equal(t, req, before)
	require.Nil(t, after)

	// test after cutoff
	before, after = partitionRequest(req, time.Unix(0, 0))
	require.Nil(t, before)
	require.Equal(t, req, after)

}

func Test_shardSplitter(t *testing.T) {
	splitter := &shardSplitter{
		shardingware:        mockHandler(lokiResps[0], nil),
		next:                mockHandler(lokiResps[1], nil),
		now:                 time.Now,
		MinShardingLookback: 0,
	}

	req := defaultReq().WithStartEnd(
		TimeToMillis(time.Now().Add(-time.Hour)),
		TimeToMillis(time.Now().Add(time.Hour)),
	)

	resp, err := splitter.Do(context.Background(), req)
	require.Nil(t, err)
	expected, err := lokiCodec.MergeResponse(lokiResps...)
	require.Nil(t, err)
	require.Equal(t, expected, resp)
}

func Test_astMapper(t *testing.T) {
	called := 0

	handler := queryrange.HandlerFunc(func(ctx context.Context, req queryrange.Request) (queryrange.Response, error) {
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
