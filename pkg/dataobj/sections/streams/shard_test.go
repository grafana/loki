package streams

import (
	"math"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestShardLayout(t *testing.T) {
	t.Run("default layout has 32 buckets from the top 5 hash bits", func(t *testing.T) {
		require.Equal(t, 5, DefaultShardLayout().Bits())
		require.Equal(t, uint32(32), DefaultShardLayout().Buckets())
		require.Equal(t, uint32(0), DefaultShardLayout().BucketFromHash(0))
		require.Equal(t, uint32(0), DefaultShardLayout().BucketFromHash(1<<59-1))
		require.Equal(t, uint32(1), DefaultShardLayout().BucketFromHash(1<<59))
		require.Equal(t, uint32(31), DefaultShardLayout().BucketFromHash(math.MaxUint64))
	})

	t.Run("zero layout maps every hash to bucket 0", func(t *testing.T) {
		var layout ShardLayout
		require.Equal(t, 0, layout.Bits())
		require.Equal(t, uint32(1), layout.Buckets())
		require.Equal(t, uint32(0), layout.BucketFromHash(0))
		require.Equal(t, uint32(0), layout.BucketFromHash(math.MaxUint64))
	})

	t.Run("labels bucket equals the bucket of the stable hash", func(t *testing.T) {
		ls := labels.FromStrings("app", "auth")
		require.Equal(t, DefaultShardLayout().BucketFromHash(labels.StableHash(ls)), DefaultShardLayout().BucketFromLabels(ls))
	})

	t.Run("sort key takes its bucket from the layout", func(t *testing.T) {
		ls := labels.FromStrings("app", "api", "cluster", "prod")
		hash := labels.StableHash(ls)
		layout := ShardLayout{bits: 1}

		key := layout.NewSortKey(ls, "fake-schema-key")

		require.Equal(t, layout.BucketFromHash(hash), key.ShardBucket)
		require.Equal(t, "fake-schema-key", key.SchemaKey)
		require.Equal(t, hash, key.Hash)
		require.Equal(t, ls, key.Labels)
	})
}

func TestShardLayoutFromBuckets(t *testing.T) {
	t.Run("returns the layout whose bucket count is the input", func(t *testing.T) {
		for _, buckets := range []uint32{1, 2, 32, 1 << maxShardBits} {
			layout, err := ShardLayoutFromBuckets(buckets)
			require.NoError(t, err)
			require.Equal(t, buckets, layout.Buckets())
		}
	})

	t.Run("returns the default layout for the default bucket count", func(t *testing.T) {
		layout, err := ShardLayoutFromBuckets(DefaultShardBuckets)
		require.NoError(t, err)
		require.Equal(t, DefaultShardLayout(), layout)
	})

	t.Run("returns an error when the count is zero", func(t *testing.T) {
		_, err := ShardLayoutFromBuckets(0)
		require.ErrorContains(t, err, "not a power of two")
	})

	t.Run("returns an error when the count is not a power of two", func(t *testing.T) {
		_, err := ShardLayoutFromBuckets(24)
		require.ErrorContains(t, err, "not a power of two")
	})

	t.Run("returns an error when the count is above the maximum", func(t *testing.T) {
		_, err := ShardLayoutFromBuckets(1 << (maxShardBits + 1))
		require.ErrorContains(t, err, "more than the maximum")
	})
}
