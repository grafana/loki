package logsobj

import (
	"slices"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// TargetSortLayout returns the canonical shard-first layout for schemaLabels.
func TargetSortLayout(schemaLabels []string) logs.SortLayout {
	return logs.SortLayout{
		SchemaLabels: schemaLabels,
		StreamOrder:  logs.StreamOrderStableHashV1,
		ShardCount:   streams.ShardFactor,
	}
}

// SortLayoutsEqual reports whether two physical sort layouts are compatible.
func SortLayoutsEqual(a, b logs.SortLayout) bool {
	return slices.Equal(a.SchemaLabels, b.SchemaLabels) &&
		a.StreamOrder == b.StreamOrder &&
		a.ShardCount == b.ShardCount
}

// StreamOrderKey is the globally stable ordering key for one stream.
//
// Physical log order is [shard_bucket, tenant sort-schema, stream hash, timestamp].
// Full labels provide a deterministic tiebreaker when hashes collide so
// independently written objects assign compatible stream IDs.
type StreamOrderKey struct {
	Shard     uint32
	SchemaKey string
	Hash      uint64
	Labels    labels.Labels
}

// NewStreamOrderKey computes the globally stable ordering key for a stream.
func NewStreamOrderKey(streamLabels labels.Labels, schemaLabels []string) (StreamOrderKey, error) {
	schemaKey, err := ComputeSortKey(streamLabels, schemaLabels)
	if err != nil {
		return StreamOrderKey{}, err
	}
	hash := labels.StableHash(streamLabels)
	return StreamOrderKey{
		Shard:     streams.ShardBucketFromHash(hash),
		SchemaKey: schemaKey,
		Hash:      hash,
		Labels:    streamLabels,
	}, nil
}

// CompareStreamOrderKey compares stream keys by shard bucket, schema key,
// stable hash, and full labels.
func CompareStreamOrderKey(a, b StreamOrderKey) int {
	if n := a.streamSort().Compare(b.streamSort()); n != 0 {
		return n
	}
	return labels.Compare(a.Labels, b.Labels)
}

func (k StreamOrderKey) streamSort() logs.StreamSort {
	return logs.StreamSort{Shard: k.Shard, Key: k.SchemaKey, Hash: k.Hash}
}
