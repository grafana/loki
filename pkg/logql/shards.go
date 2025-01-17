package logql

import (
	"encoding/json"

	"github.com/grafana/dskit/multierror"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
)

type Shards []Shard

type ShardVersion uint8

// TODO(owen-d): refactor this file. There's too many layers (sharding strategies, sharding resolvers).
// Eventually we should have a single strategy (bounded) and a single resolver (dynamic).
// It's likely this could be refactored anyway -- I was in a rush writing it the first time around.
const (
	PowerOfTwoVersion ShardVersion = iota
	BoundedVersion
)

func (v ShardVersion) Strategy(resolver ShardResolver, defaultTargetShardBytes uint64) ShardingStrategy {
	switch v {
	case BoundedVersion:
		return NewDynamicBoundsStrategy(resolver, defaultTargetShardBytes)
	default:
		// TODO(owen-d): refactor, ugly, etc, but the power of two strategy already populated
		// the default target shard bytes through it's resolver
		return NewPowerOfTwoStrategy(resolver)
	}
}

func (v ShardVersion) String() string {
	switch v {
	case PowerOfTwoVersion:
		return "power_of_two"
	case BoundedVersion:
		return "bounded"
	default:
		return "unknown"
	}
}

var validStrategies = map[string]ShardVersion{
	PowerOfTwoVersion.String(): PowerOfTwoVersion,
	BoundedVersion.String():    BoundedVersion,
}

func ParseShardVersion(s string) (ShardVersion, error) {
	v, ok := validStrategies[s]
	if !ok {
		return PowerOfTwoVersion, errors.Errorf("invalid shard version %s", s)
	}
	return v, nil
}

type ShardResolver interface {
	Shards(expr syntax.Expr) (int, uint64, error)
	// ShardingRanges returns shards and optionally a set of precomputed chunk refs for each group. If present,
	// they will be used in lieu of resolving chunk refs from the index durin evaluation.
	// If chunks are present, the number of shards returned must match the number of chunk ref groups.
	ShardingRanges(expr syntax.Expr, targetBytesPerShard uint64) ([]logproto.Shard, []logproto.ChunkRefGroup, error)
	GetStats(e syntax.Expr) (stats.Stats, error)
}

type ConstantShards int

func (s ConstantShards) Shards(_ syntax.Expr) (int, uint64, error) { return int(s), 0, nil }
func (s ConstantShards) ShardingRanges(_ syntax.Expr, _ uint64) ([]logproto.Shard, []logproto.ChunkRefGroup, error) {
	return sharding.LinearShards(int(s), 0), nil, nil
}
func (s ConstantShards) GetStats(_ syntax.Expr) (stats.Stats, error) { return stats.Stats{}, nil }

type ShardingStrategy interface {
	// The chunks for each shard are optional and are used to precompute chunk refs for each group
	Shards(expr syntax.Expr) (shards []ShardWithChunkRefs, maxBytesPerShard uint64, err error)
	Resolver() ShardResolver
}

type DynamicBoundsStrategy struct {
	resolver            ShardResolver
	targetBytesPerShard uint64
}

func (s DynamicBoundsStrategy) Shards(expr syntax.Expr) ([]ShardWithChunkRefs, uint64, error) {
	shards, chunks, err := s.resolver.ShardingRanges(expr, s.targetBytesPerShard)
	if err != nil {
		return nil, 0, err
	}

	var maxBytes uint64
	res := make([]ShardWithChunkRefs, 0, len(shards))
	for i, shard := range shards {
		x := ShardWithChunkRefs{
			Shard: NewBoundedShard(shard),
		}
		if shard.Stats != nil {
			maxBytes = max(maxBytes, shard.Stats.Bytes)
		}
		if len(chunks) > 0 {
			x.chunks = &chunks[i]
		}
		res = append(res, x)
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

// PowerOfTwo strategy does not support precomputed chunk refs
func (s PowerOfTwoStrategy) Shards(expr syntax.Expr) ([]ShardWithChunkRefs, uint64, error) {
	factor, bytesPerShard, err := s.resolver.Shards(expr)
	if err != nil {
		return nil, 0, err
	}

	if factor == 0 {
		return nil, bytesPerShard, nil
	}

	res := make([]ShardWithChunkRefs, 0, factor)
	for i := 0; i < factor; i++ {
		res = append(
			res,
			ShardWithChunkRefs{
				Shard: NewPowerOfTwoShard(index.ShardAnnotation{Of: uint32(factor), Shard: uint32(i)}),
			},
		)
	}
	return res, bytesPerShard, nil
}

// ShardWithChunkRefs is a convenience type for passing around shards with associated chunk refs.
// The chunk refs are optional as determined by their contents (zero chunks means no precomputed refs)
// and are used to precompute chunk refs for each group
type ShardWithChunkRefs struct {
	Shard
	chunks *logproto.ChunkRefGroup
}

// Shard represents a shard annotation
// It holds either a power of two shard (legacy) or a bounded shard
type Shard struct {
	PowerOfTwo *index.ShardAnnotation
	Bounded    *logproto.Shard
}

func (s *Shard) Variant() ShardVersion {
	if s.Bounded != nil {
		return BoundedVersion
	}

	return PowerOfTwoVersion
}

// implement FingerprintFilter
func (s *Shard) Match(fp model.Fingerprint) bool {
	if s.Bounded != nil {
		return v1.BoundsFromProto(s.Bounded.Bounds).Match(fp)
	}

	return s.PowerOfTwo.Match(fp)
}

func (s *Shard) GetFromThrough() (model.Fingerprint, model.Fingerprint) {
	if s.Bounded != nil {
		return v1.BoundsFromProto(s.Bounded.Bounds).GetFromThrough()
	}

	return s.PowerOfTwo.GetFromThrough()
}

// convenience method for unaddressability concerns using constructors in literals (tests)
func (s Shard) Ptr() *Shard {
	return &s
}

func (s Shard) Bind(chunks *logproto.ChunkRefGroup) *ShardWithChunkRefs {
	return &ShardWithChunkRefs{
		Shard:  s,
		chunks: chunks,
	}
}

func NewBoundedShard(shard logproto.Shard) Shard {
	return Shard{Bounded: &shard}
}

func NewPowerOfTwoShard(shard index.ShardAnnotation) Shard {
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
	casted := old.TSDB()
	if v1Err == nil {
		return Shard{PowerOfTwo: &casted}, PowerOfTwoVersion, nil
	}

	err := errors.Wrap(
		multierror.New(v1Err, v2Err).Err(),
		"failed to parse shard",
	)
	return Shard{}, PowerOfTwoVersion, err
}
