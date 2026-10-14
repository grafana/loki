package streams

import "github.com/prometheus/prometheus/model/labels"

// DefaultShardBits is the shard bit count of [DefaultShardLayout].
//
// Do not change it. Readers assume that every object uses the default
// layout, so a new value makes them mis-read every existing object.
const DefaultShardBits = 5

// DefaultShardBuckets is the bucket count of [DefaultShardLayout].
const DefaultShardBuckets = 1 << DefaultShardBits

// DefaultShardLayout returns the shard layout of every object that Loki
// writes. Readers assume this layout when they map query shards to buckets.
func DefaultShardLayout() ShardLayout {
	return ShardLayout{bits: DefaultShardBits}
}

// ShardLayout maps streams to shard buckets. A stream's bucket is the top
// bits of labels.StableHash of its labels.
//
// The layout is part of the storage format. Writers persist bucket values,
// and readers trust them without a new hash check. A reader that uses a
// different layout than the writer returns wrong results.
//
// The zero value has one bucket.
type ShardLayout struct {
	bits uint8
}

// Bits returns the number of high hash bits that select a bucket.
func (l ShardLayout) Bits() int { return int(l.bits) }

// Buckets returns the number of shard buckets, 2^Bits.
func (l ShardLayout) Buckets() uint32 { return 1 << l.bits }

// BucketFromHash returns the shard bucket for a stream hash, in [0, Buckets()).
func (l ShardLayout) BucketFromHash(hash uint64) uint32 {
	// A shift by 64 yields 0 in Go, so the zero layout maps every hash to
	// bucket 0.
	return uint32(hash >> (64 - uint(l.bits)))
}

// BucketFromLabels returns the shard bucket for streamLabels, in [0, Buckets()).
func (l ShardLayout) BucketFromLabels(streamLabels labels.Labels) uint32 {
	return l.BucketFromHash(labels.StableHash(streamLabels))
}

// NewSortKey returns the sort key of streamLabels. Keys are stable across
// objects that use the same layout and schemaKey.
func (l ShardLayout) NewSortKey(streamLabels labels.Labels, schemaKey string) SortKey {
	hash := labels.StableHash(streamLabels)
	return SortKey{
		ShardBucket: l.BucketFromHash(hash),
		SchemaKey:   schemaKey,
		Hash:        hash,
		Labels:      streamLabels,
	}
}
