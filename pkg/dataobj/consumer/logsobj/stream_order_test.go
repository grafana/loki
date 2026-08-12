package logsobj

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestCompareStreamOrderKey_HashCollisionUsesFullLabels(t *testing.T) {
	a := StreamOrderKey{
		SchemaKey: "service",
		Hash:      42,
		Labels:    labels.FromStrings("app", "a"),
	}
	b := StreamOrderKey{
		SchemaKey: "service",
		Hash:      42,
		Labels:    labels.FromStrings("app", "b"),
	}
	require.Negative(t, CompareStreamOrderKey(a, b))
	require.Positive(t, CompareStreamOrderKey(b, a))
}

func TestCompareStreamOrderKey_ShardPrecedesSchema(t *testing.T) {
	z, err := NewStreamOrderKey(labels.FromStrings("app", "z"), []string{"label:app"})
	require.NoError(t, err)
	a, err := NewStreamOrderKey(labels.FromStrings("app", "a"), []string{"label:app"})
	require.NoError(t, err)

	require.Less(t, z.Shard, a.Shard)
	require.Negative(t, CompareStreamOrderKey(z, a))
}
