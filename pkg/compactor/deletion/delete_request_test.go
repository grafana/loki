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

	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/filter"
)

const (
	lblFooBar = `{foo="bar"}`
	lblPing   = "ping"
	lblPong   = "pong"
)

func TestDeleteRequest_GetChunkFilter(t *testing.T) {
	now := model.Now()
	user1 := "user1"

	lbl := `{foo="bar", fizz="buzz"}`
	lblWithLineFilter := `{foo="bar", fizz="buzz"} |= "filter"`

	lblWithStructuredMetadataFilter := `{foo="bar", fizz="buzz"} | ping="pong"`
	lblWithLineAndStructuredMetadataFilter := `{foo="bar", fizz="buzz"} | ping="pong" |= "filter"`

	chunkEntry := retention.Chunk{
		From:    now.Add(-3 * time.Hour),
		Through: now.Add(-time.Hour),
	}

	type resp struct {
		isDeleted      bool
		expectedFilter filter.Func
	}

	for _, tc := range []struct {
		name          string
		deleteRequest deleteRequest
		expectedResp  resp
	}{
		{
			name: "whole chunk deleted",
			deleteRequest: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					UserID:    user1,
					StartTime: now.Add(-3 * time.Hour),
					EndTime:   now.Add(-time.Hour),
					Query:     lbl,
				},
			},
			expectedResp: resp{
				isDeleted: true,
			},
		},
		{
			name: "whole chunk deleted with line filter present",
			deleteRequest: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					UserID:    user1,
					StartTime: now.Add(-3 * time.Hour),
					EndTime:   now.Add(-time.Hour),
					Query:     lblWithLineFilter,
				},
			},
			expectedResp: resp{
				isDeleted: true,
				expectedFilter: func(ts time.Time, s string, _ labels.Labels) bool {
					tsUnixNano := ts.UnixNano()
					if strings.Contains(s, "filter") && now.Add(-3*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.Add(-time.Hour).UnixNano() {
						return true
					}
					return false
				},
			},
		},
		{
			name: "whole chunk deleted with structured metadata filter present",
			deleteRequest: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					UserID:    user1,
					StartTime: now.Add(-3 * time.Hour),
					EndTime:   now.Add(-time.Hour),
					Query:     lblWithStructuredMetadataFilter,
				},
			},
			expectedResp: resp{
				isDeleted: true,
				expectedFilter: func(ts time.Time, _ string, structuredMetadata labels.Labels) bool {
					tsUnixNano := ts.UnixNano()
					if structuredMetadata.Get(lblPing) == lblPong && now.Add(-3*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.Add(-time.Hour).UnixNano() {
						return true
					}
					return false
				},
			},
		},
		{
			name: "whole chunk deleted with line and structured metadata filter present",
			deleteRequest: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					UserID:    user1,
					StartTime: now.Add(-3 * time.Hour),
					EndTime:   now.Add(-time.Hour),
					Query:     lblWithLineAndStructuredMetadataFilter,
				},
			},
			expectedResp: resp{
				isDeleted: true,
				expectedFilter: func(ts time.Time, s string, structuredMetadata labels.Labels) bool {
					tsUnixNano := ts.UnixNano()
					if strings.Contains(s, "filter") && structuredMetadata.Get(lblPing) == lblPong && now.Add(-3*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.Add(-time.Hour).UnixNano() {
						return true
					}
					return false
				},
			},
		},
		{
			name: "chunk deleted from beginning",
			deleteRequest: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					UserID:    user1,
					StartTime: now.Add(-3 * time.Hour),
					EndTime:   now.Add(-2 * time.Hour),
					Query:     lbl,
				},
			},
			expectedResp: resp{
				isDeleted: true,
				expectedFilter: func(ts time.Time, _ string, _ labels.Labels) bool {
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
			deleteRequest: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					UserID:    user1,
					StartTime: now.Add(-2 * time.Hour),
					EndTime:   now,
					Query:     lbl,
				},
			},
			expectedResp: resp{
				isDeleted: true,
				expectedFilter: func(ts time.Time, _ string, _ labels.Labels) bool {
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
			deleteRequest: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					UserID:    user1,
					StartTime: now.Add(-2 * time.Hour),
					EndTime:   now,
					Query:     lblWithLineFilter,
				},
			},
			expectedResp: resp{
				isDeleted: true,
				expectedFilter: func(ts time.Time, s string, _ labels.Labels) bool {
					tsUnixNano := ts.UnixNano()
					if strings.Contains(s, "filter") && now.Add(-2*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.UnixNano() {
						return true
					}
					return false
				},
			},
		},
		{
			name: "chunk deleted from end with structured metadata filter present",
			deleteRequest: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					UserID:    user1,
					StartTime: now.Add(-2 * time.Hour),
					EndTime:   now,
					Query:     lblWithStructuredMetadataFilter,
				},
			},
			expectedResp: resp{
				isDeleted: true,
				expectedFilter: func(ts time.Time, _ string, structuredMetadata labels.Labels) bool {
					tsUnixNano := ts.UnixNano()
					if structuredMetadata.Get(lblPing) == lblPong && now.Add(-2*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.UnixNano() {
						return true
					}
					return false
				},
			},
		},
		{
			name: "chunk deleted from end with line and structured metadata filter present",
			deleteRequest: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					UserID:    user1,
					StartTime: now.Add(-2 * time.Hour),
					EndTime:   now,
					Query:     lblWithLineAndStructuredMetadataFilter,
				},
			},
			expectedResp: resp{
				isDeleted: true,
				expectedFilter: func(ts time.Time, s string, structuredMetadata labels.Labels) bool {
					tsUnixNano := ts.UnixNano()
					if strings.Contains(s, "filter") && structuredMetadata.Get(lblPing) == lblPong && now.Add(-2*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= now.UnixNano() {
						return true
					}
					return false
				},
			},
		},
		{
			name: "chunk deleted in the middle",
			deleteRequest: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					UserID:    user1,
					StartTime: now.Add(-(2*time.Hour + 30*time.Minute)),
					EndTime:   now.Add(-(time.Hour + 30*time.Minute)),
					Query:     lbl,
				},
			},
			expectedResp: resp{
				isDeleted: true,
				expectedFilter: func(ts time.Time, _ string, _ labels.Labels) bool {
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
			deleteRequest: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					UserID:    user1,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     lbl,
				},
			},
			expectedResp: resp{
				isDeleted: false,
			},
		},
		{
			name: "request not matching due to matchers",
			deleteRequest: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					UserID:    "user1",
					StartTime: now.Add(-3 * time.Hour),
					EndTime:   now.Add(-time.Hour),
					Query:     `{foo1="bar"}`,
				},
			},
			expectedResp: resp{
				isDeleted: false,
			},
		},
		{
			name: "request for a different user",
			deleteRequest: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					UserID:    "user2",
					StartTime: now.Add(-3 * time.Hour),
					EndTime:   now.Add(-time.Hour),
					Query:     lbl,
				},
			},
			expectedResp: resp{
				isDeleted: false,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, tc.deleteRequest.SetQuery(tc.deleteRequest.Query))
			tc.deleteRequest.TotalLinesDeletedMetric = newDeleteRequestsManagerMetrics(nil).deletedLinesTotal
			isExpired, filterFunc := tc.deleteRequest.GetChunkFilter([]byte(user1), mustParseLabel(lbl), chunkEntry)
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

				// mix of empty, ding=dong and ping=pong as structured metadata
				var structuredMetadata labels.Labels
				if start.Time().Minute()%3 == 0 {
					structuredMetadata = labels.FromStrings(lblPing, lblPong)
				} else if start.Time().Minute()%2 == 0 {
					structuredMetadata = labels.FromStrings("ting", "tong")
				}
				require.Equal(t, tc.expectedResp.expectedFilter(start.Time(), line, structuredMetadata), filterFunc(start.Time(), line, structuredMetadata), "line", line, "time", start.Time(), "now", now.Time())
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
	t.Run("one line matching with line filter", func(t *testing.T) {
		dr := deleteRequest{
			DeleteRequest: deletionproto.DeleteRequest{
				Query:     `{foo="bar"} |= "some"`,
				StartTime: 0,
				EndTime:   math.MaxInt64,
			},
			DeletedLines:            0,
			TotalLinesDeletedMetric: newDeleteRequestsManagerMetrics(prometheus.NewPedanticRegistry()).deletedLinesTotal,
		}

		lblStr := lblFooBar
		lbls := mustParseLabel(lblStr)

		require.NoError(t, dr.SetQuery(dr.Query))
		f, err := dr.FilterFunction(lbls)
		require.NoError(t, err)

		require.True(t, f(time.Now(), `some line`, labels.EmptyLabels()))
		require.False(t, f(time.Now(), "", labels.EmptyLabels()))
		require.False(t, f(time.Now(), "other line", labels.EmptyLabels()))
		require.Equal(t, int32(1), dr.DeletedLines)
		require.Equal(t, float64(1), testutil.ToFloat64(dr.TotalLinesDeletedMetric))
	})

	t.Run("one line matching with structured metadata filter", func(t *testing.T) {
		dr := deleteRequest{
			DeleteRequest: deletionproto.DeleteRequest{
				Query:     `{foo="bar"} | ping="pong"`,
				StartTime: 0,
				EndTime:   math.MaxInt64,
			},
			DeletedLines:            0,
			TotalLinesDeletedMetric: newDeleteRequestsManagerMetrics(prometheus.NewPedanticRegistry()).deletedLinesTotal,
		}

		lblStr := lblFooBar
		lbls := mustParseLabel(lblStr)

		require.NoError(t, dr.SetQuery(dr.Query))
		f, err := dr.FilterFunction(lbls)
		require.NoError(t, err)

		require.True(t, f(time.Now(), `some line`, labels.FromStrings(lblPing, lblPong)))
		require.False(t, f(time.Now(), "", labels.EmptyLabels()))
		require.False(t, f(time.Now(), "some line", labels.EmptyLabels()))
		require.Equal(t, int32(1), dr.DeletedLines)
		require.Equal(t, float64(1), testutil.ToFloat64(dr.TotalLinesDeletedMetric))
	})

	t.Run("one line matching with line and structured metadata filter", func(t *testing.T) {
		dr := deleteRequest{
			DeleteRequest: deletionproto.DeleteRequest{
				Query:     `{foo="bar"} | ping="pong" |= "some"`,
				StartTime: 0,
				EndTime:   math.MaxInt64,
			},
			DeletedLines:            0,
			TotalLinesDeletedMetric: newDeleteRequestsManagerMetrics(prometheus.NewPedanticRegistry()).deletedLinesTotal,
		}

		lblStr := lblFooBar
		lbls := mustParseLabel(lblStr)

		require.NoError(t, dr.SetQuery(dr.Query))
		f, err := dr.FilterFunction(lbls)
		require.NoError(t, err)

		require.True(t, f(time.Now(), `some line`, labels.FromStrings(lblPing, lblPong)))
		require.False(t, f(time.Now(), "", labels.EmptyLabels()))
		require.False(t, f(time.Now(), "some line", labels.EmptyLabels()))
		require.False(t, f(time.Now(), "other line", labels.FromStrings(lblPing, lblPong)))
		require.Equal(t, int32(1), dr.DeletedLines)
		require.Equal(t, float64(1), testutil.ToFloat64(dr.TotalLinesDeletedMetric))
	})

	t.Run("labels not matching", func(t *testing.T) {
		dr := deleteRequest{
			DeleteRequest: deletionproto.DeleteRequest{
				Query:  `{foo="bar"} |= "some"`,
				UserID: "tenant1",
			},
			DeletedLines:            0,
			TotalLinesDeletedMetric: newDeleteRequestsManagerMetrics(prometheus.NewPedanticRegistry()).deletedLinesTotal,
		}

		lblStr := `{foo2="buzz"}`
		lbls := mustParseLabel(lblStr)

		require.NoError(t, dr.SetQuery(dr.Query))
		f, err := dr.FilterFunction(lbls)
		require.NoError(t, err)

		require.False(t, f(time.Time{}, "", labels.EmptyLabels()))
		require.False(t, f(time.Time{}, "other line", labels.EmptyLabels()))
		require.False(t, f(time.Time{}, "some line", labels.EmptyLabels()))
		require.Equal(t, int32(0), dr.DeletedLines)
		// testutil.ToFloat64 panics when there are 0 metrics
		require.Panics(t, func() { testutil.ToFloat64(dr.TotalLinesDeletedMetric) })
	})

	t.Run("no line filter", func(t *testing.T) {
		now := model.Now()
		dr := deleteRequest{
			DeleteRequest: deletionproto.DeleteRequest{
				Query:     `{namespace="default"}`,
				StartTime: now.Add(-time.Hour),
				EndTime:   now,
			},
			DeletedLines:            0,
			TotalLinesDeletedMetric: newDeleteRequestsManagerMetrics(prometheus.NewPedanticRegistry()).deletedLinesTotal,
		}

		lblStr := `{namespace="default"}`
		lbls := mustParseLabel(lblStr)

		require.NoError(t, dr.SetQuery(dr.Query))
		f, err := dr.FilterFunction(lbls)
		require.NoError(t, err)
		require.NotNil(t, f)

		require.True(t, f(now.Time(), `some line`, labels.EmptyLabels()))
		require.False(t, f(now.Time().Add(-2*time.Hour), `some line`, labels.EmptyLabels()))
		require.True(t, f(now.Time(), "other line", labels.EmptyLabels()))

		require.Equal(t, int32(0), dr.DeletedLines)
		// testutil.ToFloat64 panics when there are 0 metrics
		require.Panics(t, func() { testutil.ToFloat64(dr.TotalLinesDeletedMetric) })
	})
}

func TestDeleteRequest_IsDuplicate(t *testing.T) {
	query1 := `{foo="bar", fizz="buzz"} |= "foo"`
	query2 := `{foo="bar", fizz="buzz2"} |= "foo"`

	for _, tc := range []struct {
		name           string
		req1, req2     deleteRequest
		expIsDuplicate bool
	}{
		{
			name: "not duplicate - different user id",
			req1: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "1",
					UserID:    user1,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     query1,
				},
			},
			req2: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "1",
					UserID:    user2,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     query1,
				},
			},
			expIsDuplicate: false,
		},
		{
			name: "not duplicate - same request id",
			req1: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "1",
					UserID:    user1,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     query1,
				},
			},
			req2: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "1",
					UserID:    user1,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     query1,
				},
			},
			expIsDuplicate: false,
		},
		{
			name: "not duplicate - different start time",
			req1: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "1",
					UserID:    user1,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     query1,
				},
			},
			req2: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "2",
					UserID:    user1,
					StartTime: now.Add(-13 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     query1,
				},
			},
		},
		{
			name: "not duplicate - different end time",
			req1: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "1",
					UserID:    user1,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     query1,
				},
			},
			req2: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "2",
					UserID:    user1,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-11 * time.Hour),
					Query:     query1,
				},
			},
		},
		{
			name: "not duplicate - different labels",
			req1: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "1",
					UserID:    user1,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     query1,
				},
			},
			req2: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "2",
					UserID:    user1,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     query2,
				},
			},
		},
		{
			name: "duplicate - same request",
			req1: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "1",
					UserID:    user1,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     query1,
				},
			},
			req2: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "2",
					UserID:    user1,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     query1,
				},
			},
			expIsDuplicate: true,
		},
		{
			name: "duplicate - same request with irregularities in query",
			req1: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "1",
					UserID:    user1,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     query1,
				},
			},
			req2: deleteRequest{
				DeleteRequest: deletionproto.DeleteRequest{
					RequestID: "2",
					UserID:    user1,
					StartTime: now.Add(-12 * time.Hour),
					EndTime:   now.Add(-10 * time.Hour),
					Query:     "{foo=\"bar\",      fizz=`buzz`}     |=     `foo`",
				},
			},
			expIsDuplicate: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isDuplicate, err := tc.req1.IsDuplicate(&tc.req2)
			require.NoError(t, err)
			require.Equal(t, tc.expIsDuplicate, isDuplicate)
		})
	}
}
