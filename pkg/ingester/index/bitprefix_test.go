package index

import (
	"fmt"
	"testing"

	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/stretchr/testify/require"
)

func TestBitPrefixGetShards(t *testing.T) {
	ii := NewBitPrefixWithShards(4)
	for i, tc := range []struct {
		shard  *astmapper.ShardAnnotation
		exp    []*indexShard
		filter bool
	}{
		{
			shard:  &astmapper.ShardAnnotation{Shard: 0, Of: 2},
			exp:    ii.shards[:2],
			filter: false,
		},
		{
			shard:  &astmapper.ShardAnnotation{Shard: 1, Of: 2},
			exp:    ii.shards[2:],
			filter: false,
		},
		{
			shard:  &astmapper.ShardAnnotation{Shard: 0, Of: 8},
			exp:    ii.shards[:1],
			filter: true,
		},
		{
			shard:  &astmapper.ShardAnnotation{Shard: 1, Of: 8},
			exp:    ii.shards[:1],
			filter: true,
		},
		{
			shard:  &astmapper.ShardAnnotation{Shard: 7, Of: 8},
			exp:    ii.shards[3:],
			filter: true,
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			got, filter := ii.getShards(tc.shard)
			require.Equal(t, tc.filter, filter)
			require.Equal(t, tc.exp, got)

		})
	}
}
