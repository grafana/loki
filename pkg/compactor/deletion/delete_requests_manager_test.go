package deletion

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/deletionmode"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/filter"
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
	streamSelectorWithStructuredMetadataFilters := lblFoo.String() + `| ping="pong"`
	streamSelectorWithLineAndStructuredMetadataFilters := lblFoo.String() + `| ping="pong" |= "fizz"`

	chunkEntry := retention.ChunkEntry{
		ChunkRef: retention.ChunkRef{
			UserID:  []byte(testUserID),
			From:    now.Add(-12 * time.Hour),
			Through: now.Add(-time.Hour),
		},
		Labels: lblFoo,
	}

	for _, tc := range []struct {
		name                              string
		deletionMode                      deletionmode.Mode
		deleteRequestsFromStore           []DeleteRequest
		batchSize                         int
		expectedResp                      resp
		expectedDeletionRangeByUser       map[string]model.Interval
		expectedRequestsMarkedAsProcessed []int
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
			expectedRequestsMarkedAsProcessed: []int{0},
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
			expectedRequestsMarkedAsProcessed: []int{0},
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
			expectedRequestsMarkedAsProcessed: []int{0},
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
			expectedRequestsMarkedAsProcessed: []int{0},
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
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(_ time.Time, s string, _ ...labels.Label) bool {
					return strings.Contains(s, "fizz")
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-24 * time.Hour),
					End:   now,
				},
			},
			expectedRequestsMarkedAsProcessed: []int{0},
		},
		{
			name:         "whole chunk deleted by single request with structured metadata filters",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
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
				expectedFilter: func(_ time.Time, _ string, structuredMetadata ...labels.Label) bool {
					return labels.Labels(structuredMetadata).Get(lblPing) == lblPong
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-24 * time.Hour),
					End:   now,
				},
			},
			expectedRequestsMarkedAsProcessed: []int{0},
		},
		{
			name:         "whole chunk deleted by single request with line and structured metadata filters",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
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
				expectedFilter: func(_ time.Time, s string, structuredMetadata ...labels.Label) bool {
					return labels.Labels(structuredMetadata).Get(lblPing) == lblPong && strings.Contains(s, "fizz")
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-24 * time.Hour),
					End:   now,
				},
			},
			expectedRequestsMarkedAsProcessed: []int{0},
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
			expectedRequestsMarkedAsProcessed: []int{0},
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
			expectedRequestsMarkedAsProcessed: []int{0, 1},
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
			expectedRequestsMarkedAsProcessed: []int{0, 1},
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
				expectedFilter: func(_ time.Time, s string, _ ...labels.Label) bool {
					return strings.Contains(s, "fizz")
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-48 * time.Hour),
					End:   now,
				},
			},
			expectedRequestsMarkedAsProcessed: []int{0, 1},
		},
		{
			name:         "multiple delete requests with structured metadata filters and one deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
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
				expectedFilter: func(_ time.Time, _ string, structuredMetadata ...labels.Label) bool {
					return labels.Labels(structuredMetadata).Get(lblPing) == lblPong
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-48 * time.Hour),
					End:   now,
				},
			},
			expectedRequestsMarkedAsProcessed: []int{0, 1},
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
				expectedFilter: func(ts time.Time, _ string, _ ...labels.Label) bool {
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
			expectedRequestsMarkedAsProcessed: []int{0, 1, 2, 3},
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
				expectedFilter: func(_ time.Time, _ string, _ ...labels.Label) bool {
					return true
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-13 * time.Hour),
					End:   now,
				},
			},
			expectedRequestsMarkedAsProcessed: []int{0, 1},
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
				expectedFilter: func(_ time.Time, s string, _ ...labels.Label) bool {
					return strings.Contains(s, "fizz")
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-13 * time.Hour),
					End:   now,
				},
			},
			expectedRequestsMarkedAsProcessed: []int{0, 1},
		},
		{
			name:         "multiple overlapping requests with structured metadata filters deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
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
				expectedFilter: func(_ time.Time, _ string, structuredMetadata ...labels.Label) bool {
					return labels.Labels(structuredMetadata).Get(lblPing) == lblPong
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-13 * time.Hour),
					End:   now,
				},
			},
			expectedRequestsMarkedAsProcessed: []int{0, 1},
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
				expectedFilter: func(_ time.Time, _ string, _ ...labels.Label) bool {
					return true
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-12 * time.Hour),
					End:   now,
				},
			},
			expectedRequestsMarkedAsProcessed: []int{0, 1, 2},
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
				expectedFilter: func(_ time.Time, s string, _ ...labels.Label) bool {
					return strings.Contains(s, "fizz")
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-12 * time.Hour),
					End:   now,
				},
			},
			expectedRequestsMarkedAsProcessed: []int{0, 1, 2},
		},
		{
			name:         "multiple non-overlapping requests with structured metadata filter deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
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
				expectedFilter: func(_ time.Time, _ string, structuredMetadata ...labels.Label) bool {
					return labels.Labels(structuredMetadata).Get(lblPing) == lblPong
				},
			},
			expectedDeletionRangeByUser: map[string]model.Interval{
				testUserID: {
					Start: now.Add(-12 * time.Hour),
					End:   now,
				},
			},
			expectedRequestsMarkedAsProcessed: []int{0, 1, 2},
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
					StartTime: now.Add(-10 * time.Hour),
					EndTime:   now.Add(-8 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, _ string, _ ...labels.Label) bool {
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
			expectedRequestsMarkedAsProcessed: []int{2, 3},
		},
		{
			name:         "Deletes beyond retention are marked as processed straight away without being batched for processing",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    2,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    "different-user",
					Query:     lblFoo.String(),
					StartTime: now.Add(-14 * 24 * time.Hour),
					EndTime:   now.Add(-10 * 24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-14 * 24 * time.Hour),
					EndTime:   now.Add(-10 * 24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-2 * time.Hour),
					EndTime:   now,
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
					StartTime: now.Add(-10 * time.Hour),
					EndTime:   now.Add(-8 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, _ string, _ ...labels.Label) bool {
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
			expectedRequestsMarkedAsProcessed: []int{0, 1, 4, 5},
		},
		{
			name:         "All deletes beyond retention",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    2,
			deleteRequestsFromStore: []DeleteRequest{
				{
					UserID:    "different-user",
					Query:     lblFoo.String(),
					StartTime: now.Add(-14 * 24 * time.Hour),
					EndTime:   now.Add(-10 * 24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-14 * 24 * time.Hour),
					EndTime:   now.Add(-10 * 24 * time.Hour),
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: false,
			},
			expectedRequestsMarkedAsProcessed: []int{0, 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockDeleteRequestsStore := &mockDeleteRequestsStore{deleteRequests: tc.deleteRequestsFromStore}
			mgr := NewDeleteRequestsManager(mockDeleteRequestsStore, time.Hour, tc.batchSize, &fakeLimits{defaultLimit: limit{
				retentionPeriod: 7 * 24 * time.Hour,
				deletionMode:    tc.deletionMode.String(),
			}}, nil)
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
			} else {
				require.NotNil(t, filterFunc)

				for start := chunkEntry.From; start <= chunkEntry.Through; start = start.Add(time.Minute) {
					line := "foo bar"
					if start.Time().Minute()%2 == 1 {
						line = "fizz buzz"
					}
					// mix of empty, ding=dong and ping=pong as structured metadata
					var structuredMetadata []labels.Label
					if start.Time().Minute()%3 == 0 {
						structuredMetadata = []labels.Label{{Name: lblPing, Value: lblPong}}
					} else if start.Time().Minute()%2 == 0 {
						structuredMetadata = []labels.Label{{Name: "ting", Value: "tong"}}
					}
					require.Equal(t, tc.expectedResp.expectedFilter(start.Time(), line, structuredMetadata...), filterFunc(start.Time(), line, structuredMetadata...), "line", line, "time", start.Time(), "now", now.Time())
				}

				require.Equal(t, len(tc.expectedDeletionRangeByUser), len(mgr.deleteRequestsToProcess))
				for userID, dr := range tc.expectedDeletionRangeByUser {
					require.Equal(t, dr, mgr.deleteRequestsToProcess[userID].requestsInterval)
				}
			}

			mgr.MarkPhaseFinished()

			processedRequests, err := mockDeleteRequestsStore.GetDeleteRequestsByStatus(context.Background(), StatusProcessed)
			require.NoError(t, err)
			require.Len(t, processedRequests, len(tc.expectedRequestsMarkedAsProcessed))

			for i, reqIdx := range tc.expectedRequestsMarkedAsProcessed {
				require.True(t, requestsAreEqual(tc.deleteRequestsFromStore[reqIdx], processedRequests[i]))
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
		{[]DeleteRequest{{Query: `0`, UserID: "test-user", StartTime: 0, EndTime: 100, Status: StatusReceived}}, true, "test-user"},
		{[]DeleteRequest{{Query: `1`, UserID: "test-user", StartTime: 200, EndTime: 400, Status: StatusReceived}}, true, "test-user"},
		{[]DeleteRequest{{Query: `2`, UserID: "test-user", StartTime: 400, EndTime: 500, Status: StatusReceived}}, true, "test-user"},
		{[]DeleteRequest{{Query: `3`, UserID: "test-user", StartTime: 500, EndTime: 700, Status: StatusReceived}}, true, "test-user"},
		{[]DeleteRequest{{Query: `3`, UserID: "other-user", StartTime: 500, EndTime: 700, Status: StatusReceived}}, false, "test-user"},
		{[]DeleteRequest{{Query: `4`, UserID: "test-user", StartTime: 700, EndTime: 900, Status: StatusReceived}}, true, "test-user"},
		{[]DeleteRequest{{Query: `4`, UserID: "", StartTime: 700, EndTime: 900, Status: StatusReceived}}, true, ""},
		{[]DeleteRequest{}, false, ""},
	}

	for _, tc := range tt {
		mgr := NewDeleteRequestsManager(&mockDeleteRequestsStore{deleteRequests: tc.deleteRequestsFromStore}, time.Hour, 70, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}}, nil)
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

func (m *mockDeleteRequestsStore) GetDeleteRequestsByStatus(_ context.Context, status DeleteRequestStatus) ([]DeleteRequest, error) {
	reqs := make([]DeleteRequest, 0, len(m.deleteRequests))
	for i := range m.deleteRequests {
		if m.deleteRequests[i].Status == status {
			reqs = append(reqs, m.deleteRequests[i])
		}
	}
	return reqs, nil
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

func (m *mockDeleteRequestsStore) UpdateStatus(_ context.Context, req DeleteRequest, newStatus DeleteRequestStatus) error {
	for i := range m.deleteRequests {
		if requestsAreEqual(m.deleteRequests[i], req) {
			m.deleteRequests[i].Status = newStatus
		}
	}

	return nil
}

func requestsAreEqual(req1, req2 DeleteRequest) bool {
	if req1.UserID == req2.UserID &&
		req1.Query == req2.Query &&
		req1.StartTime == req2.StartTime &&
		req1.EndTime == req2.EndTime {
		return true
	}

	return false
}
