package logsobj

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestRankStreams_SameLabelsShareID(t *testing.T) {
	ls := labels.FromStrings("app", "auth")
	other := labels.FromStrings("app", "web")
	a := map[int64]streams.Stream{
		2: {ID: 2, Labels: ls, ShardBucket: int64(streams.ShardBucket(ls))},
		7: {ID: 7, Labels: other, ShardBucket: int64(streams.ShardBucket(other))},
	}
	b := map[int64]streams.Stream{
		5: {ID: 5, Labels: ls.Copy(), ShardBucket: int64(streams.ShardBucket(ls))},
	}

	ranks, err := RankMixedStreams([]string{"label:app"}, a, b)
	require.NoError(t, err)

	leftID := ranks.Resolve(0, 2)
	rightID := ranks.Resolve(1, 5)
	left := ranks.ByID(leftID)
	right := ranks.ByID(rightID)
	require.Equal(t, left, right, "same labels across objects must share one key")
	require.Equal(t, left, ranks.ByID(leftID))
	require.NotEqual(t, leftID, ranks.Resolve(0, 7))

	count := ranks.Size()
	require.Equal(t, count, 2)
	for id := int64(2); id <= int64(count); id++ {
		prev := ranks.ByID(id - 1)
		curr := ranks.ByID(id)
		require.Negative(t, streams.CompareSortKey(prev, curr),
			"global stream IDs must increase in StreamOrderKey order")
	}
}
