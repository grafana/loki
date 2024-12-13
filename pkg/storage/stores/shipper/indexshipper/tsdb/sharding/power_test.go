package sharding

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
)

func TestGuessShardFactor(t *testing.T) {
	for _, tc := range []struct {
		stats     stats.Stats
		maxShards int
		exp       int
	}{
		{
			// no data == no sharding
			exp: 0,
		},
		{
			exp: 4,
			stats: stats.Stats{
				Bytes: DefaultTSDBMaxBytesPerShard * 4,
			},
		},
		{
			// round up shard factor
			exp: 16,
			stats: stats.Stats{
				Bytes: DefaultTSDBMaxBytesPerShard * 15,
			},
		},
		{
			exp: 2,
			stats: stats.Stats{
				Bytes: DefaultTSDBMaxBytesPerShard + 1,
			},
		},
		{
			exp: 0,
			stats: stats.Stats{
				Bytes: DefaultTSDBMaxBytesPerShard,
			},
		},
		{
			maxShards: 8,
			exp:       4,
			stats: stats.Stats{
				Bytes: DefaultTSDBMaxBytesPerShard * 4,
			},
		},
		{
			maxShards: 2,
			exp:       2,
			stats: stats.Stats{
				Bytes: DefaultTSDBMaxBytesPerShard * 4,
			},
		},
		{
			maxShards: 1,
			exp:       0,
			stats: stats.Stats{
				Bytes: DefaultTSDBMaxBytesPerShard * 4,
			},
		},
	} {
		t.Run(fmt.Sprintf("%+v", tc.stats), func(t *testing.T) {
			require.Equal(t, tc.exp, GuessShardFactor(tc.stats.Bytes, uint64(DefaultTSDBMaxBytesPerShard), tc.maxShards))
		})
	}
}
