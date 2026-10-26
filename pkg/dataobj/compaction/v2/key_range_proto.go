package compactionv2

import (
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// FullKeyRangeProto returns the key range that holds every stream, from
// [streams.MinSortPrefix] to [streams.SortPrefixLimit]. Each call returns a
// new value that the caller owns.
func FullKeyRangeProto() *compactionv2pb.KeyRange {
	return &compactionv2pb.KeyRange{
		Start: &compactionv2pb.SortPrefix{ShardBucket: streams.MinSortPrefix().ShardBucket},
		End:   &compactionv2pb.SortPrefix{ShardBucket: streams.SortPrefixLimit().ShardBucket},
	}
}
