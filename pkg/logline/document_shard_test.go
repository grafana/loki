package logline

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// TestDocumentShard_MatchesShardAnnotation pins document shards to TSDB's
// power-of-two query shards: a fingerprint's document shard is the one shard
// whose ShardAnnotation matches it.
func TestDocumentShard_MatchesShardAnnotation(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	fps := []uint64{0, 1, math.MaxUint64, 1 << 63, (1 << 63) - 1}
	for range 200 {
		fps = append(fps, rng.Uint64())
	}

	for shards := 2; shards <= MaxDocumentShards; shards *= 2 {
		t.Run(fmt.Sprintf("shards=%d", shards), func(t *testing.T) {
			for _, fp := range fps {
				got := DocumentShard(fp, shards)
				require.Less(t, got, uint32(shards))
				for s := range uint32(shards) {
					match := index.NewShard(s, uint32(shards)).Match(model.Fingerprint(fp))
					require.Equal(t, s == got, match, "fp %x shard %d of %d", fp, s, shards)
				}
			}
		})
	}
}

func TestDocumentShard_SingleShard(t *testing.T) {
	for _, shards := range []int{0, 1} {
		require.Zero(t, DocumentShard(math.MaxUint64, shards))
	}
}
