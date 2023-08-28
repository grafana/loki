package deletion

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/deletionmode"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/retention"
	"github.com/grafana/loki/pkg/util/filter"
)

const testUserID = "test-user"

func TestDeleteRequestsManager_Expired(t *testing.T) {
	type resp struct {
		isExpired      bool
		expectedFilter filter.Func
	}

	now := model.Now()
	lblFoo, err := syntax.ParseLabels(`{foo="bar"}`)
	require.NoError(t, err)
	streamSelectorWithLineFilters := lblFoo.String() + `|="fizz"`
	streamSelectorWithNonIndexedLabelsFilters := lblFoo.String() + `| ping="pong"`
	streamSelectorWithLineAndNonIndexedLabelsFilters := lblFoo.String() + `| ping="pong" |= "fizz"`

	chunkEntry := retention.ChunkEntry{
		ChunkRef: retention.ChunkRef{
			UserID:  []byte(testUserID),
			From:    now.Add(-12 * time.Hour),
			Through: now.Add(-time.Hour),
		},
		Labels: lblFoo,
	}

	for _, tc := range []struct {
		name                        string
		deletionMode                deletionmode.Mode
		deleteRequestsFromStore     []DeleteRequest
		batchSize                   int
		expectedResp                resp
		expectedDeletionRangeByUser map[string]model.Interval
	}{
		{
			name:         "no delete requests",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			expectedResp: resp{
				isExpired: false,
			},
		},
		{
			name:         "no relevant delete requests",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    "different-user",
					Query:     lblFoo.String(),
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: false,
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				"different-user": {
					Start: now.Add(-24 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "no relevant delete requests",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    "different-user",
					Query:     lblFoo.String(),
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: false,
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				"different-user": {
					Start: now.Add(-24 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "delete request not matching labels",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     `{fizz="buzz"}`,
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: false,
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-24 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "whole chunk deleted by single request",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-24 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "whole chunk deleted by single request with line filters",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, s string, _ ...labels.Label) bool {
					return strings.Contains(s, "fizz")
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-24 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "whole chunk deleted by single request with non-indexed labels filters",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithNonIndexedLabelsFilters,
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, s string, nonIndexedLabels ...labels.Label) bool {
					return labels.Labels(nonIndexedLabels).Get(lblPing) == lblPong
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-24 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "whole chunk deleted by single request with line and non-indexed labels filters",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineAndNonIndexedLabelsFilters,
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, s string, nonIndexedLabels ...labels.Label) bool {
					return labels.Labels(nonIndexedLabels).Get(lblPing) == lblPong && strings.Contains(s, "fizz")
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-24 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "deleted interval out of range",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
				},
			},
			expectedResp: resp{
				isExpired: false,
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-48 * time.Hour),
					End:   now.Add(-24 * time.Hour),
				},
			},
		},
		{
			name:         "deleted interval out of range(with multiple user requests)",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
				},
				{
					UserID:    "different-user",
					Query:     lblFoo.String(),
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: false,
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-48 * time.Hour),
					End:   now.Add(-24 * time.Hour),
				},
				"different-user": {
					Start: now.Add(-24 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "multiple delete requests with one deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-48 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "multiple delete requests with line filters and one deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, s string, _ ...labels.Label) bool {
					return strings.Contains(s, "fizz")
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-48 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "multiple delete requests with non-indexed labels filters and one deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithNonIndexedLabelsFilters,
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithNonIndexedLabelsFilters,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, s string, nonIndexedLabels ...labels.Label) bool {
					return labels.Labels(nonIndexedLabels).Get(lblPing) == lblPong
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-48 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "multiple delete requests causing multiple holes",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-10 * time.Hour),
					EndTime:   now.Add(-8 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-5 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-2 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, s string, _ ...labels.Label) bool {
					tsUnixNano := ts.UnixNano()
					if (now.Add(-13*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.Add(-11*time.Hour).UnixNano()) ||
						(now.Add(-10*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.Add(-8*time.Hour).UnixNano()) ||
						(now.Add(-6*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.Add(-5*time.Hour).UnixNano()) ||
						(now.Add(-2*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.UnixNano()) {
						return true
					}
					return false
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-13 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "multiple overlapping requests deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-6 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-8 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, s string, _ ...labels.Label) bool {
					return true
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-13 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "multiple overlapping requests with line filters deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-6 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-8 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, s string, _ ...labels.Label) bool {
					return strings.Contains(s, "fizz")
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-13 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "multiple overlapping requests with non-indexed labels filters deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithNonIndexedLabelsFilters,
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-6 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithNonIndexedLabelsFilters,
					StartTime: now.Add(-8 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, s string, nonIndexedLabels ...labels.Label) bool {
					return labels.Labels(nonIndexedLabels).Get(lblPing) == lblPong
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-13 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "multiple non-overlapping requests deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-6*time.Hour) - 1,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-4*time.Hour) - 1,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-4 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, s string, _ ...labels.Label) bool {
					return true
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-12 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "multiple non-overlapping requests with line filter deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-6*time.Hour) - 1,
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-4*time.Hour) - 1,
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-4 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, s string, _ ...labels.Label) bool {
					return strings.Contains(s, "fizz")
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-12 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "multiple non-overlapping requests with non-indexed labels filter deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithNonIndexedLabelsFilters,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-6*time.Hour) - 1,
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithNonIndexedLabelsFilters,
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-4*time.Hour) - 1,
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithNonIndexedLabelsFilters,
					StartTime: now.Add(-4 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, s string, nonIndexedLabels ...labels.Label) bool {
					return labels.Labels(nonIndexedLabels).Get(lblPing) == lblPong
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-12 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:         "deletes are disabled",
			deletionMode: deletionmode.Disabled,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-10 * time.Hour),
					EndTime:   now.Add(-8 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-5 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-2 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: false,
			},
		},
		{
			name:         "deletes are `filter-only`",
			deletionMode: deletionmode.FilterOnly,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-10 * time.Hour),
					EndTime:   now.Add(-8 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-5 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-2 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: false,
			},
		},
		{
			name:         "Deletes are sorted by start time and limited by batch size",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    2,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-2 * time.Hour),
					EndTime:   now,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-5 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-10 * time.Hour),
					EndTime:   now.Add(-8 * time.Hour),
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, s string, _ ...labels.Label) bool {
					tsUnixNano := ts.UnixNano()
					if (now.Add(-13*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.Add(-11*time.Hour).UnixNano()) ||
						(now.Add(-10*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.Add(-8*time.Hour).UnixNano()) {
						return true
					}

					return false
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-13 * time.Hour),
					End:   now.Add(-8 * time.Hour),
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := NewDeleteRequestsManager(&mockDeleteRequestsStore{deleteRequests: tc.deleteRequestsFromStore}, time.Hour, tc.batchSize, &fakeLimits{mode: tc.deletionMode.String()}, nil)
			require.NoError(t, mgr.loadDeleteRequestsToProcess())

			for _, deleteRequests := range mgr.deleteRequestsToProcess {
				for _, dr := range deleteRequests.requests {
					require.EqualValues(t, 0, dr.DeletedLines)
				}
			}

			isExpired, filterFunc := mgr.Expired(chunkEntry, model.Now())
			require.Equal(t, tc.expectedResp.isExpired, isExpired)
			if tc.expectedResp.expectedFilter == nil {
				require.Nil(t, filterFunc)
				return
			}
			require.NotNil(t, filterFunc)

			for start := chunkEntry.From; start <= chunkEntry.Through; start = start.Add(time.Minute) {
				line := "foo bar"
				if start.Time().Minute()%2 == 1 {
					line = "fizz buzz"
				}
				// mix of empty, ding=dong and ping=pong as non-indexed labels
				var nonIndexedLabels []labels.Label
				if start.Time().Minute()%3 == 0 {
					nonIndexedLabels = []labels.Label{{Name: lblPing, Value: lblPong}}
				} else if start.Time().Minute()%2 == 0 {
					nonIndexedLabels = []labels.Label{{Name: "ting", Value: "tong"}}
				}
				require.Equal(t, tc.expectedResp.expectedFilter(start.Time(), line, nonIndexedLabels...), filterFunc(start.Time(), line, nonIndexedLabels...), "line", line, "time", start.Time(), "now", now.Time())
			}

			require.Equal(t, len(tc.expectedDeletionRangeByUser), len(mgr.deleteRequestsToProcess))
			for userID, dr := range tc.expectedDeletionRangeByUser {
				require.Equal(t, dr, mgr.deleteRequestsToProcess[userID].requestsInterval)
			}
		})
	}
}

func TestDeleteRequestsManager_IntervalMayHaveExpiredChunks(t *testing.T) {
	tt := []struct {
		deleteRequestsFromStore []DeleteRequest
		hasChunks               bool
		user                    string
	}{
		{[]DeleteRequest{{Query: `0`, UserID: "test-user", StartTime: 0, EndTime: 100}}, true, "test-user"},
		{[]DeleteRequest{{Query: `1`, UserID: "test-user", StartTime: 200, EndTime: 400}}, true, "test-user"},
		{[]DeleteRequest{{Query: `2`, UserID: "test-user", StartTime: 400, EndTime: 500}}, true, "test-user"},
		{[]DeleteRequest{{Query: `3`, UserID: "test-user", StartTime: 500, EndTime: 700}}, true, "test-user"},
		{[]DeleteRequest{{Query: `3`, UserID: "other-user", StartTime: 500, EndTime: 700}}, false, "test-user"},
		{[]DeleteRequest{{Query: `4`, UserID: "test-user", StartTime: 700, EndTime: 900}}, true, "test-user"},
		{[]DeleteRequest{{Query: `4`, UserID: "", StartTime: 700, EndTime: 900}}, true, ""},
		{[]DeleteRequest{}, false, ""},
	}

	for _, tc := range tt {
		mgr := NewDeleteRequestsManager(&mockDeleteRequestsStore{deleteRequests: tc.deleteRequestsFromStore}, time.Hour, 70, &fakeLimits{mode: deletionmode.FilterAndDelete.String()}, nil)
		require.NoError(t, mgr.loadDeleteRequestsToProcess())

		interval := model.Interval{Start: 300, End: 600}
		require.Equal(t, tc.hasChunks, mgr.IntervalMayHaveExpiredChunks(interval, tc.user))
	}
}

type mockDeleteRequestsStore struct {
	DeleteRequestsStore
	deleteRequests           []DeleteRequest
	addReqs                  []DeleteRequest
	addErr                   error
	returnZeroDeleteRequests bool

	removeReqs []DeleteRequest
	removeErr  error

	getUser   string
	getID     string
	getResult []DeleteRequest
	getErr    error

	getAllUser   string
	getAllResult []DeleteRequest
	getAllErr    error

	genNumber string
}

func (m *mockDeleteRequestsStore) GetDeleteRequestsByStatus(_ context.Context, _ DeleteRequestStatus) ([]DeleteRequest, error) {
	return m.deleteRequests, nil
}

func (m *mockDeleteRequestsStore) AddDeleteRequestGroup(_ context.Context, reqs []DeleteRequest) ([]DeleteRequest, error) {
	m.addReqs = reqs
	if m.returnZeroDeleteRequests {
		return []DeleteRequest{}, m.addErr
	}
	return m.addReqs, m.addErr
}

func (m *mockDeleteRequestsStore) RemoveDeleteRequests(_ context.Context, reqs []DeleteRequest) error {
	m.removeReqs = reqs
	return m.removeErr
}

func (m *mockDeleteRequestsStore) GetDeleteRequestGroup(_ context.Context, userID, requestID string) ([]DeleteRequest, error) {
	m.getUser = userID
	m.getID = requestID
	return m.getResult, m.getErr
}

func (m *mockDeleteRequestsStore) GetAllDeleteRequestsForUser(_ context.Context, userID string) ([]DeleteRequest, error) {
	m.getAllUser = userID
	return m.getAllResult, m.getAllErr
}

func (m *mockDeleteRequestsStore) GetCacheGenerationNumber(_ context.Context, _ string) (string, error) {
	return m.genNumber, m.getErr
}
