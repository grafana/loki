package index

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"

	"github.com/prometheus/common/model"
)

const (
	// ShardLabel is a reserved label referencing a cortex shard
	ShardLabel = "__tsdb_shard__"
	// ShardLabelFmt is the fmt of the ShardLabel key.
	ShardLabelFmt = "%d_of_%d"
)

var errDisallowedIdentityShard = errors.New("shard with factor of 1 is explicitly disallowed. It's equivalent to no sharding")

type FingerprintFilter interface {
	// TODO(owen-d): Match() is redundant and can be inferred from GetFromThrough()
	// TODO(owen-d): GetFromThrough should just return FingerprintBounds as it's a better utility struct.
	Match(model.Fingerprint) bool
	// GetFromThrough shows the [minimum, maximum) fingerprints. If there is no maximum, math.MaxUint64 may be used
	GetFromThrough() (model.Fingerprint, model.Fingerprint)
}

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
	if shard.Of < 2 {
		return true
	}
	requiredBits := shard.RequiredBits()

	// A shard only matches a fingerprint when they both start with the same prefix
	return uint64(shard.Shard) == uint64(fp)>>(64-requiredBits)
}

// String encodes a shardAnnotation into a label value
func (shard ShardAnnotation) String() string {
	return fmt.Sprintf(ShardLabelFmt, shard.Shard, shard.Of)
}

func (shard ShardAnnotation) RequiredBits() uint64 {
	// The minimum number of bits required to represent shard.Of
	return uint64(math.Log2(float64(shard.Of)))

}

func (shard ShardAnnotation) Validate() error {
	if shard.Of == 0 {
		// 0 factor is explicitly not sharding
		return nil
	}

	if shard.Of == 1 {
		return errDisallowedIdentityShard
	}

	if 1<<shard.RequiredBits() != shard.Of {
		return fmt.Errorf("shard factor must be a power of two, got %d", shard.Of)
	}
	return nil
}

// ParseShardString parses a shard in the ShardLabelFmt form `<x>_of_<K>`. K
// must be a power of two and x below K. `0_of_1` is the whole fingerprint
// space.
func ParseShardString(s string) (ShardAnnotation, error) {
	parts := strings.Split(s, "_of_")
	if len(parts) != 2 {
		return ShardAnnotation{}, fmt.Errorf("invalid shard %q: want <shard>_of_<factor>", s)
	}
	x, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return ShardAnnotation{}, fmt.Errorf("invalid shard %q: %w", s, err)
	}
	of, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return ShardAnnotation{}, fmt.Errorf("invalid shard %q: %w", s, err)
	}
	if bits.OnesCount64(of) != 1 {
		return ShardAnnotation{}, fmt.Errorf("invalid shard %q: factor must be a power of two", s)
	}
	if x >= of {
		return ShardAnnotation{}, fmt.Errorf("invalid shard %q: shard must be below the factor", s)
	}
	return NewShard(uint32(x), uint32(of)), nil
}

// GetFromThrough shows the [minimum, maximum) fingerprints. If there is no maximum
// fingerprint (for example the last shard), math.MaxUint64 is used as the maximum.
func (shard ShardAnnotation) GetFromThrough() (model.Fingerprint, model.Fingerprint) {
	requiredBits := model.Fingerprint(shard.RequiredBits())
	from := model.Fingerprint(shard.Shard) << (64 - requiredBits)

	if shard.Shard+1 == shard.Of {
		return from, model.Fingerprint(math.MaxUint64)
	}
	return from, model.Fingerprint(shard.Shard+1) << (64 - requiredBits)
}

// FingerprintRangesOverlap reports whether two half-open fingerprint ranges
// overlap. MaxUint64 is an unbounded upper sentinel and includes the maximum
// fingerprint, including a range whose lower bound is MaxUint64 itself.
func FingerprintRangesOverlap(aFrom, aThrough, bFrom, bThrough model.Fingerprint) bool {
	if aThrough != math.MaxUint64 {
		if aFrom >= aThrough {
			return false
		}
		aThrough--
	}
	if bThrough != math.MaxUint64 {
		if bFrom >= bThrough {
			return false
		}
		bThrough--
	}
	return aFrom <= aThrough && bFrom <= bThrough && aFrom <= bThrough && bFrom <= aThrough
}
