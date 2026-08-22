package logsobj

import (
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
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
	r, err := newStreamRemap(streamLabels, schemaLabels)
	if err != nil {
		return StreamOrderKey{}, err
	}
	return r.orderKey(streamLabels), nil
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
