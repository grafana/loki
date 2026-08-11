package util

import (
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

// HasStreamShardLabels reports whether ls carries a label added by automatic
// stream sharding (constants.StreamShardLabel or constants.TimeShardLabel).
func HasStreamShardLabels(ls labels.Labels) bool {
	return ls.Has(constants.StreamShardLabel) || ls.Has(constants.TimeShardLabel)
}

// LabelsWithoutStreamShards returns ls without the labels that automatic stream
// sharding adds (constants.StreamShardLabel, constants.TimeShardLabel). Labels
// carrying none of them are returned unchanged.
//
// Automatic stream sharding splits one stream into shards by adding these
// labels. A log line that a client resends can land in a different shard, so the
// copies end up in different streams and are not deduplicated at query time.
// Dropping the shard labels from a stream's query-time identity gives every
// shard a single identity, so the merge iterators can drop the duplicates.
//
// Only the sharding labels are removed. Other reserved labels, such as
// __aggregated_metric__ and __pattern__, identify genuinely different streams
// and are left in place.
func LabelsWithoutStreamShards(ls labels.Labels) labels.Labels {
	if !HasStreamShardLabels(ls) {
		return ls
	}
	return labels.NewBuilder(ls).Del(constants.StreamShardLabel, constants.TimeShardLabel).Labels()
}
