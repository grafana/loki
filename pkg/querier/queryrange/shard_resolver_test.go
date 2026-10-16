package queryrange

import (
	"context"
	"testing"
	"time"

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

func TestDynamicShardResolver_policy(t *testing.T) {
	cfg := Config{DataObjEnabled: true, DataObjStorageLag: dataObjRoutingLag, now: func() time.Time { return dataObjRoutingNow }}
	limits := dataObjTenantLimits("1")
	tenants := []string{"1"}
	expr := syntax.MustParseExpr(streamFirstQuery)

	newResolver := func(start, end time.Time) *dynamicShardResolver {
		return &dynamicShardResolver{
			start:         start,
			end:           end,
			chunkPolicy:   newChunkShardingPolicy(limits),
			dataObjPolicy: newDataObjShardingPolicy(cfg, limits),
		}
	}

	t.Run("returns the data-object policy for a split that crosses the end of the held range and lies mostly inside it", func(t *testing.T) {
		resolver := newResolver(dataObjRoutingBandEnd.Add(-40*time.Minute), dataObjRoutingBandEnd.Add(20*time.Minute))

		require.IsType(t, &dataObjShardingPolicy{}, resolver.policy(tenants, expr))
	})

	t.Run("returns the chunk policy for a split that crosses the end of the held range and lies mostly outside it", func(t *testing.T) {
		resolver := newResolver(dataObjRoutingBandEnd.Add(-20*time.Minute), dataObjRoutingBandEnd.Add(40*time.Minute))

		require.IsType(t, chunkShardingPolicy{}, resolver.policy(tenants, expr))
	})
}
