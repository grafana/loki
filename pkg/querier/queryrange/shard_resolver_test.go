package queryrange

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/storage/stores/index/stats"
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
				Bytes: maxBytesPerShard * 4,
			},
		},
		{
			// round up shard factor
			exp: 16,
			stats: stats.Stats{
				Bytes: maxBytesPerShard * 15,
			},
		},
		{
			exp: 2,
			stats: stats.Stats{
				Bytes: maxBytesPerShard + 1,
			},
		},
		{
			exp: 0,
			stats: stats.Stats{
				Bytes: maxBytesPerShard,
			},
		},
		{
			maxShards: 8,
			exp:       4,
			stats: stats.Stats{
				Bytes: maxBytesPerShard * 4,
			},
		},
		{
			maxShards: 2,
			exp:       2,
			stats: stats.Stats{
				Bytes: maxBytesPerShard * 4,
			},
		},
		{
			maxShards: 1,
			exp:       0,
			stats: stats.Stats{
				Bytes: maxBytesPerShard * 4,
			},
		},
	} {
		t.Run(fmt.Sprintf("%+v", tc.stats), func(t *testing.T) {
			require.Equal(t, tc.exp, guessShardFactor(tc.stats, tc.maxShards))
		})
	}
}

func Test_dynamicShardResolver_checkQuerySizeLimit(t *testing.T) {
	for _, tc := range []struct {
		desc          string
		bytesPerShard uint64
		limits        Limits
		shouldErr     bool
	}{
		{
			desc: "Unlimited",
			limits: fakeLimits{
				maxQuerierBytesRead: 0,
			},
			bytesPerShard: 10,
			shouldErr:     false,
		},
		{
			desc: "Bellow limit",
			limits: fakeLimits{
				maxQuerierBytesRead: 10,
			},
			bytesPerShard: 5,
			shouldErr:     false,
		},
		{
			desc: "Equal limit",
			limits: fakeLimits{
				maxQuerierBytesRead: 10,
			},
			bytesPerShard: 10,
			shouldErr:     false,
		},
		{
			desc: "Above limit",
			limits: fakeLimits{
				maxQuerierBytesRead: 10,
			},
			bytesPerShard: 15,
			shouldErr:     true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			r := &dynamicShardResolver{
				ctx: user.InjectOrgID(context.Background(), "foo"),
				limits: fakeLimits{
					maxQuerierBytesRead: 10,
				},
			}

			err := r.checkQuerySizeLimit(tc.bytesPerShard)
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
