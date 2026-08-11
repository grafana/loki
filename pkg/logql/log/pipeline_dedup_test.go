package log

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

// TestForStreamIgnoresStreamShardLabels verifies that streams which automatic
// stream sharding split across shards (via __stream_shard__ and __time_shard__)
// share a single query-time identity: one stream hash and one set of base
// labels, regardless of which shard is seen first. Streams that differ in a
// real label, or in a reserved label that identifies a distinct stream (such as
// __aggregated_metric__ or __pattern__), must keep distinct hashes.
func TestForStreamIgnoresStreamShardLabels(t *testing.T) {
	base := labels.FromStrings("app", "foo", "namespace", "prod")
	streamShard0 := labels.FromStrings("__stream_shard__", "0", "app", "foo", "namespace", "prod")
	streamShard1 := labels.FromStrings("__stream_shard__", "1", "app", "foo", "namespace", "prod")
	timeShard := labels.FromStrings("__stream_shard__", "1", "__time_shard__", "5", "app", "foo", "namespace", "prod")
	different := labels.FromStrings("app", "bar", "namespace", "prod")
	aggregated := labels.FromStrings("__aggregated_metric__", "svc", "app", "foo", "namespace", "prod")
	pattern := labels.FromStrings("__pattern__", "level=<_>", "app", "foo", "namespace", "prod")

	t.Run("pipelines", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			p    Pipeline
		}{
			{"noop", NewNoopPipeline()},
			{"stages", NewPipeline([]Stage{NoopStage})},
		} {
			t.Run(tc.name, func(t *testing.T) {
				hash := func(ls labels.Labels) uint64 { return tc.p.ForStream(ls).BaseLabels().Hash() }

				require.Equal(t, hash(base), hash(streamShard0), "a sharded stream must hash like its unsharded form")
				require.Equal(t, hash(streamShard0), hash(streamShard1), "shards of one stream must share a hash")
				require.Equal(t, hash(streamShard0), hash(timeShard), "time-sharding must not change the hash either")

				require.NotEqual(t, hash(base), hash(different), "genuinely different streams must still differ")
				require.NotEqual(t, hash(base), hash(aggregated), "__aggregated_metric__ identifies a distinct stream; it must not be treated as a shard label")
				require.NotEqual(t, hash(base), hash(pattern), "__pattern__ identifies a distinct stream; it must not be treated as a shard label")
			})
		}
	})

	t.Run("sample extractors", func(t *testing.T) {
		lineEx, err := NewLineSampleExtractor(CountExtractor, nil, nil, false, false)
		require.NoError(t, err)
		labelEx, err := LabelExtractorWithStages("foo", ConvertFloat, nil, false, false, nil, NoopStage)
		require.NoError(t, err)

		for _, tc := range []struct {
			name string
			ex   SampleExtractor
		}{
			{"line", lineEx},
			{"label", labelEx},
		} {
			t.Run(tc.name, func(t *testing.T) {
				hash := func(ls labels.Labels) uint64 { return tc.ex.ForStream(ls).BaseLabels().Hash() }

				require.Equal(t, hash(base), hash(streamShard0), "a sharded stream must hash like its unsharded form")
				require.Equal(t, hash(streamShard0), hash(streamShard1), "shards of one stream must share a hash")
				require.Equal(t, hash(streamShard0), hash(timeShard), "time-sharding must not change the hash either")

				require.NotEqual(t, hash(base), hash(different), "genuinely different streams must still differ")
				require.NotEqual(t, hash(base), hash(aggregated), "__aggregated_metric__ identifies a distinct stream; it must not be treated as a shard label")
			})
		}
	})

	// A fresh pipeline whose first stream is a shard must produce the unsharded
	// base labels, not the shard's own labels, so the identity does not depend
	// on which shard is seen first.
	t.Run("base labels are the unsharded form regardless of order", func(t *testing.T) {
		sp1 := NewNoopPipeline().ForStream(streamShard1)
		require.Equal(t, base.String(), sp1.BaseLabels().String())

		sp0 := NewNoopPipeline().ForStream(streamShard0)
		require.Equal(t, base.String(), sp0.BaseLabels().String())

		// Result labels feed the streams returned to clients and the line hash
		// that deduplicates metric samples, so separate queriers seeing
		// different shards of a stream must produce identical result labels.
		_, res0, ok := sp0.ProcessString(0, "line", labels.EmptyLabels())
		require.True(t, ok)
		_, res1, ok := sp1.ProcessString(0, "line", labels.EmptyLabels())
		require.True(t, ok)
		require.Equal(t, res0.String(), res1.String())
	})
}

// BenchmarkForStreamShardStrip reports the per-ForStream cost of dropping the
// shard labels. The unsharded case (the common one) only checks for the two
// labels; the sharded case additionally rebuilds the label set without them.
func BenchmarkForStreamShardStrip(b *testing.B) {
	unsharded := labels.FromStrings(
		"app", "foo", "namespace", "prod", "cluster", "dev",
		"pod", "foo-0", "container", "app", "level", "info",
	)
	sharded := labels.FromStrings(
		"__stream_shard__", "3", "app", "foo", "namespace", "prod", "cluster", "dev",
		"pod", "foo-0", "container", "app", "level", "info",
	)

	p := NewNoopPipeline()
	b.Run("unsharded", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = p.ForStream(unsharded)
		}
	})
	b.Run("sharded", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = p.ForStream(sharded)
		}
	})
}
