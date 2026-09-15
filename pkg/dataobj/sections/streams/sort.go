package streams

import (
	"cmp"

	"github.com/prometheus/prometheus/model/labels"
)

// SortKey is the globally stable ordering key for one stream.
//
// Streams are sorted by [shard_bucket, tenant sort-schema, stream hash]
// Full labels provide a deterministic tiebreaker when hashes collide so
// independently written objects assign compatible stream IDs.
type SortKey struct {
	ShardBucket uint32
	SchemaKey   string
	Hash        uint64
	Labels      labels.Labels
}

// NewSortKey computes the globally stable sorting key for a stream.
func NewSortKey(streamLabels labels.Labels, schemaKey string) SortKey {
	hash := labels.StableHash(streamLabels)
	return SortKey{
		ShardBucket: ShardBucketFromHash(hash),
		SchemaKey:   schemaKey,
		Hash:        hash,
		Labels:      streamLabels,
	}
}

// CompareSortKey compares stream keys by shard bucket, schema key,
// stable hash, and full labels.
func CompareSortKey(a, b SortKey) int {
	if n := a.Compare(b); n != 0 {
		return n
	}
	return labels.Compare(a.Labels, b.Labels)
}

// Compare reports the order of a and b by [shard, key, hash].
// Labels are not compared directly. Use CompareSortKey for a full deduplication using labels.
func (a SortKey) Compare(b SortKey) int {
	return cmp.Or(
		cmp.Compare(a.ShardBucket, b.ShardBucket),
		cmp.Compare(a.SchemaKey, b.SchemaKey),
		cmp.Compare(a.Hash, b.Hash),
	)
}
