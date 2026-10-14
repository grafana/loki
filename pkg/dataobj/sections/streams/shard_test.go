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
