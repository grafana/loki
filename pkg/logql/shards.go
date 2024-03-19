package logql

import (
	"encoding/json"

	"github.com/grafana/dskit/multierror"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/pkg/errors"
)

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

func (xs Shards) Encode() (encoded []string) {
	for _, shard := range xs {
		encoded = append(encoded, shard.String())
	}

	return encoded
}

// ParseShards parses a list of string encoded shards
func ParseShards(strs []string) (Shards, error) {
	if len(strs) == 0 {
		return nil, nil
	}
	shards := make(Shards, 0, len(strs))

	for _, str := range strs {
		shard, err := ParseShard(str)
		if err != nil {
			return nil, err
		}
		shards = append(shards, shard)
	}
	return shards, nil
}

func ParseShard(s string) (Shard, error) {

	var bounded logproto.Shard
	v2Err := json.Unmarshal([]byte(s), &bounded)
	if v2Err == nil {
		return Shard{Bounded: &bounded}, nil
	}

	old, v1Err := astmapper.ParseShard(s)
	if v1Err == nil {
		return Shard{PowerOfTwo: &old}, nil
	}

	err := errors.Wrap(
		multierror.New(v1Err, v2Err).Err(),
		"failed to parse shard",
	)
	return Shard{}, err
}
