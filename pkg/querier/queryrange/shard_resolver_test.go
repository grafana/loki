package queryrange

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/stores/index/stats"
)

func TestGuessShardFactor(t *testing.T) {
	for _, tc := range []struct {
		stats          stats.Stats
		maxParallelism int
		exp            int
	}{
		{
			// no data == no sharding
			exp:            0,
			maxParallelism: 10,
		},
		{
			exp:            4,
			maxParallelism: 10,
			stats: stats.Stats{
				Bytes: 1200 << 20, // 1200MB
			},
		},
		{
			exp:            8,
			maxParallelism: 10,
			// 1500MB moves us to the next
			// power of 2 parallelism factor
			stats: stats.Stats{
				Bytes: 1500 << 20,
			},
		},
		{
			// Two fully packed parallelism cycles
			exp:            16,
			maxParallelism: 8,
			stats: stats.Stats{
				Bytes: maxSchedulableBytes * 16,
			},
		},
		{
			// increase to next factor of two
			exp:            32,
			maxParallelism: 8,
			stats: stats.Stats{
				Bytes: maxSchedulableBytes * 17,
			},
		},
	} {
		t.Run(fmt.Sprintf("%+v", tc.stats), func(t *testing.T) {
			require.Equal(t, tc.exp, guessShardFactor(tc.stats, tc.maxParallelism))
		})
	}
}
