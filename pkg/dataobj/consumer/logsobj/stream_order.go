package logsobj

import (
	"cmp"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// StreamOrderKey is the globally stable ordering key for one stream.
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
		Shard:     uint32(hash % uint64(streams.ShardFactor)),
		SchemaKey: schemaKey,
		Hash:      hash,
		Labels:    streamLabels,
	}, nil
}

// CompareStreamOrderKey compares stream keys by schema, stable hash, and full
// labels. Full labels provide deterministic ordering when hashes collide.
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
