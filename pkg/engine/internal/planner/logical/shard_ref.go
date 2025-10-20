package logical

import (
	"fmt"
)

// A ShardInfo defines a subset of a table relation. ShardInfo only implements [Value].
// It is the equivalent to the [index.ShardAnnotation] in the old query engine.
type ShardInfo struct {
	Shard uint32
	Of    uint32 // MUST be a power of 2 to ensure sharding logic works correctly.
}

var (
	_ Value = (*ShardInfo)(nil)
)

// Name returns the identifier of the ShardRef.
func (s *ShardInfo) Name() string {
	return fmt.Sprintf("%d_of_%d", s.Shard, s.Of)
}

// String returns [ShardInfo.Name].
func (s *ShardInfo) String() string {
	return s.Name()
}

func (s *ShardInfo) isValue() {}

func NewShard(shard, of uint32) *ShardInfo {
	return &ShardInfo{
		Shard: shard,
		Of:    of,
	}
}

var noShard = NewShard(0, 1)
