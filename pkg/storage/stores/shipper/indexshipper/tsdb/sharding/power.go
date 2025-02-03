package sharding

import (
	"math"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
)

const (
	DefaultTSDBMaxBytesPerShard = 600 << 20 // 600MB
)

// PowerOfTwoSharding is a slimmed down legacy sharding implementation
// designed for use as a fallback when the newer impls aren't found
// (i.e. during a system upgrade to support the new impl)
type PowerOfTwoSharding struct {
	MaxShards int
}

func (p PowerOfTwoSharding) ShardsFor(bytes uint64, maxBytesPerShard uint64) []logproto.Shard {
	factor := GuessShardFactor(bytes, maxBytesPerShard, p.MaxShards)
	return LinearShards(factor, bytes)
}

// LinearShards is a sharding implementation that splits the data into
// equal sized shards covering the entire keyspace. It populates
// the `bytes` of each shard's stats with a proportional estimation
func LinearShards(n int, bytes uint64) []logproto.Shard {
	if n < 2 {
		return []logproto.Shard{
			{
				Bounds: logproto.FPBounds{
					Min: 0,
					Max: math.MaxUint64,
				},
				Stats: &stats.Stats{
					Bytes: bytes,
				},
			},
		}
	}

	bytesPerShard := bytes / uint64(n)
	fpPerShard := model.Fingerprint(math.MaxUint64) / model.Fingerprint(n)

	shards := make([]logproto.Shard, n)
	for i := range shards {
		shards[i] = logproto.Shard{
			Bounds: logproto.FPBounds{
				Min: model.Fingerprint(i) * fpPerShard,
				Max: model.Fingerprint(i+1) * fpPerShard,
			},
			Stats: &stats.Stats{
				Bytes: bytesPerShard,
			},
		}
	}
	// The last shard should have the remainder of the bytes
	// and the max bound should be math.MaxUint64
	// NB(owen-d): this can only happen when maxShards is used
	// and the maxShards isn't a factor of 2
	shards[len(shards)-1].Stats.Bytes += bytes % uint64(n)
	shards[len(shards)-1].Bounds.Max = math.MaxUint64

	return shards

}

// Since we shard by powers of two and we increase shard factor
// once each shard surpasses maxBytesPerShard, if the shard factor
// is at least two, the range of data per shard is (maxBytesPerShard/2, maxBytesPerShard]
// For instance, for a maxBytesPerShard of 500MB and a query touching 1000MB, we split into two shards of 500MB.
// If there are 1004MB, we split into four shards of 251MB.
func GuessShardFactor(bytes, maxBytesPerShard uint64, maxShards int) int {
	// If maxBytesPerShard is 0, we use the default value
	// to avoid division by zero
	if maxBytesPerShard < 1 {
		maxBytesPerShard = DefaultTSDBMaxBytesPerShard
	}

	minShards := float64(bytes) / float64(maxBytesPerShard)

	// round up to nearest power of 2
	power := math.Ceil(math.Log2(minShards))

	// Since x^0 == 1 and we only support factors of 2
	// reset this edge case manually
	factor := int(math.Pow(2, power))
	if maxShards > 0 {
		factor = min(factor, maxShards)
	}

	// shortcut: no need to run any sharding logic when factor=1
	// as it's the same as no sharding
	if factor == 1 {
		factor = 0
	}
	return factor
}
