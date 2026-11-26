package streams

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestBuilder_Record(t *testing.T) {
	t.Run("assigns shard buckets from the builder's layout", func(t *testing.T) {
		layout := ShardLayout{bits: 1}
		b := NewBuilder(nil, 1024, 0, layout)

		var differsFromDefault bool
		for i := range 16 {
			ls := labels.FromStrings("app", fmt.Sprintf("app-%d", i))
			b.Record(ls, time.Unix(0, 0), 1)

			if layout.BucketFromLabels(ls) != DefaultShardLayout().BucketFromLabels(ls) {
				differsFromDefault = true
			}
		}
		require.True(t, differsFromDefault, "the streams must have a different bucket in the default layout, or a builder that ignores its layout could go unseen")

		for _, stream := range b.ordered {
			require.Equal(t, int64(layout.BucketFromLabels(stream.Labels)), stream.ShardBucket)
		}
	})
}
