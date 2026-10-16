package streams

import (
	"fmt"
	"math/bits"

	"github.com/prometheus/prometheus/model/labels"
)

// maxShardBits is the largest bit count of a [ShardLayout]. It caps the bucket
// count at 65536, far above any useful shard factor, and keeps every bucket
// number well inside a uint32.
const maxShardBits = 16

// DefaultShardBits is the shard bit count of [DefaultShardLayout].
//
// Do not change it. Readers assume that every object uses the default
// layout, so a new value makes them mis-read every existing object. The
// per-tenant shard factor limit rejects other factors for the same reason.
const DefaultShardBits = 5

// DefaultShardBuckets is the bucket count of [DefaultShardLayout].
const DefaultShardBuckets = 1 << DefaultShardBits

// DefaultShardLayout returns the layout that readers assume for every object
// when they map query shards to buckets. Writers use it unless a tenant sets
// another shard factor.
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
// The zero value has one bucket. Bits is never more than 16.
type ShardLayout struct {
	bits uint8
}

// ShardLayoutFromBuckets returns the layout with the given bucket count. It
// returns an error if buckets is not a power of two from 1 to 65536.
func ShardLayoutFromBuckets(buckets uint32) (ShardLayout, error) {
	if buckets == 0 || buckets&(buckets-1) != 0 {
		return ShardLayout{}, fmt.Errorf("shard bucket count %d is not a power of two", buckets)
	}
	n := bits.TrailingZeros32(buckets)
	if n > maxShardBits {
		return ShardLayout{}, fmt.Errorf("shard bucket count %d is more than the maximum %d", buckets, 1<<maxShardBits)
	}
	return ShardLayout{bits: uint8(n)}, nil
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
