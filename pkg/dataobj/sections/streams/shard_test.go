package streams

import (
	"fmt"
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

func TestShardLayout_BucketRange(t *testing.T) {
	for _, buckets := range []uint32{1, 2, 8, 32, 64, 1 << maxShardBits} {
		t.Run(fmt.Sprintf("maps the whole hash space to every bucket of a layout of %d buckets", buckets), func(t *testing.T) {
			layout, err := ShardLayoutFromBuckets(buckets)
			require.NoError(t, err)

			from, to := layout.BucketRange(HashRange{From: 0, To: math.MaxUint64})
			require.Equal(t, uint32(0), from)
			require.Equal(t, buckets-1, to)
		})
	}

	t.Run("maps a range inside one bucket to that bucket", func(t *testing.T) {
		from, to := DefaultShardLayout().BucketRange(HashRange{From: 1<<59 + 5, To: 1<<59 + 100})
		require.Equal(t, uint32(1), from)
		require.Equal(t, uint32(1), to)
	})

	t.Run("maps a range that ends on the first hash of the next bucket to both buckets", func(t *testing.T) {
		from, to := DefaultShardLayout().BucketRange(HashRange{From: 1 << 59, To: 2 << 59})
		require.Equal(t, uint32(1), from)
		require.Equal(t, uint32(2), to)
	})

	t.Run("maps a range that ends on the last hash of a bucket to that bucket", func(t *testing.T) {
		from, to := DefaultShardLayout().BucketRange(HashRange{From: 1 << 59, To: 2<<59 - 1})
		require.Equal(t, uint32(1), from)
		require.Equal(t, uint32(1), to)
	})

	t.Run("maps the hashes of shard 3 of 64 to buckets 6 and 7 of a layout of 128 buckets", func(t *testing.T) {
		layout, err := ShardLayoutFromBuckets(128)
		require.NoError(t, err)

		from, to := layout.BucketRange(HashRange{From: 3 << 58, To: 4<<58 - 1})
		require.Equal(t, uint32(6), from)
		require.Equal(t, uint32(7), to)
	})

	t.Run("maps a range to one bucket in the zero layout", func(t *testing.T) {
		from, to := ShardLayout{}.BucketRange(HashRange{From: 3 << 58, To: 4<<58 - 1})
		require.Equal(t, uint32(0), from)
		require.Equal(t, uint32(0), to)
	})
}

func TestShardLayout_CoversAllShardBuckets(t *testing.T) {
	for _, buckets := range []uint32{1, 2, 32, 1 << maxShardBits} {
		layout := MustShardLayoutFromBuckets(buckets)

		t.Run(fmt.Sprintf("is true for the range from the first to the last bucket of a layout of %d buckets", buckets), func(t *testing.T) {
			require.True(t, layout.CoversAllShardBuckets(0, buckets-1))
		})
	}

	t.Run("is false when the range starts after the first bucket", func(t *testing.T) {
		require.False(t, DefaultShardLayout().CoversAllShardBuckets(1, DefaultShardLayout().Buckets()-1))
	})

	t.Run("is false when the range ends before the last bucket", func(t *testing.T) {
		require.False(t, DefaultShardLayout().CoversAllShardBuckets(0, DefaultShardLayout().Buckets()-2))
	})

	t.Run("is false when the range ends past the last bucket", func(t *testing.T) {
		require.False(t, DefaultShardLayout().CoversAllShardBuckets(0, DefaultShardLayout().Buckets()))
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

func TestMustShardLayoutFromBuckets(t *testing.T) {
	t.Run("returns the layout whose bucket count is the input", func(t *testing.T) {
		for _, buckets := range []uint32{1, 2, 32, 1 << maxShardBits} {
			require.Equal(t, buckets, MustShardLayoutFromBuckets(buckets).Buckets())
		}
	})

	t.Run("panics with the error of ShardLayoutFromBuckets when the count is not a power of two", func(t *testing.T) {
		require.PanicsWithError(t, "shard bucket count 24 is not a power of two", func() {
			MustShardLayoutFromBuckets(24)
		})
	})

	t.Run("panics with the error of ShardLayoutFromBuckets when the count is above the maximum", func(t *testing.T) {
		require.PanicsWithError(t, "shard bucket count 131072 is more than the maximum 65536", func() {
			MustShardLayoutFromBuckets(1 << (maxShardBits + 1))
		})
	})
}
