package deletion

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestTimeRangeDeletes(t *testing.T) {
	t.Run("returns deletes without line filters", func(t *testing.T) {
		deletes, err := TimeRangeDeletes([]*logproto.Delete{
			{Selector: `{foo="bar"}`, Start: 1, End: 2},
			{Selector: `{foo="bar", fizz="buzz"}`, Start: 3, End: 4},
		})
		require.NoError(t, err)
		require.Len(t, deletes, 2)
		require.Equal(t, int64(1), deletes[0].Start)
		require.Equal(t, int64(2), deletes[0].End)
		require.Len(t, deletes[1].Matchers, 2)
	})

	t.Run("skips deletes with line filters", func(t *testing.T) {
		deletes, err := TimeRangeDeletes([]*logproto.Delete{
			{Selector: `{foo="bar"} |= "some line"`, Start: 1, End: 2},
		})
		require.NoError(t, err)
		require.Empty(t, deletes)
	})

	t.Run("errors on an invalid selector", func(t *testing.T) {
		_, err := TimeRangeDeletes([]*logproto.Delete{
			{Selector: `not a selector`, Start: 1, End: 2},
		})
		require.Error(t, err)
	})
}

func TestDeletedIntervals(t *testing.T) {
	deletes, err := TimeRangeDeletes([]*logproto.Delete{
		{Selector: `{foo="bar"}`, Start: 10, End: 20},
		{Selector: `{foo="bar"}`, Start: 15, End: 30},
		{Selector: `{foo="bar"}`, Start: 50, End: 60},
		{Selector: `{fizz="buzz"}`, Start: 100, End: 200},
	})
	require.NoError(t, err)

	t.Run("merges overlapping intervals for a matching stream", func(t *testing.T) {
		intervals := DeletedIntervals(deletes, labels.FromStrings("foo", "bar"))
		require.Equal(t, []Interval{{Start: 10, End: 30}, {Start: 50, End: 60}}, intervals)
	})

	t.Run("returns nothing for a non-matching stream", func(t *testing.T) {
		require.Empty(t, DeletedIntervals(deletes, labels.FromStrings("ping", "pong")))
	})
}

func TestUndeletedFactor(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		from, through, minTime, maxTime int64
		deleted                         []Interval
		expected                        float64
	}{
		{name: "no deletes", from: 0, through: 100, minTime: 0, maxTime: 50, expected: 1},
		{name: "chunk fully deleted", from: 0, through: 100, minTime: 0, maxTime: 50, deleted: []Interval{{Start: 0, End: 50}}, expected: 0},
		{name: "half of the chunk deleted", from: 0, through: 100, minTime: 0, maxTime: 50, deleted: []Interval{{Start: 0, End: 25}}, expected: 0.5},
		{name: "delete outside the chunk", from: 0, through: 100, minTime: 0, maxTime: 50, deleted: []Interval{{Start: 60, End: 80}}, expected: 1},
		{name: "delete outside the query range", from: 0, through: 40, minTime: 0, maxTime: 50, deleted: []Interval{{Start: 45, End: 50}}, expected: 0.8},
		{name: "no overlap between chunk and query", from: 60, through: 100, minTime: 0, maxTime: 50, deleted: []Interval{{Start: 0, End: 50}}, expected: 0},
		{name: "single-entry chunk deleted", from: 0, through: 100, minTime: 50, maxTime: 50, deleted: []Interval{{Start: 40, End: 60}}, expected: 0},
		{name: "single-entry chunk not deleted", from: 0, through: 100, minTime: 50, maxTime: 50, deleted: []Interval{{Start: 60, End: 80}}, expected: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.InDelta(t, tc.expected, UndeletedFactor(tc.from, tc.through, tc.minTime, tc.maxTime, tc.deleted), 1e-9)
		})
	}
}
