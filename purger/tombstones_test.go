package purger

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTombstonesLoader(t *testing.T) {
	deleteRequestSelectors := []string{"foo"}
	metric, err := parser.ParseMetric(deleteRequestSelectors[0])
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
			deleteStore := setupTestDeleteStore(t)
			tombstonesLoader := NewTombstonesLoader(deleteStore, nil)

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

func TestTombstonesLoader_GetCacheGenNumber(t *testing.T) {
	s := &store{
		numbers: map[string]*cacheGenNumbers{
			"tenant-a": {
				results: "1000",
				store:   "2050",
			},
			"tenant-b": {
				results: "1050",
				store:   "2000",
			},
			"tenant-c": {
				results: "",
				store:   "",
			},
			"tenant-d": {
				results: "results-c",
				store:   "store-c",
			},
		},
	}
	tombstonesLoader := NewTombstonesLoader(s, nil)

	for _, tc := range []struct {
		name                          string
		expectedResultsCacheGenNumber string
		expectedStoreCacheGenNumber   string
		tenantIDs                     []string
	}{
		{
			name:                          "single tenant with numeric values",
			tenantIDs:                     []string{"tenant-a"},
			expectedResultsCacheGenNumber: "1000",
			expectedStoreCacheGenNumber:   "2050",
		},
		{
			name:                          "single tenant with non-numeric values",
			tenantIDs:                     []string{"tenant-d"},
			expectedResultsCacheGenNumber: "results-c",
			expectedStoreCacheGenNumber:   "store-c",
		},
		{
			name:                          "multiple tenants with numeric values",
			tenantIDs:                     []string{"tenant-a", "tenant-b"},
			expectedResultsCacheGenNumber: "1050",
			expectedStoreCacheGenNumber:   "2050",
		},
		{
			name:                          "multiple tenants with numeric and non-numeric values",
			tenantIDs:                     []string{"tenant-d", "tenant-c", "tenant-b", "tenant-a"},
			expectedResultsCacheGenNumber: "1050",
			expectedStoreCacheGenNumber:   "2050",
		},
		{
			name: "no tenants", // not really an expected call, edge case check to avoid any panics
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expectedResultsCacheGenNumber, tombstonesLoader.GetResultsCacheGenNumber(tc.tenantIDs))
			assert.Equal(t, tc.expectedStoreCacheGenNumber, tombstonesLoader.GetStoreCacheGenNumber(tc.tenantIDs))
		})
	}
}

func TestTombstonesReloadDoesntDeadlockOnFailure(t *testing.T) {
	s := &store{}
	tombstonesLoader := NewTombstonesLoader(s, nil)
	tombstonesLoader.getCacheGenNumbers("test")

	s.err = errors.New("error")
	require.NotNil(t, tombstonesLoader.reloadTombstones())

	s.err = nil
	require.NotNil(t, tombstonesLoader.getCacheGenNumbers("test2"))
}

type store struct {
	numbers map[string]*cacheGenNumbers
	err     error
}

func (f *store) getCacheGenerationNumbers(ctx context.Context, user string) (*cacheGenNumbers, error) {
	if f.numbers != nil {
		number, ok := f.numbers[user]
		if ok {
			return number, nil
		}
	}
	return &cacheGenNumbers{}, f.err
}

func (f *store) GetPendingDeleteRequestsForUser(ctx context.Context, id string) ([]DeleteRequest, error) {
	return nil, nil
}
