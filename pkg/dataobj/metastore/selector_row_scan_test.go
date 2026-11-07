package metastore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

func TestSelectorRowScan(t *testing.T) {
	t.Run("fails to cut the matches of each bucket from a scan of a bucket range", func(t *testing.T) {
		scan := &selectorRowScan{bucketRange: &postings.ShardBucketRange{From: 1, To: 2}, bucketRangesChecked: true}

		_, err := scan.selectorMatchesForEachBucket(4)

		require.EqualError(t, err, "the scan read only the shard buckets 1 to 2")
	})

	t.Run("fails to cut the matches of each bucket from a scan that did not check the shard bucket ranges", func(t *testing.T) {
		scan := &selectorRowScan{bucketRangesChecked: false}

		_, err := scan.selectorMatchesForEachBucket(4)

		require.EqualError(t, err, "the scan did not check that its sections record the shard bucket range of their rows")
	})
}
