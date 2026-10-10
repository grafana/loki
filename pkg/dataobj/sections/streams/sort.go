package streams

import (
	"cmp"
	"strings"

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
		a.Prefix().Compare(b.Prefix()),
		cmp.Compare(a.Hash, b.Hash),
	)
}

// Prefix returns the leading [shard, schema key] part of the sort key.
func (a SortKey) Prefix() SortPrefix {
	return SortPrefix{ShardBucket: a.ShardBucket, SchemaKey: a.SchemaKey}
}

// SortPrefix is the [shard bucket, schema key] prefix of a [SortKey]. Streams
// with the same prefix are contiguous in stream ID order, so a range of
// prefixes selects a contiguous range of stream IDs.
type SortPrefix struct {
	ShardBucket uint32
	SchemaKey   string
}

// NewSortPrefix returns the prefix for a shard bucket and the stream's values
// of the sort-schema labels, in schema order.
func NewSortPrefix(shardBucket uint32, schemaValues []string) SortPrefix {
	return SortPrefix{ShardBucket: shardBucket, SchemaKey: EncodeSchemaKey(schemaValues)}
}

// Compare reports the order of a and b by shard bucket, then schema key.
func (a SortPrefix) Compare(b SortPrefix) int {
	return cmp.Or(
		cmp.Compare(a.ShardBucket, b.ShardBucket),
		cmp.Compare(a.SchemaKey, b.SchemaKey),
	)
}

// EncodeSchemaKey joins sort-schema label values into one schema key. A NUL
// byte separates the values.
//
// Stream IDs in written objects follow the byte order of this encoding. Code
// that must agree with stream ID order compares encoded keys, not the
// separate values: they order differently when a value contains a NUL byte.
func EncodeSchemaKey(values []string) string {
	return strings.Join(values, "\x00")
}
