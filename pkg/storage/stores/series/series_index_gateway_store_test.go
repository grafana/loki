package series

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
)

type mockClient struct {
	GatewayClient
}

func (mockClient) GetSeries(_ context.Context, _ *logproto.GetSeriesRequest) (*logproto.GetSeriesResponse, error) {
	return &logproto.GetSeriesResponse{}, nil
}

func (mockClient) GetChunkRef(_ context.Context, _ *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
	return &logproto.GetChunkRefResponse{
		Refs: []*logproto.ChunkRef{},
		Stats: stats.Index{
			TotalChunks:      1000,
			PostFilterChunks: 10,
			ShardsDuration:   0,
			UsedBloomFilters: true,
		},
	}, nil
}

type statsRecordingClient struct {
	GatewayClient
	req *logproto.IndexStatsRequest
}

func (c *statsRecordingClient) GetStats(_ context.Context, req *logproto.IndexStatsRequest) (*logproto.IndexStatsResponse, error) {
	c.req = req
	return &logproto.IndexStatsResponse{}, nil
}

func Test_IndexGatewayClientStore_Stats(t *testing.T) {
	hints := &logproto.HintTimeRanges{Ranges: []logproto.HintTimeRange{{Start: time.Unix(1, 0), End: time.Unix(2, 0)}}}
	for _, tc := range []struct {
		desc  string
		hints *logproto.HintTimeRanges
	}{
		{desc: "without hints", hints: nil},
		{desc: "with hints", hints: hints},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			client := &statsRecordingClient{}
			idx := NewIndexGatewayClientStore(client, log.NewNopLogger())

			_, err := idx.Stats(context.Background(), "tenant", 1000, 2000, tc.hints, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
			require.NoError(t, err)
			require.Equal(t, &logproto.IndexStatsRequest{
				From:       1000,
				Through:    2000,
				Matchers:   `{foo="bar"}`,
				HintRanges: tc.hints,
			}, client.req)
		})
	}
}

func Test_IndexGatewayClientStore_GetSeries(t *testing.T) {
	idx := NewIndexGatewayClientStore(&mockClient{}, log.NewNopLogger())
	_, err := idx.GetSeries(context.Background(), "tenant", model.Earliest, model.Latest)
	require.NoError(t, err)
}

func Test_IndexGatewayClientStore_GetChunkRefs(t *testing.T) {
	idx := NewIndexGatewayClientStore(&mockClient{}, log.NewNopLogger())

	t.Run("stats context is merged correctly", func(t *testing.T) {
		ctx := context.Background()
		_, ctx = stats.NewContext(ctx)

		_, err := idx.GetChunkRefs(ctx, "tenant", model.Earliest, model.Latest, chunk.NewPredicate(nil, nil))
		require.NoError(t, err)

		_, err = idx.GetChunkRefs(ctx, "tenant", model.Earliest, model.Latest, chunk.NewPredicate(nil, nil))
		require.NoError(t, err)

		statsCtx := stats.FromContext(ctx)
		require.True(t, statsCtx.Index().UsedBloomFilters)
		require.Equal(t, int64(2000), statsCtx.Index().TotalChunks)
		require.Equal(t, int64(20), statsCtx.Index().PostFilterChunks)
	})
}
