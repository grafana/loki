package index

import (
	"fmt"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

const (
	// ShardLabel is a reserved label referencing a cortex shard
	ShardLabel = "__tsdb_shard__"
	// ShardLabelFmt is the fmt of the ShardLabel key.
	ShardLabelFmt = "%d_of_%d"
)

// ShardAnnotation is a convenience struct which holds data from a parsed shard label
type ShardAnnotation struct {
	Shard uint32
	Of    uint32
}

func (shard ShardAnnotation) Match(fp model.Fingerprint) bool {
	return uint64(fp)%uint64(shard.Of) == uint64(shard.Shard)
}

// String encodes a shardAnnotation into a label value
func (shard ShardAnnotation) String() string {
	return fmt.Sprintf(ShardLabelFmt, shard.Shard, shard.Of)
}

// Label generates the ShardAnnotation as a label
func (shard ShardAnnotation) Label() labels.Label {
	return labels.Label{
		Name:  ShardLabel,
		Value: shard.String(),
	}
}
