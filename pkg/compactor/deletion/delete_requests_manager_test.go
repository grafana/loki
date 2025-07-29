package deletion

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

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

	chunkEntry := retention.Chunk{
		From:    now.Add(-12 * time.Hour),
		Through: now.Add(-time.Hour),
	}

	for _, tc := range []struct {
		name                              string
		deletionMode                      deletionmode.Mode
		deleteRequestsFromStore           []DeleteRequest
		batchSize                         int
		expectedResp                      resp
		expectedDeletionRangeByUser       map[string]model.Interval
		expectedRequestsMarkedAsProcessed []int
		expectedRequestsToProcess         []int
		expectedDuplicateRequests         []int
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
					RequestID: "1",
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
			expectedRequestsToProcess:         []int{0},
		},
		{
			name:         "no relevant delete requests",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
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
			expectedRequestsToProcess:         []int{0},
		},
		{
			name:         "delete request not matching labels",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
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
			expectedRequestsToProcess:         []int{0},
		},
		{
			name:         "whole chunk deleted by single request",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
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
			expectedRequestsToProcess:         []int{0},
		},
		{
			name:         "whole chunk deleted by single request with line filters",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
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
			expectedRequestsMarkedAsProcessed: []int{0},
			expectedRequestsToProcess:         []int{0},
		},
		{
			name:         "whole chunk deleted by single request with structured metadata filters",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
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
			expectedRequestsMarkedAsProcessed: []int{0},
			expectedRequestsToProcess:         []int{0},
		},
		{
			name:         "whole chunk deleted by single request with line and structured metadata filters",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
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
			expectedRequestsMarkedAsProcessed: []int{0},
			expectedRequestsToProcess:         []int{0},
		},
		{
			name:         "deleted interval out of range",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
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
			expectedRequestsToProcess:         []int{0},
		},
		{
			name:         "deleted interval out of range(with multiple user requests)",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
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
			expectedRequestsToProcess:         []int{0, 1},
		},
		{
			name:         "multiple delete requests with one deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
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
			expectedRequestsToProcess:         []int{0, 1},
		},
		{
			name:         "multiple delete requests with line filters and one deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
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
			expectedRequestsMarkedAsProcessed: []int{0, 1},
			expectedRequestsToProcess:         []int{0, 1},
		},
		{
			name:         "multiple delete requests with structured metadata filters and one deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    testUserID,
					Query:     streamSelectorWithStructuredMetadataFilters,
					StartTime: now.Add(-48 * time.Hour),
					EndTime:   now.Add(-24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
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
			expectedRequestsMarkedAsProcessed: []int{0, 1},
			expectedRequestsToProcess:         []int{0, 1},
		},
		{
			name:         "multiple delete requests causing multiple holes",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-10 * time.Hour),
					EndTime:   now.Add(-8 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "3",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-5 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "4",
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
			expectedRequestsMarkedAsProcessed: []int{0, 1, 2, 3},
			expectedRequestsToProcess:         []int{0, 1, 2, 3},
		},
		{
			name:         "multiple overlapping requests deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-6 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
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
			expectedRequestsMarkedAsProcessed: []int{0, 1},
			expectedRequestsToProcess:         []int{0, 1},
		},
		{
			name:         "multiple overlapping requests with line filters deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-6 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
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
			expectedRequestsMarkedAsProcessed: []int{0, 1},
			expectedRequestsToProcess:         []int{0, 1},
		},
		{
			name:         "multiple overlapping requests with structured metadata filters deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    testUserID,
					Query:     streamSelectorWithStructuredMetadataFilters,
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-6 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
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
			expectedRequestsMarkedAsProcessed: []int{0, 1},
			expectedRequestsToProcess:         []int{0, 1},
		},
		{
			name:         "multiple non-overlapping requests deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-6*time.Hour) - 1,
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-4*time.Hour) - 1,
					Status:    StatusReceived,
				},
				{
					RequestID: "3",
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
			expectedRequestsMarkedAsProcessed: []int{0, 1, 2},
			expectedRequestsToProcess:         []int{0, 1, 2},
		},
		{
			name:         "multiple non-overlapping requests with line filter deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-6*time.Hour) - 1,
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-4*time.Hour) - 1,
					Status:    StatusReceived,
				},
				{
					RequestID: "3",
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
			expectedRequestsMarkedAsProcessed: []int{0, 1, 2},
			expectedRequestsToProcess:         []int{0, 1, 2},
		},
		{
			name:         "multiple non-overlapping requests with structured metadata filter deleting the whole chunk",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    testUserID,
					Query:     streamSelectorWithStructuredMetadataFilters,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-6*time.Hour) - 1,
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
					UserID:    testUserID,
					Query:     streamSelectorWithStructuredMetadataFilters,
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-4*time.Hour) - 1,
					Status:    StatusReceived,
				},
				{
					RequestID: "3",
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
			expectedRequestsMarkedAsProcessed: []int{0, 1, 2},
			expectedRequestsToProcess:         []int{0, 1, 2},
		},
		{
			name:         "deletes are disabled",
			deletionMode: deletionmode.Disabled,
			batchSize:    70,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-10 * time.Hour),
					EndTime:   now.Add(-8 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "3",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-5 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "4",
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
					RequestID: "1",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-10 * time.Hour),
					EndTime:   now.Add(-8 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "3",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-5 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "4",
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
					RequestID: "1",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-2 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-5 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "3",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-10 * time.Hour),
					EndTime:   now.Add(-8 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "4",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, _ string, _ labels.Labels) bool {
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
			expectedRequestsToProcess:         []int{2, 3},
		},
		{
			name:         "Deletes beyond retention are marked as processed straight away without being batched for processing",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    2,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    "different-user",
					Query:     lblFoo.String(),
					StartTime: now.Add(-14 * 24 * time.Hour),
					EndTime:   now.Add(-10 * 24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-14 * 24 * time.Hour),
					EndTime:   now.Add(-10 * 24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "3",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-2 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
				{
					RequestID: "4",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-6 * time.Hour),
					EndTime:   now.Add(-5 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "5",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-10 * time.Hour),
					EndTime:   now.Add(-8 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "6",
					UserID:    testUserID,
					Query:     lblFoo.String(),
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
					Status:    StatusReceived,
				},
			},
			expectedResp: resp{
				isExpired: true,
				expectedFilter: func(ts time.Time, _ string, _ labels.Labels) bool {
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
			expectedRequestsToProcess:         []int{4, 5},
		},
		{
			name:         "All deletes beyond retention",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    2,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    "different-user",
					Query:     lblFoo.String(),
					StartTime: now.Add(-14 * 24 * time.Hour),
					EndTime:   now.Add(-10 * 24 * time.Hour),
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
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
		{
			name:         "duplicate delete request marked as processed with loaded request",
			deletionMode: deletionmode.FilterAndDelete,
			batchSize:    2,
			deleteRequestsFromStore: []DeleteRequest{
				{
					RequestID: "1",
					UserID:    testUserID,
					Query:     streamSelectorWithLineFilters,
					StartTime: now.Add(-24 * time.Hour),
					EndTime:   now,
					Status:    StatusReceived,
				},
				{
					RequestID: "2",
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
			expectedRequestsMarkedAsProcessed: []int{0, 1},
			expectedRequestsToProcess:         []int{0},
			expectedDuplicateRequests:         []int{1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mockDeleteRequestsStore := &mockDeleteRequestsStore{deleteRequests: tc.deleteRequestsFromStore}
			mgr, err := NewDeleteRequestsManager(t.TempDir(), mockDeleteRequestsStore, time.Hour, tc.batchSize, &fakeLimits{defaultLimit: limit{
				retentionPeriod: 7 * 24 * time.Hour,
				deletionMode:    tc.deletionMode.String(),
			}}, false, nil, nil)
			require.NoError(t, err)
			require.NoError(t, mgr.Init(nil, nil))
			mgr.MarkPhaseStarted()
			require.NotNil(t, mgr.currentBatch)

			// verify we have picked up expected requests for processing
			requests := mgr.currentBatch.getAllRequests()
			require.Len(t, requests, len(tc.expectedRequestsToProcess))
			slices.SortFunc(requests, func(a, b *DeleteRequest) int {
				return strings.Compare(a.RequestID, b.RequestID)
			})
			for i, reqIdx := range tc.expectedRequestsToProcess {
				require.True(t, requestsAreEqual(tc.deleteRequestsFromStore[reqIdx], *requests[i]))
			}

			// verify we have considered appropriate requests as duplicate of what we are currently processing
			duplicateRequests := mgr.currentBatch.duplicateRequests
			require.Len(t, duplicateRequests, len(tc.expectedDuplicateRequests))
			for i, reqIdx := range tc.expectedDuplicateRequests {
				require.True(t, requestsAreEqual(tc.deleteRequestsFromStore[reqIdx], duplicateRequests[i]))
			}

			for _, deleteRequests := range mgr.currentBatch.deleteRequestsToProcess {
				for _, dr := range deleteRequests.requests {
					require.EqualValues(t, 0, dr.DeletedLines)
				}
			}

			isExpired, filterFunc := mgr.Expired([]byte(testUserID), chunkEntry, lblFoo, nil, "", model.Now())
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

				require.Equal(t, len(tc.expectedDeletionRangeByUser), len(mgr.currentBatch.deleteRequestsToProcess))
				for userID, dr := range tc.expectedDeletionRangeByUser {
					require.Equal(t, dr, mgr.currentBatch.deleteRequestsToProcess[userID].requestsInterval)
				}
			}

			mgr.MarkPhaseFinished()

			processedRequests, err := mockDeleteRequestsStore.getDeleteRequestsByStatus(StatusProcessed)
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
		mgr, err := NewDeleteRequestsManager(t.TempDir(), &mockDeleteRequestsStore{deleteRequests: tc.deleteRequestsFromStore}, time.Hour, 70, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}}, false, nil, nil)
		require.NoError(t, err)
		require.NoError(t, mgr.Init(nil, nil))
		mgr.MarkPhaseStarted()
		require.NotNil(t, mgr.currentBatch)

		interval := model.Interval{Start: 300, End: 600}
		require.Equal(t, tc.hasChunks, mgr.IntervalMayHaveExpiredChunks(interval, tc.user))
	}
}

func TestDeleteRequestsManager_SeriesProgress(t *testing.T) {
	user1 := []byte("user1")
	user2 := []byte("user2")
	lblFooBar := mustParseLabel(`{foo="bar"}`)
	lblFizzBuzz := mustParseLabel(`{fizz="buzz"}`)
	type markSeriesProcessed struct {
		userID, seriesID []byte
		lbls             labels.Labels
		tableName        string
	}

	type chunkEntry struct {
		userID    []byte
		chk       retention.Chunk
		lbls      labels.Labels
		seriesID  []byte
		tableName string
	}

	for _, tc := range []struct {
		name                  string
		seriesToMarkProcessed []markSeriesProcessed
		chunkEntry            chunkEntry
		expSkipSeries         bool
		expExpired            bool
	}{
		{
			name: "no series marked as processed",
			chunkEntry: chunkEntry{
				userID: user1,
				chk: retention.Chunk{
					From:    10,
					Through: 20,
				},
				lbls:      lblFooBar,
				seriesID:  []byte(lblFooBar.String()),
				tableName: "t1",
			},
			expSkipSeries: false,
			expExpired:    true,
		},
		{
			name: "chunk's series marked as processed",
			seriesToMarkProcessed: []markSeriesProcessed{
				{
					userID:    user1,
					seriesID:  []byte(lblFooBar.String()),
					lbls:      lblFooBar,
					tableName: "t1",
				},
			},
			chunkEntry: chunkEntry{
				userID: user1,
				chk: retention.Chunk{
					From:    10,
					Through: 20,
				},
				lbls:      lblFooBar,
				seriesID:  []byte(lblFooBar.String()),
				tableName: "t1",
			},
			expSkipSeries: true,
			expExpired:    false,
		},
		{
			name: "a different series marked as processed",
			seriesToMarkProcessed: []markSeriesProcessed{
				{
					userID:    user1,
					seriesID:  []byte(lblFizzBuzz.String()),
					lbls:      lblFizzBuzz,
					tableName: "t1",
				},
			},
			chunkEntry: chunkEntry{
				userID: user1,
				chk: retention.Chunk{
					From:    10,
					Through: 20,
				},
				lbls:      lblFooBar,
				seriesID:  []byte(lblFooBar.String()),
				tableName: "t1",
			},
			expSkipSeries: false,
			expExpired:    true,
		},
		{
			name: "a different users series marked as processed",
			seriesToMarkProcessed: []markSeriesProcessed{
				{
					userID:    user2,
					seriesID:  []byte(lblFooBar.String()),
					lbls:      lblFooBar,
					tableName: "t1",
				},
			},
			chunkEntry: chunkEntry{
				userID: user1,
				chk: retention.Chunk{
					From:    10,
					Through: 20,
				},
				lbls:      lblFooBar,
				seriesID:  []byte(lblFooBar.String()),
				tableName: "t1",
			},
			expSkipSeries: false,
			expExpired:    true,
		},
		{
			name: "series from different table marked as processed",
			seriesToMarkProcessed: []markSeriesProcessed{
				{
					userID:    user1,
					seriesID:  []byte(lblFooBar.String()),
					lbls:      lblFooBar,
					tableName: "t2",
				},
			},
			chunkEntry: chunkEntry{
				userID: user1,
				chk: retention.Chunk{
					From:    10,
					Through: 20,
				},
				lbls:      lblFooBar,
				seriesID:  []byte(lblFooBar.String()),
				tableName: "t1",
			},
			expSkipSeries: false,
			expExpired:    true,
		},
		{
			name: "multiple series marked as processed",
			seriesToMarkProcessed: []markSeriesProcessed{
				{
					userID:    user1,
					seriesID:  []byte(lblFooBar.String()),
					lbls:      lblFooBar,
					tableName: "t1",
				},
				{
					userID:    user1,
					seriesID:  []byte(lblFooBar.String()),
					lbls:      lblFooBar,
					tableName: "t2",
				},
				{
					userID:    user2,
					seriesID:  []byte(lblFooBar.String()),
					lbls:      lblFooBar,
					tableName: "t1",
				},
			},
			chunkEntry: chunkEntry{
				userID: user1,
				chk: retention.Chunk{
					From:    10,
					Through: 20,
				},
				lbls:      lblFooBar,
				seriesID:  []byte(lblFooBar.String()),
				tableName: "t1",
			},
			expSkipSeries: true,
			expExpired:    false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workingDir := t.TempDir()
			deleteRequestsStore := &mockDeleteRequestsStore{deleteRequests: []DeleteRequest{
				{RequestID: "1", Query: lblFooBar.String(), UserID: string(user1), StartTime: 0, EndTime: 100, Status: StatusReceived},
				{RequestID: "2", Query: lblFooBar.String(), UserID: string(user2), StartTime: 0, EndTime: 100, Status: StatusReceived},
			}}

			mgr, err := NewDeleteRequestsManager(workingDir, deleteRequestsStore, time.Hour, 70, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}}, false, nil, nil)
			require.NoError(t, err)
			require.NoError(t, mgr.Init(nil, nil))

			wg := sync.WaitGroup{}
			mgrCtx, mgrCtxCancel := context.WithCancel(context.Background())
			wg.Add(1)
			go func() {
				defer wg.Done()
				mgr.Start(mgrCtx)
			}()

			mgr.MarkPhaseStarted()
			require.NotNil(t, mgr.currentBatch)

			for _, m := range tc.seriesToMarkProcessed {
				require.NoError(t, mgr.MarkSeriesAsProcessed(m.userID, m.seriesID, m.lbls, m.tableName))
			}

			require.Equal(t, tc.expSkipSeries, mgr.CanSkipSeries(tc.chunkEntry.userID, tc.chunkEntry.lbls, tc.chunkEntry.seriesID, 0, tc.chunkEntry.tableName, 0))
			isExpired, _ := mgr.Expired(tc.chunkEntry.userID, tc.chunkEntry.chk, tc.chunkEntry.lbls, tc.chunkEntry.seriesID, tc.chunkEntry.tableName, 0)
			require.Equal(t, tc.expExpired, isExpired)

			// see if stopping the manager properly retains the progress and loads back when initialized
			storedSeriesProgress := getAllSeriesProgressKeys(t, mgr.seriesProgress)
			mgrCtxCancel()
			wg.Wait()

			mgr, err = NewDeleteRequestsManager(workingDir, deleteRequestsStore, time.Hour, 70, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}}, false, nil, nil)
			require.NoError(t, err)
			require.NoError(t, mgr.Init(nil, nil))
			require.Equal(t, storedSeriesProgress, getAllSeriesProgressKeys(t, mgr.seriesProgress))
			mgr.MarkPhaseStarted()
			require.NotNil(t, mgr.currentBatch)

			// when the mark phase ends, series progress should get cleared
			mgr.MarkPhaseFinished()
			require.Len(t, getAllSeriesProgressKeys(t, mgr.seriesProgress), 0)
		})
	}
}

func TestDeleteRequestsManager_SeriesProgressWithTimeout(t *testing.T) {
	workingDir := t.TempDir()

	user1 := []byte("user1")
	lblFooBar := mustParseLabel(`{foo="bar"}`)
	deleteRequestsStore := &mockDeleteRequestsStore{deleteRequests: []DeleteRequest{
		{RequestID: "1", Query: lblFooBar.String(), UserID: string(user1), StartTime: 0, EndTime: 100, Status: StatusReceived},
		{RequestID: "1", Query: lblFooBar.String(), UserID: string(user1), StartTime: 100, EndTime: 200, Status: StatusReceived},
	}}

	mgr, err := NewDeleteRequestsManager(workingDir, deleteRequestsStore, time.Hour, 70, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}}, false, nil, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.Init(nil, nil))
	mgr.MarkPhaseStarted()
	require.NotNil(t, mgr.currentBatch)

	require.NoError(t, mgr.MarkSeriesAsProcessed(user1, []byte(lblFooBar.String()), lblFooBar, "t1"))

	// timeout the retention processing
	mgr.MarkPhaseTimedOut()

	// timeout should not clear the series progress
	mgr.MarkPhaseFinished()
	require.Len(t, getAllSeriesProgressKeys(t, mgr.seriesProgress), 2)
	require.FileExists(t, filepath.Join(workingDir, seriesProgressFilename))

	// load the requests again for processing
	mgr.MarkPhaseStarted()
	require.NotNil(t, mgr.currentBatch)

	// not hitting the timeout should clear the series progress
	mgr.MarkPhaseFinished()
	require.Len(t, getAllSeriesProgressKeys(t, mgr.seriesProgress), 0)
}

func TestDeleteRequestsManagerWithoutHorizontalScalingMode_SeriesProgress(t *testing.T) {
	workingDir := t.TempDir()

	user1 := []byte("user1")
	lblFooBar := mustParseLabel(`{foo="bar"}`)
	deleteRequestsStore := &mockDeleteRequestsStore{deleteRequests: []DeleteRequest{
		{RequestID: "1", Query: lblFooBar.String(), UserID: string(user1), StartTime: 0, EndTime: 100, Status: StatusReceived},
		{RequestID: "1", Query: lblFooBar.String(), UserID: string(user1), StartTime: 100, EndTime: 200, Status: StatusReceived},
	}}

	mgr, err := NewDeleteRequestsManager(workingDir, deleteRequestsStore, time.Hour, 70, &fakeLimits{defaultLimit: limit{deletionMode: deletionmode.FilterAndDelete.String()}}, true, nil, nil)
	require.NoError(t, err)
	require.NoError(t, mgr.Init(struct{ TablesManager }{}, nil))

	// series progress file should not have been created
	require.Nil(t, mgr.seriesProgress)
	require.NoFileExists(t, filepath.Join(workingDir, seriesProgressFilename))

	mgr.MarkPhaseStarted()
	require.NotNil(t, mgr.currentBatch)

	// MarkSeriesAsProcessed should be just ignored and not fail
	require.NoError(t, mgr.MarkSeriesAsProcessed(user1, []byte(lblFooBar.String()), lblFooBar, "t1"))
	// expiry check should not fail either
	isExpired, _ := mgr.Expired(user1, retention.Chunk{From: 0, Through: 50}, lblFooBar, []byte(lblFooBar.String()), "t1", model.Now())
	require.True(t, isExpired)

	// mark phases should not cause any trouble due to a nil series progress file reference
	mgr.MarkPhaseTimedOut()
	mgr.MarkPhaseFinished()

	// series progress file should still not be present
	require.Nil(t, mgr.seriesProgress)
	require.NoFileExists(t, filepath.Join(workingDir, seriesProgressFilename))

	// load the requests again for processing
	mgr.MarkPhaseStarted()
	require.NotNil(t, mgr.currentBatch)

	// do not hit the timeout this time and see if things continue to work as usual
	mgr.MarkPhaseFinished()

	// series progress file should still not be present
	require.Nil(t, mgr.seriesProgress)
	require.NoFileExists(t, filepath.Join(workingDir, seriesProgressFilename))
}

type storeAddReqDetails struct {
	userID, query      string
	startTime, endTime model.Time
	shardByInterval    time.Duration
}

type removeReqDetails struct {
	userID, reqID string
}

type mockDeleteRequestsStore struct {
	DeleteRequestsStore
	deleteRequests           []DeleteRequest
	addReq                   storeAddReqDetails
	addErr                   error
	returnZeroDeleteRequests bool

	removeReqs removeReqDetails
	removeErr  error

	getUser   string
	getID     string
	getResult []DeleteRequest
	getErr    error

	getAllUser                           string
	getAllResult                         []DeleteRequest
	getAllErr                            error
	getAllRequestedForQuerytimeFiltering bool

	genNumber string
}

func (m *mockDeleteRequestsStore) GetUnprocessedShards(_ context.Context) ([]DeleteRequest, error) {
	return m.getDeleteRequestsByStatus(StatusReceived)
}

func (m *mockDeleteRequestsStore) getDeleteRequestsByStatus(status DeleteRequestStatus) ([]DeleteRequest, error) {
	reqs := make([]DeleteRequest, 0, len(m.deleteRequests))
	for i := range m.deleteRequests {
		if m.deleteRequests[i].Status == status {
			reqs = append(reqs, m.deleteRequests[i])
		}
	}
	return reqs, nil
}

func (m *mockDeleteRequestsStore) GetAllRequests(_ context.Context) ([]DeleteRequest, error) {
	return m.deleteRequests, nil
}

func (m *mockDeleteRequestsStore) AddDeleteRequest(_ context.Context, userID, query string, startTime, endTime model.Time, shardByInterval time.Duration) (string, error) {
	m.addReq = storeAddReqDetails{
		userID:          userID,
		query:           query,
		startTime:       startTime,
		endTime:         endTime,
		shardByInterval: shardByInterval,
	}
	return "", m.addErr
}

func (m *mockDeleteRequestsStore) RemoveDeleteRequest(_ context.Context, userID string, requestID string) error {
	m.removeReqs = removeReqDetails{
		userID: userID,
		reqID:  requestID,
	}
	return m.removeErr
}

func (m *mockDeleteRequestsStore) GetDeleteRequest(_ context.Context, userID, requestID string) (DeleteRequest, error) {
	m.getUser = userID
	m.getID = requestID
	if m.getErr != nil {
		return DeleteRequest{}, m.getErr
	}
	return m.getResult[0], m.getErr
}

func (m *mockDeleteRequestsStore) GetAllDeleteRequestsForUser(_ context.Context, userID string, forQuerytimeFiltering bool) ([]DeleteRequest, error) {
	m.getAllUser = userID
	m.getAllRequestedForQuerytimeFiltering = forQuerytimeFiltering
	return m.getAllResult, m.getAllErr
}

func (m *mockDeleteRequestsStore) GetCacheGenerationNumber(_ context.Context, _ string) (string, error) {
	return m.genNumber, m.getErr
}

func (m *mockDeleteRequestsStore) MarkShardAsProcessed(_ context.Context, req DeleteRequest) error {
	for i := range m.deleteRequests {
		if requestsAreEqual(m.deleteRequests[i], req) {
			m.deleteRequests[i].Status = StatusProcessed
		}
	}

	return nil
}

func (m *mockDeleteRequestsStore) MergeShardedRequests(_ context.Context) error {
	return nil
}

func requestsAreEqual(req1, req2 DeleteRequest) bool {
	if req1.RequestID == req2.RequestID &&
		req1.UserID == req2.UserID &&
		req1.Query == req2.Query &&
		req1.StartTime == req2.StartTime &&
		req1.EndTime == req2.EndTime &&
		req1.SequenceNum == req2.SequenceNum &&
		req1.Status == req2.Status {
		return true
	}

	return false
}

func getAllSeriesProgressKeys(t *testing.T, db *bbolt.DB) map[string]struct{} {
	keys := make(map[string]struct{})
	err := db.View(func(tx *bbolt.Tx) error {
		return tx.Bucket(boltdbBucketName).ForEach(func(k, _ []byte) error {
			keys[string(k)] = struct{}{}
			return nil
		})
	})
	require.NoError(t, err)

	return keys
}
