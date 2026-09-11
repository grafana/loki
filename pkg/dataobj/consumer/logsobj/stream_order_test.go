package logsobj

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestCompareStreamOrderKey_HashCollisionUsesFullLabels(t *testing.T) {
	a := StreamOrderKey{Shard: 1, SchemaKey: "svc", Hash: 42, Labels: labels.FromStrings("app", "a")}
	b := StreamOrderKey{Shard: 1, SchemaKey: "svc", Hash: 42, Labels: labels.FromStrings("app", "b")}

	require.Negative(t, CompareStreamOrderKey(a, b))
	require.Positive(t, CompareStreamOrderKey(b, a))
}

func TestCompareStreamOrderKey_ShardPrecedesSchema(t *testing.T) {
	z := StreamOrderKey{Shard: 0, SchemaKey: "z", Hash: 1, Labels: labels.FromStrings("app", "z")}
	a := StreamOrderKey{Shard: 1, SchemaKey: "a", Hash: 1, Labels: labels.FromStrings("app", "a")}

	require.Negative(t, CompareStreamOrderKey(z, a))
	require.Positive(t, CompareStreamOrderKey(a, z))
}

func TestNewStreamOrderKey_UsesShardBucket(t *testing.T) {
	ls := labels.FromStrings("app", "auth")
	key, err := NewStreamOrderKey(ls, []string{"label:app"})
	require.NoError(t, err)
	require.Equal(t, streams.ShardBucket(ls), key.Shard)
	require.Equal(t, streams.ShardBucketFromHash(key.Hash), key.Shard)
	require.Equal(t, "auth", key.SchemaKey)
	require.Equal(t, labels.StableHash(ls), key.Hash)
}

func TestNewStreamRemap_FillsSortFromOneHash(t *testing.T) {
	ls := labels.FromStrings("app", "auth")
	r, err := newStreamRemap(ls, []string{"label:app"})
	require.NoError(t, err)
	require.Zero(t, r.newID)
	require.Equal(t, streams.ShardBucket(ls), r.Shard)
	require.Equal(t, "auth", r.Key)
	require.Equal(t, labels.StableHash(ls), r.Hash)
	require.Equal(t, streams.ShardBucketFromHash(r.Hash), r.Shard)
}
