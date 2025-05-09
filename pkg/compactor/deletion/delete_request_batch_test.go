package deletion

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/filter"
)

func TestDeleteRequestBatch_Expired(t *testing.T) {
	type resp struct {
		isExpired      bool
		expectedFilter filter.Func
	}

	now := model.Now()
	lblFoo, err := syntax.ParseLabels(`{foo="bar"}`)
	require.NoError(t, err)
	streamSelectorWithLineFilters := lblFoo.String() + `|="fizz"`
	streamSelectorWithStructuredMetadataFilters := lblFoo.String() + `| ping="pong"`
	streamSelectorWithLineAndStructuredMetadataFilters := lblFoo.String() + `| ping="pong" |= "fizz"`

	chunkEntry := retention.Chunk{
		From:    now.Add(-12 * time.Hour),
		Through: now.Add(-time.Hour),
	}

	for _, tc := range []struct {
		name                        string
		deleteRequests              []DeleteRequest
		expectedResp                resp
		expectedDeletionRangeByUser map[string]model.Interval
	}{
		{
			name: "no delete requests",
			expectedResp: resp{
				isExpired: false,
			},
		},
		{
			name: "no relevant delete requests",
			deleteRequests: []DeleteRequest{
				{
					UserID:    "different-user",
					Query:     lblFoo.String(),
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
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
			name: "no relevant delete requests",
			deleteRequests: []DeleteRequest{
				{
					UserID:    "different-user",
					Query:     lblFoo.String(),
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
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
			name: "delete request not matching labels",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     `{fizz="buzz"}`,
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
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
			name: "whole chunk deleted by single request",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
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
			name: "whole chunk deleted by single request with line filters",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(_ time.Time, s string, _ labels.Labels) bool {
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
			name: "whole chunk deleted by single request with structured metadata filters",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithStructuredMetadataFilters,
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(_ time.Time, _ string, structuredMetadata labels.Labels) bool {
					return structuredMetadata.Get(lblPing) == lblPong
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
			name: "whole chunk deleted by single request with line and structured metadata filters",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineAndStructuredMetadataFilters,
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(_ time.Time, s string, structuredMetadata labels.Labels) bool {
					return structuredMetadata.Get(lblPing) == lblPong && strings.Contains(s, "fizz")
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
			name: "deleted interval out of range",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
					Status:    StatusReceived,
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
			name: "deleted interval out of range(with multiple user requests)",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    "different-user",
					Query:     lblFoo.String(),
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
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
			name: "multiple delete requests with one deleting the whole chunk",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
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
			name: "multiple delete requests with line filters and one deleting the whole chunk",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(_ time.Time, s string, _ labels.Labels) bool {
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
			name: "multiple delete requests with structured metadata filters and one deleting the whole chunk",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithStructuredMetadataFilters,
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithStructuredMetadataFilters,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(_ time.Time, _ string, structuredMetadata labels.Labels) bool {
					return structuredMetadata.Get(lblPing) == lblPong
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
			name: "multiple delete requests causing multiple holes",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-10 * time.Hour),
					EndTime:   now.Add(-8 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-5 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-2 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, _ string, _ labels.Labels) bool {
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
			name: "multiple overlapping requests deleting the whole chunk",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-6 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-8 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(_ time.Time, _ string, _ labels.Labels) bool {
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
			name: "multiple overlapping requests with line filters deleting the whole chunk",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-6 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-8 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(_ time.Time, s string, _ labels.Labels) bool {
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
			name: "multiple overlapping requests with structured metadata filters deleting the whole chunk",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithStructuredMetadataFilters,
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-6 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithStructuredMetadataFilters,
					StartTime: now.Add(-8 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(_ time.Time, _ string, structuredMetadata labels.Labels) bool {
					return structuredMetadata.Get(lblPing) == lblPong
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
			name: "multiple non-overlapping requests deleting the whole chunk",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-6*time.Hour) - 1,
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-4*time.Hour) - 1,
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-4 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(_ time.Time, _ string, _ labels.Labels) bool {
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
			name: "multiple non-overlapping requests with line filter deleting the whole chunk",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-6*time.Hour) - 1,
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-4*time.Hour) - 1,
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-4 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(_ time.Time, s string, _ labels.Labels) bool {
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
			name: "multiple non-overlapping requests with structured metadata filter deleting the whole chunk",
			deleteRequests: []DeleteRequest{
				{
					UserID:    testUserID,
					Query:     streamSelectorWithStructuredMetadataFilters,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-6*time.Hour) - 1,
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithStructuredMetadataFilters,
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-4*time.Hour) - 1,
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     streamSelectorWithStructuredMetadataFilters,
					StartTime: now.Add(-4 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(_ time.Time, _ string, structuredMetadata labels.Labels) bool {
					return structuredMetadata.Get(lblPing) == lblPong
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-12 * time.Hour),
					End:   now,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batch := newDeleteRequestBatch(newDeleteRequestsManagerMetrics(nil))
			for _, req := range tc.deleteRequests {
				batch.addDeleteRequest(&req)
			}

			for _, deleteRequests := range batch.deleteRequestsToProcess {
				for _, dr := range deleteRequests.requests {
					require.EqualValues(t, 0, dr.DeletedLines)
				}
			}

			isExpired, filterFunc := batch.expired([]byte(testUserID), chunkEntry, lblFoo, func(_ *DeleteRequest) bool {
				return false
			})
			require.Equal(t, tc.expectedResp.isExpired, isExpired)
			if tc.expectedResp.expectedFilter == nil {
				require.Nil(t, filterFunc)
			} else {
				require.NotNil(t, filterFunc)

				for start := chunkEntry.From; start <= chunkEntry.Through; start = start.Add(time.Minute) {
					line := "foo bar"
					if start.Time().Minute()%2 == 1 {
						line = "fizz buzz"
					}
					// mix of empty, ding=dong and ping=pong as structured metadata
					var structuredMetadata labels.Labels
					if start.Time().Minute()%3 == 0 {
						structuredMetadata = labels.FromStrings(lblPing, lblPong)
					} else if start.Time().Minute()%2 == 0 {
						structuredMetadata = labels.FromStrings("ting", "tong")
					}
					require.Equal(t, tc.expectedResp.expectedFilter(start.Time(), line, structuredMetadata), filterFunc(start.Time(), line, structuredMetadata), "line", line, "time", start.Time(), "now", now.Time())
				}

				require.Equal(t, len(tc.expectedDeletionRangeByUser), len(batch.deleteRequestsToProcess))
				for userID, dr := range tc.expectedDeletionRangeByUser {
					require.Equal(t, dr, batch.deleteRequestsToProcess[userID].requestsInterval)
				}
			}
		})
	}
}

func TestDeleteRequestBatch_IntervalMayHaveExpiredChunks(t *testing.T) {
	tests := []struct {
		name           string
		deleteRequests map[string]*userDeleteRequests
		userID         string
		expected       bool
	}{
		{
			name:           "no delete requests",
			deleteRequests: map[string]*userDeleteRequests{},
			userID:         "test-user",
			expected:       false,
		},
		{
			name: "has delete requests for user",
			deleteRequests: map[string]*userDeleteRequests{
				"test-user": {
					requests: []*DeleteRequest{
						{
							UserID: "test-user",
						},
					},
				},
			},
			userID:   "test-user",
			expected: true,
		},
		{
			name: "has delete requests but not for user",
			deleteRequests: map[string]*userDeleteRequests{
				"other-user": {
					requests: []*DeleteRequest{
						{
							UserID: "other-user",
						},
					},
				},
			},
			userID:   "test-user",
			expected: false,
		},
		{
			name: "check for all users",
			deleteRequests: map[string]*userDeleteRequests{
				"test-user": {
					requests: []*DeleteRequest{
						{
							UserID: "test-user",
						},
					},
				},
			},
			userID:   "",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			batch := &deleteRequestBatch{
				deleteRequestsToProcess: tc.deleteRequests,
				metrics:                 &deleteRequestsManagerMetrics{},
			}

			result := batch.intervalMayHaveExpiredChunks(tc.userID)
			require.Equal(t, tc.expected, result)
		})
	}
}
