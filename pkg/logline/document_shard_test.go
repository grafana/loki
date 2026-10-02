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

	for bits := uint(1); bits <= MaxDocumentShardBits; bits++ {
		shards := uint32(1) << bits
		t.Run(fmt.Sprintf("bits=%d", bits), func(t *testing.T) {
			for _, fp := range fps {
				got := DocumentShard(fp, bits)
				require.Less(t, got, shards)
				for s := range shards {
					match := index.NewShard(s, shards).Match(model.Fingerprint(fp))
					require.Equal(t, s == got, match, "fp %x shard %d of %d", fp, s, shards)
				}
			}
		})
	}
}

func TestDocumentShard_ZeroBitsIsOneShard(t *testing.T) {
	require.Zero(t, DocumentShard(math.MaxUint64, 0))
}
