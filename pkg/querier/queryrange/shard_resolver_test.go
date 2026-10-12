package queryrange

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func TestDynamicShardResolver_ShardingRanges(t *testing.T) {
	t.Run("returns the shards and the chunk refs that the index returns", func(t *testing.T) {
		shards, chunkGroups := boundedShards(4, 10)
		index := queryrangebase.HandlerFunc(func(context.Context, queryrangebase.Request) (queryrangebase.Response, error) {
			return &ShardsResponse{Response: &logproto.ShardsResponse{Shards: shards, ChunkGroups: chunkGroups}}, nil
		})
		resolver := &dynamicShardResolver{
			ctx:              user.InjectOrgID(t.Context(), "1"),
			logger:           log.NewNopLogger(),
			retryNextHandler: index,
			chunkPolicy:      newChunkShardingPolicy(fakeLimits{}),
			dataObjPolicy:    newDataObjShardingPolicy(Config{}, fakeLimits{}),
		}

		gotShards, gotChunkGroups, err := resolver.ShardingRanges(syntax.MustParseExpr(`{app="a"}`), 1<<20)

		require.NoError(t, err)
		require.Equal(t, shards, gotShards)
		require.Equal(t, chunkGroups, gotChunkGroups)
	})
}
