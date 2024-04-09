package index

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_FingerprintOffsetRange(t *testing.T) {
	offsets := FingerprintOffsets{
		{1, 1},       // 00 prefix
		{2, 1 << 62}, // 01 prefix
		{3, 1 << 63}, // 10 prefix
		{4, 3 << 62}, // 11 prefix
	}

	for _, tc := range []struct {
		shard    ShardAnnotation
		min, max uint64
	}{
		{
			shard: NewShard(0, 2),
			min:   0,
			max:   3,
		},
		{
			shard: NewShard(1, 2),
			min:   2,
			max:   math.MaxUint64,
		},
		{
			shard: NewShard(1, 4),
			min:   1,
			max:   3,
		},
	} {
		t.Run(fmt.Sprint(tc.shard, tc.min, tc.max), func(t *testing.T) {
			left, right := offsets.Range(tc.shard)
			require.Equal(t, tc.min, left)
			require.Equal(t, tc.max, right)
		})
	}

}
