package index

import (
	"fmt"
	"testing"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/stretchr/testify/require"
)

// Example of subsharding where the available (32) is higher and divisible by request (16)
// We will return those index shards for each request shard.
// 0 16 = 1
// 1 17 = 2
// 2 18 = 3
// 3 19 = 4
// 4 20 = 5
// 5 21 = 6
// 6 22 = 7
// 7 23 = 8
// 8 24 = 9
// 9 25 = 10
// 10 26 = 11
// 11 27 = 12
// 12 28 = 13
// 13 29 = 14
// 14 30 = 15
// 15 31 = 16

func Test_GetShards(t *testing.T) {
	for _, tt := range []struct {
		total    uint32
		shard    *astmapper.ShardAnnotation
		expected []uint32
	}{
		{16, &astmapper.ShardAnnotation{Shard: 0, Of: 16}, []uint32{0}},
		{16, &astmapper.ShardAnnotation{Shard: 4, Of: 16}, []uint32{4}},
		{16, &astmapper.ShardAnnotation{Shard: 15, Of: 16}, []uint32{15}},

		{32, &astmapper.ShardAnnotation{Shard: 0, Of: 16}, []uint32{0, 16}},
		{32, &astmapper.ShardAnnotation{Shard: 4, Of: 16}, []uint32{4, 20}},
		{32, &astmapper.ShardAnnotation{Shard: 15, Of: 16}, []uint32{15, 31}},

		{64, &astmapper.ShardAnnotation{Shard: 0, Of: 16}, []uint32{0, 16, 32, 48}},
		{64, &astmapper.ShardAnnotation{Shard: 4, Of: 16}, []uint32{4, 20, 36, 52}},
		{64, &astmapper.ShardAnnotation{Shard: 15, Of: 16}, []uint32{15, 31, 47, 63}},
	} {
		tt := tt
		t.Run(tt.shard.String()+fmt.Sprintf("_total_%d", tt.total), func(t *testing.T) {
			ii := NewWithShards(tt.total)
			res := ii.getShards(tt.shard)
			resInt := []uint32{}
			for _, r := range res {
				resInt = append(resInt, r.shard)
			}
			require.Equal(t, tt.expected, resInt)
		})
	}
}

func Test_ValidateShards(t *testing.T) {
	require.NoError(t, validateShard(32, &astmapper.ShardAnnotation{Shard: 1, Of: 16}))
}
