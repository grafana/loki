package logql

import (
	"encoding/json"

	"github.com/grafana/dskit/multierror"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	"github.com/pkg/errors"
)

type ShardResolver interface {
	Shards(expr syntax.Expr) (int, uint64, error)
	ShardingRanges(expr syntax.Expr, targetBytesPerShard uint64) ([]logproto.Shard, error)
	GetStats(e syntax.Expr) (stats.Stats, error)
}

type ConstantShards int

func (s ConstantShards) Shards(_ syntax.Expr) (int, uint64, error) { return int(s), 0, nil }
func (s ConstantShards) ShardingRanges(_ syntax.Expr, _ uint64) ([]logproto.Shard, error) {
	return sharding.LinearShards(int(s), 0), nil
}
func (s ConstantShards) GetStats(_ syntax.Expr) (stats.Stats, error) { return stats.Stats{}, nil }

type ShardingStrategy interface {
	Shards(expr syntax.Expr) (shards Shards, maxBytesPerShard uint64, err error)
	Resolver() ShardResolver
}

type DynamicBoundsStrategy struct {
	resolver            ShardResolver
	targetBytesPerShard uint64
}

func (s DynamicBoundsStrategy) Shards(expr syntax.Expr) (Shards, uint64, error) {
	shards, err := s.resolver.ShardingRanges(expr, s.targetBytesPerShard)
	if err != nil {
		return nil, 0, err
	}

	var maxBytes uint64
	res := make(Shards, 0, len(shards))
	for _, shard := range shards {
		if shard.Stats != nil {
			maxBytes = max(maxBytes, shard.Stats.Bytes)
		}
		res = append(res, NewBoundedShard(shard))
	}

	return res, maxBytes, nil
}

func (s DynamicBoundsStrategy) Resolver() ShardResolver {
	return s.resolver
}

func NewDynamicBoundsStrategy(resolver ShardResolver, targetBytesPerShard uint64) DynamicBoundsStrategy {
	return DynamicBoundsStrategy{resolver: resolver, targetBytesPerShard: targetBytesPerShard}
}

type PowerOfTwoStrategy struct {
	resolver ShardResolver
}

func NewPowerOfTwoStrategy(resolver ShardResolver) PowerOfTwoStrategy {
	return PowerOfTwoStrategy{resolver: resolver}
}

func (s PowerOfTwoStrategy) Resolver() ShardResolver {
	return s.resolver
}

func (s PowerOfTwoStrategy) Shards(expr syntax.Expr) (Shards, uint64, error) {
	factor, bytesPerShard, err := s.resolver.Shards(expr)
	if err != nil {
		return nil, 0, err
	}

	if factor == 0 {
		return nil, bytesPerShard, nil
	}

	res := make(Shards, 0, factor)
	for i := 0; i < factor; i++ {
		res = append(res, NewPowerOfTwoShard(astmapper.ShardAnnotation{Of: factor, Shard: i}))
	}
	return res, bytesPerShard, nil
}

// Shard represents a shard annotation
// It holds either a power of two shard (legacy) or a bounded shard
type Shard struct {
	PowerOfTwo *astmapper.ShardAnnotation
	Bounded    *logproto.Shard
}

// convenience method for unaddressability concerns using constructors in literals (tests)
func (s Shard) Ptr() *Shard {
	return &s
}

func NewBoundedShard(shard logproto.Shard) Shard {
	return Shard{Bounded: &shard}
}

func NewPowerOfTwoShard(shard astmapper.ShardAnnotation) Shard {
	return Shard{PowerOfTwo: &shard}
}

func (s Shard) String() string {
	if s.Bounded != nil {
		b, err := json.Marshal(s.Bounded)
		if err != nil {
			panic(err)
		}
		return string(b)
	}

	return s.PowerOfTwo.String()
}

type Shards []Shard

type ShardVersion uint8

const (
	PowerOfTwoVersion ShardVersion = iota
	BoundedVersion
)

func (xs Shards) Encode() (encoded []string) {
	for _, shard := range xs {
		encoded = append(encoded, shard.String())
	}

	return encoded
}

// ParseShards parses a list of string encoded shards
func ParseShards(strs []string) (Shards, ShardVersion, error) {
	if len(strs) == 0 {
		return nil, PowerOfTwoVersion, nil
	}
	shards := make(Shards, 0, len(strs))

	var prevVersion ShardVersion
	for i, str := range strs {
		shard, version, err := ParseShard(str)
		if err != nil {
			return nil, PowerOfTwoVersion, err
		}

		if i == 0 {
			prevVersion = version
		} else if prevVersion != version {
			return nil, PowerOfTwoVersion, errors.New("shards must be of the same version")
		}
		shards = append(shards, shard)
	}
	return shards, prevVersion, nil
}

func ParseShard(s string) (Shard, ShardVersion, error) {

	var bounded logproto.Shard
	v2Err := json.Unmarshal([]byte(s), &bounded)
	if v2Err == nil {
		return Shard{Bounded: &bounded}, BoundedVersion, nil
	}

	old, v1Err := astmapper.ParseShard(s)
	if v1Err == nil {
		return Shard{PowerOfTwo: &old}, PowerOfTwoVersion, nil
	}

	err := errors.Wrap(
		multierror.New(v1Err, v2Err).Err(),
		"failed to parse shard",
	)
	return Shard{}, PowerOfTwoVersion, err
}
