package deletion

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
)

func TestDeleteRequest_IsDeleted(t *testing.T) {
	now := model.Now()
	user1 := "user1"

	lbl := `{foo="bar", fizz="buzz"}`
	lblWithFilter := `{foo="bar", fizz="buzz"} |= "filter"`

	chunkEntry := retention.ChunkEntry{
		ChunkRef: retention.ChunkRef{
			UserID:  []byte(user1),
			From:    now.Add(-3 * time.Hour),
			Through: now.Add(-time.Hour),
		},
		Labels: mustParseLabel(lbl),
	}

	type resp struct {
		isDeleted           bool
		nonDeletedIntervals []retention.IntervalFilter
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
				Query:     lbl,
			},
			expectedResp: resp{
				isDeleted:           true,
				nonDeletedIntervals: nil,
			},
		},
		{
			name: "whole chunk deleted with filter present",
			deleteRequest: DeleteRequest{
				UserID:    user1,
				StartTime: now.Add(-3 * time.Hour),
				EndTime:   now.Add(-time.Hour),
				Query:     lblWithFilter,
			},
			expectedResp: resp{
				isDeleted: true,
				nonDeletedIntervals: []retention.IntervalFilter{
					{
						Interval: model.Interval{
							Start: now.Add(-3 * time.Hour),
							End:   now.Add(-time.Hour),
						},
					},
				},
			},
		},
		{
			name: "chunk deleted from beginning",
			deleteRequest: DeleteRequest{
				UserID:    user1,
				StartTime: now.Add(-3 * time.Hour),
				EndTime:   now.Add(-2 * time.Hour),
				Query:     lbl,
			},
			expectedResp: resp{
				isDeleted: true,
				nonDeletedIntervals: []retention.IntervalFilter{
					{
						Interval: model.Interval{
							Start: now.Add(-2*time.Hour) + 1,
							End:   now.Add(-time.Hour),
						},
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
				Query:     lbl,
			},
			expectedResp: resp{
				isDeleted: true,
				nonDeletedIntervals: []retention.IntervalFilter{
					{
						Interval: model.Interval{
							Start: now.Add(-3 * time.Hour),
							End:   now.Add(-2*time.Hour) - 1,
						},
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
				Query:     lbl,
			},
			expectedResp: resp{
				isDeleted: true,
				nonDeletedIntervals: []retention.IntervalFilter{
					{
						Interval: model.Interval{
							Start: now.Add(-3 * time.Hour),
							End:   now.Add(-2*time.Hour) - 1,
						},
					},
				},
			},
		},
		{
			name: "chunk deleted from end with filter",
			deleteRequest: DeleteRequest{
				UserID:    user1,
				StartTime: now.Add(-2 * time.Hour),
				EndTime:   now,
				Query:     lblWithFilter,
			},
			expectedResp: resp{
				isDeleted: true,
				nonDeletedIntervals: []retention.IntervalFilter{
					{
						Interval: model.Interval{
							Start: now.Add(-3 * time.Hour),
							End:   now.Add(-2*time.Hour) - 1,
						},
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
				Query:     lbl,
			},
			expectedResp: resp{
				isDeleted: true,
				nonDeletedIntervals: []retention.IntervalFilter{
					{
						Interval: model.Interval{
							Start: now.Add(-3 * time.Hour),
							End:   now.Add(-(2*time.Hour + 30*time.Minute)) - 1,
						},
					},
					{
						Interval: model.Interval{
							Start: now.Add(-(time.Hour + 30*time.Minute)) + 1,
							End:   now.Add(-time.Hour),
						},
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
				Query:     lbl,
			},
			expectedResp: resp{
				isDeleted: false,
			},
		},
		{
			name: "request not matching due to matchers",
			deleteRequest: DeleteRequest{
				UserID:    "user1",
				StartTime: now.Add(-3 * time.Hour),
				EndTime:   now.Add(-time.Hour),
				Query:     `{foo1="bar"}`,
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
				Query:     lbl,
			},
			expectedResp: resp{
				isDeleted: false,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, tc.deleteRequest.SetQuery(tc.deleteRequest.Query))
			isDeleted, nonDeletedIntervals := tc.deleteRequest.IsDeleted(chunkEntry)
			require.Equal(t, tc.expectedResp.isDeleted, isDeleted)
			for idx := range tc.expectedResp.nonDeletedIntervals {
				require.Equal(t,
					tc.expectedResp.nonDeletedIntervals[idx].Interval.Start,
					nonDeletedIntervals[idx].Interval.Start,
				)
				require.Equal(t,
					tc.expectedResp.nonDeletedIntervals[idx].Interval.End,
					nonDeletedIntervals[idx].Interval.End,
				)
			}
		})
	}
}

func mustParseLabel(input string) labels.Labels {
	lbls, err := syntax.ParseLabels(input)
	if err != nil {
		panic(err)
	}

	return lbls
}

func TestDeleteRequest_FilterFunction(t *testing.T) {
	dr := DeleteRequest{
		Query: `{foo="bar"} |= "some"`,
	}

	lblStr := `{foo="bar"}`
	lbls := mustParseLabel(lblStr)

	f, err := dr.FilterFunction(lbls)
	require.NoError(t, err)

	require.True(t, f(`some line`))
	require.False(t, f(""))
	require.False(t, f("other line"))

	lblStr = `{foo2="buzz"}`
	lbls = mustParseLabel(lblStr)

	f, err = dr.FilterFunction(lbls)
	require.NoError(t, err)

	require.False(t, f(""))
	require.False(t, f("other line"))
	require.False(t, f("some line"))

	dr = DeleteRequest{
		Query: `{namespace="default"}`,
	}

	lblStr = `{namespace="default"}`
	lbls = mustParseLabel(lblStr)

	f, err = dr.FilterFunction(lbls)
	require.NoError(t, err)

	require.True(t, f(`some line`))
	require.True(t, f(""))
	require.True(t, f("other line"))

}
