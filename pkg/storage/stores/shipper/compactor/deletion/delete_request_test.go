package deletion

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
)

func TestDeleteRequest_IsDeleted(t *testing.T) {
	now := model.Now()
	user1 := "user1"

	lbls := `{foo="bar", fizz="buzz"}`

	chunkEntry := retention.ChunkEntry{
		ChunkRef: retention.ChunkRef{
			UserID:  []byte(user1),
			From:    now.Add(-3 * time.Hour),
			Through: now.Add(-time.Hour),
		},
		Labels: mustParseLabel(lbls),
	}

	type resp struct {
		isDeleted           bool
		nonDeletedIntervals []model.Interval
	}

	for _, tc := range []struct {
		name          string
		deleteRequest DeleteRequest
		expectedResp  resp
	}{
		{
			name: "whole chunk deleted",
			deleteRequest: DeleteRequest{
				UserID:    user1,
				StartTime: now.Add(-3 * time.Hour),
				EndTime:   now.Add(-time.Hour),
				Selectors: []string{lbls},
			},
			expectedResp: resp{
				isDeleted:           true,
				nonDeletedIntervals: nil,
			},
		},
		{
			name: "chunk deleted from beginning",
			deleteRequest: DeleteRequest{
				UserID:    user1,
				StartTime: now.Add(-3 * time.Hour),
				EndTime:   now.Add(-2 * time.Hour),
				Selectors: []string{lbls},
			},
			expectedResp: resp{
				isDeleted: true,
				nonDeletedIntervals: []model.Interval{
					{
						Start: now.Add(-2*time.Hour) + 1,
						End:   now.Add(-time.Hour),
					},
				},
			},
		},
		{
			name: "chunk deleted from end",
			deleteRequest: DeleteRequest{
				UserID:    user1,
				StartTime: now.Add(-2 * time.Hour),
				EndTime:   now,
				Selectors: []string{lbls},
			},
			expectedResp: resp{
				isDeleted: true,
				nonDeletedIntervals: []model.Interval{
					{
						Start: now.Add(-3 * time.Hour),
						End:   now.Add(-2*time.Hour) - 1,
					},
				},
			},
		},
		{
			name: "chunk deleted from end",
			deleteRequest: DeleteRequest{
				UserID:    user1,
				StartTime: now.Add(-2 * time.Hour),
				EndTime:   now,
				Selectors: []string{lbls},
			},
			expectedResp: resp{
				isDeleted: true,
				nonDeletedIntervals: []model.Interval{
					{
						Start: now.Add(-3 * time.Hour),
						End:   now.Add(-2*time.Hour) - 1,
					},
				},
			},
		},
		{
			name: "chunk deleted in the middle",
			deleteRequest: DeleteRequest{
				UserID:    user1,
				StartTime: now.Add(-(2*time.Hour + 30*time.Minute)),
				EndTime:   now.Add(-(time.Hour + 30*time.Minute)),
				Selectors: []string{lbls},
			},
			expectedResp: resp{
				isDeleted: true,
				nonDeletedIntervals: []model.Interval{
					{
						Start: now.Add(-3 * time.Hour),
						End:   now.Add(-(2*time.Hour + 30*time.Minute)) - 1,
					},
					{
						Start: now.Add(-(time.Hour + 30*time.Minute)) + 1,
						End:   now.Add(-time.Hour),
					},
				},
			},
		},
		{
			name: "delete request out of range",
			deleteRequest: DeleteRequest{
				UserID:    user1,
				StartTime: now.Add(-12 * time.Hour),
				EndTime:   now.Add(-10 * time.Hour),
				Selectors: []string{lbls},
			},
			expectedResp: resp{
				isDeleted: false,
			},
		},
		{
			name: "request not matching due to matchers",
			deleteRequest: DeleteRequest{
				UserID:    user1,
				StartTime: now.Add(-3 * time.Hour),
				EndTime:   now.Add(-time.Hour),
				Selectors: []string{`{foo1="bar"}`, `{fizz1="buzz"}`},
			},
			expectedResp: resp{
				isDeleted: false,
			},
		},
		{
			name: "request for a different user",
			deleteRequest: DeleteRequest{
				UserID:    "user2",
				StartTime: now.Add(-3 * time.Hour),
				EndTime:   now.Add(-time.Hour),
				Selectors: []string{lbls},
			},
			expectedResp: resp{
				isDeleted: false,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isDeleted, nonDeletedIntervals := tc.deleteRequest.IsDeleted(chunkEntry)
			require.Equal(t, tc.expectedResp.isDeleted, isDeleted)
			require.Equal(t, tc.expectedResp.nonDeletedIntervals, nonDeletedIntervals)
		})
	}
}

func mustParseLabel(input string) labels.Labels {
	lbls, err := logql.ParseLabels(input)
	if err != nil {
		panic(err)
	}

	return lbls
}
