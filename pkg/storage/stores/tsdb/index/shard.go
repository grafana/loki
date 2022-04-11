package index

import (
	"fmt"
	"math"

	"github.com/prometheus/common/model"
)

const (
	// ShardLabel is a reserved label referencing a cortex shard
	ShardLabel = "__tsdb_shard__"
	// ShardLabelFmt is the fmt of the ShardLabel key.
	ShardLabelFmt = "%d_of_%d"
)

// ShardAnnotation is a convenience struct which holds data from a parsed shard label
// Of MUST be a power of 2 to ensure sharding logic works correctly.
type ShardAnnotation struct {
	Shard uint32
	Of    uint32
}

func NewShard(x, of uint32) ShardAnnotation {
	return ShardAnnotation{
		Shard: x,
		Of:    of,
	}
}

// Match returns whether a fingerprint belongs to a certain shard.
// The Shard must be a power of 2.
// Inclusion in a shard is calculated by determining the arbitrary bit prefix
// for a shard, then ensuring the fingerprint has the same prefix
func (shard ShardAnnotation) Match(fp model.Fingerprint) bool {
	requiredBits := shard.RequiredBits()

	// A shard only matches a fingerprint when they both start with the same prefix
	prefix := uint64(shard.Shard) << (64 - requiredBits)
	return prefix^uint64(fp) < 1<<(64-requiredBits)
}

// String encodes a shardAnnotation into a label value
func (shard ShardAnnotation) String() string {
	return fmt.Sprintf(ShardLabelFmt, shard.Shard, shard.Of)
}

func (shard ShardAnnotation) RequiredBits() uint64 {
	// The minimum number of bits required to represent shard.Of
	return uint64(math.Log2(float64(shard.Of)))

}

// Bounds shows the [minimum, maximum) fingerprints. If there is no maximum
// fingerprint (for example )
func (shard ShardAnnotation) Bounds() (model.Fingerprint, model.Fingerprint) {
	requiredBits := model.Fingerprint(shard.RequiredBits())
	from := model.Fingerprint(shard.Shard) << (64 - requiredBits)

	if shard.Shard+1 == shard.Of {
		return from, model.Fingerprint(math.MaxUint64)
	}
	return from, model.Fingerprint(shard.Shard+1) << (64 - requiredBits)
}
