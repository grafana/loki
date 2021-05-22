package deletion

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
)

const testUserID = "test-user"

type mockDeleteRequestsStore struct {
	deleteRequests []DeleteRequest
}

func (m mockDeleteRequestsStore) GetDeleteRequestsByStatus(ctx context.Context, status DeleteRequestStatus) ([]DeleteRequest, error) {
	return m.deleteRequests, nil
}

func (m mockDeleteRequestsStore) UpdateStatus(ctx context.Context, userID, requestID string, newStatus DeleteRequestStatus) error {
	return nil
}

func (m mockDeleteRequestsStore) AddDeleteRequest(ctx context.Context, userID string, startTime, endTime model.Time, selectors []string) error {
	panic("implement me")
}

func (m mockDeleteRequestsStore) GetDeleteRequestsForUserByStatus(ctx context.Context, userID string, status DeleteRequestStatus) ([]DeleteRequest, error) {
	panic("implement me")
}

func (m mockDeleteRequestsStore) GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error) {
	panic("implement me")
}

func (m mockDeleteRequestsStore) GetDeleteRequest(ctx context.Context, userID, requestID string) (*DeleteRequest, error) {
	panic("implement me")
}

func (m mockDeleteRequestsStore) GetPendingDeleteRequestsForUser(ctx context.Context, userID string) ([]DeleteRequest, error) {
	panic("implement me")
}

func (m mockDeleteRequestsStore) RemoveDeleteRequest(ctx context.Context, userID, requestID string, createdAt, startTime, endTime model.Time) error {
	panic("implement me")
}

func (m mockDeleteRequestsStore) Stop() {
	panic("implement me")
}

func TestDeleteRequestsManager_Expired(t *testing.T) {
	type resp struct {
		isExpired           bool
		nonDeletedIntervals []model.Interval
	}

	now := model.Now()
	lblFoo, err := logql.ParseLabels(`{foo="bar"}`)
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
		deleteRequestsFromStore []DeleteRequest
		expectedResp            resp
	}{
		{
			name: "no delete requests",
			expectedResp: resp{
				isExpired:           false,
				nonDeletedIntervals: nil,
			},
		},
		{
			name: "no relevant delete requests",
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    "different-user",
					Selectors: []string{lblFoo.String()},
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
			name: "whole chunk deleted by single request",
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Selectors: []string{lblFoo.String()},
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
			name: "deleted interval out of range",
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Selectors: []string{lblFoo.String()},
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
			name: "multiple delete requests with one deleting the whole chunk",
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Selectors: []string{lblFoo.String()},
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
				},
				{
					UserID:    testUserID,
					Selectors: []string{lblFoo.String()},
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
			name: "multiple delete requests causing multiple holes",
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Selectors: []string{lblFoo.String()},
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
				},
				{
					UserID:    testUserID,
					Selectors: []string{lblFoo.String()},
					StartTime: now.Add(-10 * time.Hour),
					EndTime:   now.Add(-8 * time.Hour),
				},
				{
					UserID:    testUserID,
					Selectors: []string{lblFoo.String()},
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-5 * time.Hour),
				},
				{
					UserID:    testUserID,
					Selectors: []string{lblFoo.String()},
					StartTime: now.Add(-2 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired: true,
				nonDeletedIntervals: []model.Interval{
					{
						Start: now.Add(-11*time.Hour) + 1,
						End:   now.Add(-10*time.Hour) - 1,
					},
					{
						Start: now.Add(-8*time.Hour) + 1,
						End:   now.Add(-6*time.Hour) - 1,
					},
					{
						Start: now.Add(-5*time.Hour) + 1,
						End:   now.Add(-2*time.Hour) - 1,
					},
				},
			},
		},
		{
			name: "multiple overlapping requests deleting the whole chunk",
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Selectors: []string{lblFoo.String()},
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-6 * time.Hour),
				},
				{
					UserID:    testUserID,
					Selectors: []string{lblFoo.String()},
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
			name: "multiple non-overlapping requests deleting the whole chunk",
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    testUserID,
					Selectors: []string{lblFoo.String()},
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-6*time.Hour) - 1,
				},
				{
					UserID:    testUserID,
					Selectors: []string{lblFoo.String()},
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-4*time.Hour) - 1,
				},
				{
					UserID:    testUserID,
					Selectors: []string{lblFoo.String()},
					StartTime: now.Add(-4 * time.Hour),
					EndTime:   now,
				},
			},
			expectedResp: resp{
				isExpired:           true,
				nonDeletedIntervals: nil,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := NewDeleteRequestsManager(mockDeleteRequestsStore{deleteRequests: tc.deleteRequestsFromStore}, time.Hour, nil)
			require.NoError(t, mgr.loadDeleteRequestsToProcess())

			isExpired, nonDeletedIntervals := mgr.Expired(chunkEntry, model.Now())
			require.Equal(t, tc.expectedResp.isExpired, isExpired)
			require.Equal(t, tc.expectedResp.nonDeletedIntervals, nonDeletedIntervals)
		})
	}
}
