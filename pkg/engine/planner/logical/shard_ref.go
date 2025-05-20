package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// A ShardRef defines a subset of a table relation. ShardRef only implements [Value].
// It is the equivalent to the [index.ShardAnnotation] in the old query engine.
type ShardRef struct {
	Shard uint32
	Of    uint32 // MUST be a power of 2 to ensure sharding logic works correctly.
}

var (
	_ Value = (*ShardRef)(nil)
)

// Name returns the identifier of the ShardRef, which combines the column type
// and column name being referenced.
func (s *ShardRef) Name() string {
	return fmt.Sprintf("%d_of_%d", s.Shard, s.Of)
}

// String returns [ShardRef.Name].
func (s *ShardRef) String() string {
	return s.Name()
}

// Schema returns the schema of the column being referenced.
func (s *ShardRef) Schema() *schema.Schema {
	return nil
}

func (s *ShardRef) isValue() {}

func NewShardRef(shard, of uint32) *ShardRef {
	return &ShardRef{
		Shard: shard,
		Of:    of,
	}
}

var noShard = NewShardRef(0, 1)
