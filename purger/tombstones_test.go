package purger

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
)

func TestTombstonesLoader(t *testing.T) {
	deleteRequestSelectors := []string{"foo"}
	metric, err := promql.ParseMetric(deleteRequestSelectors[0])
	require.NoError(t, err)

	for _, tc := range []struct {
		name                   string
		deleteRequestIntervals []model.Interval
		queryForInterval       model.Interval
		expectedIntervals      []model.Interval
	}{
		{
			name:             "no delete requests",
			queryForInterval: model.Interval{End: modelTimeDay},
		},
		{
			name: "query out of range of delete requests",
			deleteRequestIntervals: []model.Interval{
				{End: modelTimeDay},
			},
			queryForInterval: model.Interval{Start: modelTimeDay.Add(time.Hour), End: modelTimeDay * 2},
		},
		{
			name: "no overlap but disjoint deleted intervals",
			deleteRequestIntervals: []model.Interval{
				{End: modelTimeDay},
				{Start: modelTimeDay.Add(time.Hour), End: modelTimeDay.Add(2 * time.Hour)},
			},
			queryForInterval: model.Interval{End: modelTimeDay.Add(2 * time.Hour)},
			expectedIntervals: []model.Interval{
				{End: modelTimeDay},
				{Start: modelTimeDay.Add(time.Hour), End: modelTimeDay.Add(2 * time.Hour)},
			},
		},
		{
			name: "no overlap but continuous deleted intervals",
			deleteRequestIntervals: []model.Interval{
				{End: modelTimeDay},
				{Start: modelTimeDay, End: modelTimeDay.Add(2 * time.Hour)},
			},
			queryForInterval: model.Interval{End: modelTimeDay.Add(2 * time.Hour)},
			expectedIntervals: []model.Interval{
				{End: modelTimeDay.Add(2 * time.Hour)},
			},
		},
		{
			name: "some overlap in deleted intervals",
			deleteRequestIntervals: []model.Interval{
				{End: modelTimeDay},
				{Start: modelTimeDay.Add(-time.Hour), End: modelTimeDay.Add(2 * time.Hour)},
			},
			queryForInterval: model.Interval{End: modelTimeDay.Add(2 * time.Hour)},
			expectedIntervals: []model.Interval{
				{End: modelTimeDay.Add(2 * time.Hour)},
			},
		},
		{
			name: "complete overlap in deleted intervals",
			deleteRequestIntervals: []model.Interval{
				{End: modelTimeDay},
				{End: modelTimeDay},
			},
			queryForInterval: model.Interval{End: modelTimeDay.Add(2 * time.Hour)},
			expectedIntervals: []model.Interval{
				{End: modelTimeDay},
			},
		},
		{
			name: "mix of overlaps in deleted intervals",
			deleteRequestIntervals: []model.Interval{
				{End: modelTimeDay},
				{End: modelTimeDay},
				{Start: modelTimeDay.Add(time.Hour), End: modelTimeDay.Add(2 * time.Hour)},
				{Start: modelTimeDay.Add(2 * time.Hour), End: modelTimeDay.Add(24 * time.Hour)},
				{Start: modelTimeDay.Add(23 * time.Hour), End: modelTimeDay * 3},
			},
			queryForInterval: model.Interval{End: modelTimeDay * 10},
			expectedIntervals: []model.Interval{
				{End: modelTimeDay},
				{Start: modelTimeDay.Add(time.Hour), End: modelTimeDay * 3},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deleteStore, err := setupTestDeleteStore()
			require.NoError(t, err)

			tombstonesLoader := NewTombstonesLoader(deleteStore)

			// add delete requests
			for _, interval := range tc.deleteRequestIntervals {
				err := deleteStore.AddDeleteRequest(context.Background(), userID, interval.Start, interval.End, deleteRequestSelectors)
				require.NoError(t, err)
			}

			// get all delete requests for user
			tombstonesAnalyzer, err := tombstonesLoader.GetPendingTombstones(userID)
			require.NoError(t, err)

			// verify whether number of delete requests is same as what we added
			require.Equal(t, len(tc.deleteRequestIntervals), tombstonesAnalyzer.Len())

			// if we are expecting to get deleted intervals then HasTombstonesForInterval should return true else false
			expectedHasTombstonesForInterval := true
			if len(tc.expectedIntervals) == 0 {
				expectedHasTombstonesForInterval = false
			}

			hasTombstonesForInterval := tombstonesAnalyzer.HasTombstonesForInterval(tc.queryForInterval.Start, tc.queryForInterval.End)
			require.Equal(t, expectedHasTombstonesForInterval, hasTombstonesForInterval)

			// get deleted intervals
			intervals := tombstonesAnalyzer.GetDeletedIntervals(metric, tc.queryForInterval.Start, tc.queryForInterval.End)
			require.Equal(t, len(tc.expectedIntervals), len(intervals))

			// verify whether we got expected intervals back
			for i, interval := range intervals {
				require.Equal(t, tc.expectedIntervals[i].Start, interval.Start)
				require.Equal(t, tc.expectedIntervals[i].End, interval.End)
			}
		})
	}
}
