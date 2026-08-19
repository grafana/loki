package logsobj

import (
	"cmp"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

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
		Shard:     streams.ShardBucket(streamLabels),
		SchemaKey: schemaKey,
		Hash:      hash,
		Labels:    streamLabels,
	}, nil
}

// CompareStreamOrderKey compares stream keys by shard bucket, schema key,
// stable hash, and full labels.
func CompareStreamOrderKey(a, b StreamOrderKey) int {
	if n := cmp.Compare(a.Shard, b.Shard); n != 0 {
		return n
	}
	if n := cmp.Compare(a.SchemaKey, b.SchemaKey); n != 0 {
		return n
	}
	if n := cmp.Compare(a.Hash, b.Hash); n != 0 {
		return n
	}
	return labels.Compare(a.Labels, b.Labels)
}
