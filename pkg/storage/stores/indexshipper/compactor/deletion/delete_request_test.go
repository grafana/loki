package deletion

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/retention"
	"github.com/grafana/loki/pkg/util/filter"
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
		isDeleted      bool
		expectedFilter filter.Func
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
				isDeleted: true,
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
				expectedFilter: func(ts time.Time, s string) bool {
					tsUnixNano := ts.UnixNano()
					if strings.Contains(s, "filter") && now.Add(-3*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.Add(-time.Hour).UnixNano() {
						return true
					}
					return false
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
				expectedFilter: func(ts time.Time, s string) bool {
					tsUnixNano := ts.UnixNano()
					if now.Add(-3*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.Add(-2*time.Hour).UnixNano() {
						return true
					}
					return false
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
				expectedFilter: func(ts time.Time, s string) bool {
					tsUnixNano := ts.UnixNano()
					if now.Add(-2*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.UnixNano() {
						return true
					}
					return false
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
				expectedFilter: func(ts time.Time, s string) bool {
					tsUnixNano := ts.UnixNano()
					if strings.Contains(s, "filter") && now.Add(-2*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.UnixNano() {
						return true
					}
					return false
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
				expectedFilter: func(ts time.Time, s string) bool {
					tsUnixNano := ts.UnixNano()
					if now.Add(-(2*time.Hour+30*time.Minute)).UnixNano() <= tsUnixNano && tsUnixNano <= now.Add(-(time.Hour+30*time.Minute)).UnixNano() {
						return true
					}
					return false
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
			tc.deleteRequest.Metrics = newDeleteRequestsManagerMetrics(nil)
			isExpired, filterFunc := tc.deleteRequest.IsDeleted(chunkEntry)
			require.Equal(t, tc.expectedResp.isDeleted, isExpired)
			if tc.expectedResp.expectedFilter == nil {
				require.Nil(t, filterFunc)
				return
			}
			require.NotNil(t, filterFunc)

			for start := chunkEntry.From; start <= chunkEntry.Through; start = start.Add(time.Minute) {
				line := "foo bar"
				if start.Time().Minute()%2 == 1 {
					line = "filter bar"
				}
				require.Equal(t, tc.expectedResp.expectedFilter(start.Time(), line), filterFunc(start.Time(), line), "line", line, "time", start.Time(), "now", now.Time())
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
	t.Run("one_line_matching", func(t *testing.T) {
		dr := DeleteRequest{
			Query:        `{foo="bar"} |= "some"`,
			DeletedLines: 0,
			Metrics:      newDeleteRequestsManagerMetrics(prometheus.NewPedanticRegistry()),
			StartTime:    0,
			EndTime:      math.MaxInt64,
		}

		lblStr := `{foo="bar"}`
		lbls := mustParseLabel(lblStr)

		require.NoError(t, dr.SetQuery(dr.Query))
		f, err := dr.FilterFunction(lbls)
		require.NoError(t, err)

		require.True(t, f(time.Now(), `some line`))
		require.False(t, f(time.Now(), ""))
		require.False(t, f(time.Now(), "other line"))
		require.Equal(t, int32(1), dr.DeletedLines)
		require.Equal(t, float64(1), testutil.ToFloat64(dr.Metrics.deletedLinesTotal))
	})

	t.Run("labels_not_matching", func(t *testing.T) {
		dr := DeleteRequest{
			Query:        `{foo="bar"} |= "some"`,
			DeletedLines: 0,
			Metrics:      newDeleteRequestsManagerMetrics(prometheus.NewPedanticRegistry()),
			UserID:       "tenant1",
		}

		lblStr := `{foo2="buzz"}`
		lbls := mustParseLabel(lblStr)

		require.NoError(t, dr.SetQuery(dr.Query))
		f, err := dr.FilterFunction(lbls)
		require.NoError(t, err)

		require.False(t, f(time.Time{}, ""))
		require.False(t, f(time.Time{}, "other line"))
		require.False(t, f(time.Time{}, "some line"))
		require.Equal(t, int32(0), dr.DeletedLines)
		// testutil.ToFloat64 panics when there are 0 metrics
		require.Panics(t, func() { testutil.ToFloat64(dr.Metrics.deletedLinesTotal) })
	})

	t.Run("no_line_filter", func(t *testing.T) {
		now := model.Now()
		dr := DeleteRequest{
			Query:        `{namespace="default"}`,
			DeletedLines: 0,
			Metrics:      newDeleteRequestsManagerMetrics(prometheus.NewPedanticRegistry()),
			StartTime:    now.Add(-time.Hour),
			EndTime:      now,
		}

		lblStr := `{namespace="default"}`
		lbls := mustParseLabel(lblStr)

		require.NoError(t, dr.SetQuery(dr.Query))
		f, err := dr.FilterFunction(lbls)
		require.NoError(t, err)
		require.NotNil(t, f)

		require.True(t, f(now.Time(), `some line`))
		require.False(t, f(now.Time().Add(-2*time.Hour), `some line`))
		require.True(t, f(now.Time(), "other line"))

		require.Equal(t, int32(0), dr.DeletedLines)
		// testutil.ToFloat64 panics when there are 0 metrics
		require.Panics(t, func() { testutil.ToFloat64(dr.Metrics.deletedLinesTotal) })
	})
}
