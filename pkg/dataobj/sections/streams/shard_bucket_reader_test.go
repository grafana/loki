package streams_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestReadShardBuckets(t *testing.T) {
	sec := buildStreamsSection(t, 1, 0) // streamsTestdata: apps foo, bar, baz → stream IDs 1, 2, 3

	t.Run("returns the shard bucket of every requested stream", func(t *testing.T) {
		buckets, ok, err := streams.ReadShardBuckets(t.Context(), sec, []int64{1, 2, 3})
		require.NoError(t, err)
		require.True(t, ok, "the section carries the __shard_bucket__ column")
		require.Equal(t, map[int64]uint64{
			1: uint64(shardForApp("foo")),
			2: uint64(shardForApp("bar")),
			3: uint64(shardForApp("baz")),
		}, buckets)
	})

	t.Run("filters to the requested IDs; a missing ID is absent", func(t *testing.T) {
		buckets, ok, err := streams.ReadShardBuckets(t.Context(), sec, []int64{2, 999})
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, map[int64]uint64{2: uint64(shardForApp("bar"))}, buckets)
	})

	t.Run("no requested IDs returns an empty map", func(t *testing.T) {
		buckets, ok, err := streams.ReadShardBuckets(t.Context(), sec, nil)
		require.NoError(t, err)
		require.True(t, ok)
		require.Empty(t, buckets)
	})
}
