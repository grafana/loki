package streams

import "github.com/prometheus/prometheus/model/labels"

// DefaultShardBits is the shard bit count of [DefaultShardLayout].
const DefaultShardBits = 5

// DefaultShardLayout is the shard layout of every object that Loki writes.
var DefaultShardLayout = ShardLayout{bits: DefaultShardBits}

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

// NewSortKey returns the sort key for a stream in this layout.
func (l ShardLayout) NewSortKey(streamLabels labels.Labels, schemaKey string) SortKey {
	hash := labels.StableHash(streamLabels)
	return SortKey{
		ShardBucket: l.BucketFromHash(hash),
		SchemaKey:   schemaKey,
		Hash:        hash,
		Labels:      streamLabels,
	}
}
