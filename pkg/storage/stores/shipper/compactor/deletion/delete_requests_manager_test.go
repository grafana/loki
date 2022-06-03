package deletion

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
)

const testUserID = "test-user"

func TestDeleteRequestsManager_Expired(t *testing.T) {
	type resp struct {
		isExpired           bool
		nonDeletedIntervals []retention.IntervalFilter
	}

	now := model.Now()
	lblFoo, err := syntax.ParseLabels(`{foo="bar"}`)
	require.NoError(t, err)

	chunkEntry := retention.ChunkEntry{
		ChunkRef: retention.ChunkRef{
			UserID:  []byte(testUserID),
			From:    now.Add(-12 * time.Hour),
			Through: now.Add(-time.Hour),
		},
		Labels: lblFoo,
	}

	for _, tc := range []struct {
		name                    string
		deletionMode            Mode
		deleteRequestsFromStore []DeleteRequest
		expectedResp            resp
	}{
		{
			name:         "no delete requests",
			deletionMode: WholeStreamDeletion,
			expectedResp: resp{
				isExpired:           false,
				nonDeletedIntervals: nil,
			},
		},
		{
			name:         "no relevant delete requests",
			deletionMode: WholeStreamDeletion,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    "different-user",
					Query:     lblFoo.String(),
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired:           false,
				nonDeletedIntervals: nil,
			},
		},
		{
			name:         "whole chunk deleted by single request",
			deletionMode: WholeStreamDeletion,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired:           true,
				nonDeletedIntervals: nil,
			},
		},
		{
			name:         "deleted interval out of range",
			deletionMode: WholeStreamDeletion,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
				},
			},
			expectedResp: resp{
				isExpired:           false,
				nonDeletedIntervals: nil,
			},
		},
		{
			name:         "multiple delete requests with one deleting the whole chunk",
			deletionMode: WholeStreamDeletion,
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
				isExpired:           true,
				nonDeletedIntervals: nil,
			},
		},
		{
			name:         "multiple delete requests causing multiple holes",
			deletionMode: WholeStreamDeletion,
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
				nonDeletedIntervals: []retention.IntervalFilter{
					{
						Interval: model.Interval{
							Start: now.Add(-11*time.Hour) + 1,
							End:   now.Add(-10*time.Hour) - 1,
						},
					},
					{
						Interval: model.Interval{
							Start: now.Add(-8*time.Hour) + 1,
							End:   now.Add(-6*time.Hour) - 1,
						},
					},
					{
						Interval: model.Interval{
							Start: now.Add(-5*time.Hour) + 1,
							End:   now.Add(-2*time.Hour) - 1,
						},
					},
				},
			},
		},
		{
			name:         "multiple overlapping requests deleting the whole chunk",
			deletionMode: WholeStreamDeletion,
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
				isExpired:           true,
				nonDeletedIntervals: nil,
			},
		},
		{
			name:         "multiple non-overlapping requests deleting the whole chunk",
			deletionMode: WholeStreamDeletion,
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
				isExpired:           true,
				nonDeletedIntervals: nil,
			},
		},
		{
			name:         "deletes are disabled",
			deletionMode: Disabled,
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
				isExpired:           false,
				nonDeletedIntervals: nil,
			},
		},
		{
			name:         "deletes are `filter-only`",
			deletionMode: FilterOnly,
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
				isExpired:           false,
				nonDeletedIntervals: nil,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := NewDeleteRequestsManager(mockDeleteRequestsStore{deleteRequests: tc.deleteRequestsFromStore}, time.Hour, nil, tc.deletionMode)
			require.NoError(t, mgr.loadDeleteRequestsToProcess())

			isExpired, nonDeletedIntervals := mgr.Expired(chunkEntry, model.Now())
			require.Equal(t, tc.expectedResp.isExpired, isExpired)
			for idx, interval := range nonDeletedIntervals {
				require.Equal(t, tc.expectedResp.nonDeletedIntervals[idx].Interval.Start, interval.Interval.Start)
				require.Equal(t, tc.expectedResp.nonDeletedIntervals[idx].Interval.End, interval.Interval.End)
				require.NotNil(t, interval.Filter)
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
		{[]DeleteRequest{{Query: `0`, UserID: "test-user", StartTime: 0, EndTime: 100}}, false, "test-user"},
		{[]DeleteRequest{{Query: `1`, UserID: "test-user", StartTime: 200, EndTime: 400}}, true, "test-user"},
		{[]DeleteRequest{{Query: `2`, UserID: "test-user", StartTime: 400, EndTime: 500}}, true, "test-user"},
		{[]DeleteRequest{{Query: `3`, UserID: "test-user", StartTime: 500, EndTime: 700}}, true, "test-user"},
		{[]DeleteRequest{{Query: `3`, UserID: "other-user", StartTime: 500, EndTime: 700}}, false, "test-user"},
		{[]DeleteRequest{{Query: `4`, UserID: "test-user", StartTime: 700, EndTime: 900}}, false, "test-user"},
		{[]DeleteRequest{{Query: `4`, UserID: "", StartTime: 700, EndTime: 900}}, true, ""},
		{[]DeleteRequest{}, false, ""},
	}

	for _, tc := range tt {
		mgr := NewDeleteRequestsManager(mockDeleteRequestsStore{deleteRequests: tc.deleteRequestsFromStore}, time.Hour, nil, FilterAndDelete)
		require.NoError(t, mgr.loadDeleteRequestsToProcess())

		interval := model.Interval{Start: 300, End: 600}
		require.Equal(t, tc.hasChunks, mgr.IntervalMayHaveExpiredChunks(interval, tc.user))
	}
}

type mockDeleteRequestsStore struct {
	DeleteRequestsStore
	deleteRequests []DeleteRequest
}

func (m mockDeleteRequestsStore) GetDeleteRequestsByStatus(_ context.Context, _ DeleteRequestStatus) ([]DeleteRequest, error) {
	return m.deleteRequests, nil
}
