package compactor

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

func newTestJobEnv(bucket objstore.Bucket) jobEnv {
	return jobEnv{tenant: "acme", logger: log.NewNopLogger(), bucket: bucket}
}

func TestRequireSameLogSections(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	seedTaskIndex(t, bucket, "indexes/old-0", map[string]int64{"logs/a": 100})
	seedTaskIndex(t, bucket, "indexes/old-1", map[string]int64{"logs/b": 200})
	seedTaskIndex(t, bucket, "indexes/same", map[string]int64{"logs/a": 100, "logs/b": 200})
	seedTaskIndex(t, bucket, "indexes/missing", map[string]int64{"logs/a": 100})
	seedTaskIndex(t, bucket, "indexes/resized", map[string]int64{"logs/a": 100, "logs/b": 201})
	buildOverlappingPostingsIndex(t.Context(), t, bucket, "acme", "indexes/no-stats")
	env := newTestJobEnv(bucket)
	old := []string{"indexes/old-0", "indexes/old-1"}

	t.Run("returns no error when the new indexes hold the same log sections and sizes", func(t *testing.T) {
		require.NoError(t, requireSameLogSections(t.Context(), env, old, []string{"indexes/same"}))
	})

	t.Run("returns an error when a new index lacks a log section of the old indexes", func(t *testing.T) {
		require.Error(t, requireSameLogSections(t.Context(), env, old, []string{"indexes/missing"}))
	})

	t.Run("returns an error when the size of a log section differs", func(t *testing.T) {
		require.Error(t, requireSameLogSections(t.Context(), env, old, []string{"indexes/resized"}))
	})

	t.Run("returns an error when the new indexes hold no stats rows", func(t *testing.T) {
		require.Error(t, requireSameLogSections(t.Context(), env, old, []string{"indexes/no-stats"}))
	})

	t.Run("returns an error when there are no new indexes", func(t *testing.T) {
		require.Error(t, requireSameLogSections(t.Context(), env, old, nil))
	})
}

func TestRequireLogBytes(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	seedTaskIndex(t, bucket, "indexes/out-0", map[string]int64{"logs/a": 100})
	seedTaskIndex(t, bucket, "indexes/out-1", map[string]int64{"logs/b": 200})
	env := newTestJobEnv(bucket)

	t.Run("returns no error when the indexes count the input bytes", func(t *testing.T) {
		require.NoError(t, requireLogBytes(t.Context(), env, []string{"indexes/out-0", "indexes/out-1"}, 300))
	})

	t.Run("returns an error when the indexes count fewer bytes than the input", func(t *testing.T) {
		require.Error(t, requireLogBytes(t.Context(), env, []string{"indexes/out-0"}, 300))
	})
}
