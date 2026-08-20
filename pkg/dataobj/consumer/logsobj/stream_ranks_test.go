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

	ranks, err := RankStreams([]string{"label:app"}, a, b)
	require.NoError(t, err)

	left := ranks.Resolve(0, 2)
	right := ranks.Resolve(1, 5)
	require.Equal(t, left.Stream.ID, right.Stream.ID, "same labels across objects must share one ID")
	require.Equal(t, left, ranks.ByID(left.Stream.ID))
	require.NotEqual(t, left.Stream.ID, ranks.Resolve(0, 7).Stream.ID)
}
