package queryrange

import (
	"fmt"
	"testing"

	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/stretchr/testify/require"
)

func TestGuessShardFactor(t *testing.T) {
	for _, tc := range []struct {
		stats stats.Stats
		exp   int
	}{
		{
			// no data == no sharding
			exp: 0,
		},
		{
			exp: 4,
			stats: stats.Stats{
				Bytes: 1200 << 20, // 1200MB
			},
		},
		{
			exp: 8,
			// 1500MB moves us to the next
			// power of 2 parallelism factor
			stats: stats.Stats{
				Bytes: 1500 << 20,
			},
		},
	} {
		t.Run(fmt.Sprintf("%+v", tc.stats), func(t *testing.T) {
			require.Equal(t, tc.exp, guessShardFactor(tc.stats))
		})
	}
}
