package util

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestHasStreamShardLabels(t *testing.T) {
	require.False(t, HasStreamShardLabels(labels.FromStrings("app", "foo", "namespace", "prod")))
	require.True(t, HasStreamShardLabels(labels.FromStrings("app", "foo", "__stream_shard__", "1")))
	require.True(t, HasStreamShardLabels(labels.FromStrings("app", "foo", "__time_shard__", "42")))

	// Reserved labels that identify a distinct stream are not shard labels.
	require.False(t, HasStreamShardLabels(labels.FromStrings("app", "foo", "__aggregated_metric__", "svc")))
	require.False(t, HasStreamShardLabels(labels.FromStrings("app", "foo", "__pattern__", "p")))
}

func TestLabelsWithoutStreamShards(t *testing.T) {
	hash := func(ls labels.Labels) uint64 {
		return labels.StableHash(LabelsWithoutStreamShards(ls))
	}

	base := labels.FromStrings("app", "foo", "namespace", "prod")
	streamShard0 := labels.FromStrings("__stream_shard__", "0", "app", "foo", "namespace", "prod")
	streamShard1 := labels.FromStrings("__stream_shard__", "1", "app", "foo", "namespace", "prod")
	timeShard := labels.FromStrings("__stream_shard__", "1", "__time_shard__", "42", "app", "foo", "namespace", "prod")
	different := labels.FromStrings("app", "bar", "namespace", "prod")

	// The shard labels are dropped, so shards of one stream become identical.
	require.Equal(t, base, LabelsWithoutStreamShards(streamShard0))
	require.Equal(t, base, LabelsWithoutStreamShards(timeShard))

	// And therefore hash equally, while a real label difference still does not.
	require.Equal(t, hash(base), hash(streamShard0))
	require.Equal(t, hash(streamShard0), hash(streamShard1))
	require.Equal(t, hash(streamShard0), hash(timeShard))
	require.NotEqual(t, hash(base), hash(different))

	// A stream without shard labels is returned unchanged.
	require.Equal(t, base, LabelsWithoutStreamShards(base))

	// Reserved labels that merely start with "__" but identify a distinct stream
	// must be preserved. Stripping them would collapse different streams into one
	// identity and deduplicate lines that are not actually duplicates.
	for _, name := range []string{"__aggregated_metric__", "__pattern__", "__backfill_shard__"} {
		ls := labels.NewBuilder(base).Set(name, "x").Labels()
		require.Equal(t, ls, LabelsWithoutStreamShards(ls), "%s must not be stripped", name)
		require.NotEqual(t, hash(base), hash(ls), "%s identifies a distinct stream", name)
	}
}
