package queryrange

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func TestEngineRouter_split(t *testing.T) {
	now := time.Now().Truncate(time.Second)                           // truncate to align with step
	v2Start, v2End := now.Add(-2*24*time.Hour), now.Add(-2*time.Hour) // -2d to -2h

	baseReq := &LokiRequest{
		Query:     `{app="foo"}`,
		Step:      1000, // 1 second step
		Direction: logproto.BACKWARD,
		Path:      "/query",
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(`{app="foo"}`),
		},
	}

	tests := []struct {
		name           string
		start          time.Time
		end            time.Time
		forMetricQuery bool
		expectedV1Reqs []queryrangebase.Request
		expectedV2Req  queryrangebase.Request
	}{
		{
			name:           "query entirely within V2 range",
			start:          now.Add(-24 * time.Hour), // 1 day ago
			end:            now.Add(-3 * time.Hour),  // 3 hours ago
			forMetricQuery: true,
			expectedV2Req:  baseReq.WithStartEnd(now.Add(-24*time.Hour), now.Add(-3*time.Hour)),
		},
		{
			name:           "query overlaps V2 range on left",
			start:          now.Add(-3 * 24 * time.Hour), // 3 days ago
			end:            now.Add(-24 * time.Hour),     // 1 day ago
			forMetricQuery: true,
			expectedV1Reqs: []queryrangebase.Request{baseReq.WithStartEnd(
				now.Add(-3*24*time.Hour),
				now.Add(-2*24*time.Hour).Add(-time.Second), // step gap between splits
			)},
			expectedV2Req: baseReq.WithStartEnd(now.Add(-2*24*time.Hour), now.Add(-24*time.Hour)),
		},
		{
			name:           "query overlaps V2 range on right",
			start:          now.Add(-24 * time.Hour), // 1 day ago
			end:            now.Add(-time.Hour),      // 1 hour ago
			forMetricQuery: true,
			expectedV1Reqs: []queryrangebase.Request{baseReq.WithStartEnd(now.Add(-2*time.Hour), now.Add(-time.Hour))},
			expectedV2Req: baseReq.WithStartEnd(
				now.Add(-24*time.Hour),
				now.Add(-2*time.Hour).Add(-time.Second), // step gap between splits
			),
		},
		{
			name:           "query spans entire V2 range",
			start:          now.Add(-3 * 24 * time.Hour), // 3 days ago
			end:            now.Add(-time.Hour),          // 1 hour ago
			forMetricQuery: true,
			expectedV1Reqs: []queryrangebase.Request{
				baseReq.WithStartEnd(
					now.Add(-3*24*time.Hour),
					now.Add(-2*24*time.Hour).Add(-time.Second), // step gap between splits
				),
				baseReq.WithStartEnd(now.Add(-2*time.Hour), now.Add(-time.Hour)),
			},
			expectedV2Req: baseReq.WithStartEnd(
				now.Add(-2*24*time.Hour),
				now.Add(-2*time.Hour).Add(-time.Second), // step gap between splits
			),
		},
		{
			name:           "query spans entire V2, no split gap for log queries",
			start:          now.Add(-3 * 24 * time.Hour), // 3 days ago
			end:            now.Add(-time.Hour),          // 1 hour ago
			forMetricQuery: false,                        // no gaps between splits for log queries
			expectedV1Reqs: []queryrangebase.Request{
				baseReq.WithStartEnd(
					now.Add(-3*24*time.Hour),
					now.Add(-2*24*time.Hour), // step gap between splits
				),
				baseReq.WithStartEnd(now.Add(-2*time.Hour), now.Add(-time.Hour)),
			},
			expectedV2Req: baseReq.WithStartEnd(
				now.Add(-2*24*time.Hour),
				now.Add(-2*time.Hour),
			),
		},
		{
			name:           "query overlaps V2 range exactly",
			start:          now.Add(-2 * 24 * time.Hour),
			end:            now.Add(-2 * time.Hour),
			forMetricQuery: true,
			expectedV2Req:  baseReq.WithStartEnd(now.Add(-2*24*time.Hour), now.Add(-2*time.Hour)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			splitter := &engineRouter{forMetricQuery: tt.forMetricQuery}
			splits := splitter.splitOverlapping(baseReq.WithStartEnd(tt.start, tt.end), v2Start, v2End)

			var (
				gotV1 []queryrangebase.Request
			)
			for _, split := range splits {
				if split.isV2Engine {
					if tt.expectedV2Req != nil {
						require.Equal(t, tt.expectedV2Req, split.req)
					} else {
						require.Fail(t, "v2 req is not expected")
					}
				} else {
					gotV1 = append(gotV1, split.req)
				}
			}

			require.EqualValues(t, tt.expectedV1Reqs, gotV1)
		})
	}
}

func TestEngineRouter_stepAlignment(t *testing.T) {
	now := time.Date(2025, 1, 15, 4, 30, 10, 500, time.UTC) // all ts would be off by 500ms when using 1s step.
	v2Start, v2End := now.Add(-2*24*time.Hour), now.Add(-2*time.Hour)

	buildReq := func(start, end time.Time, step int64) *LokiRequest {
		return &LokiRequest{
			Query:     `{app="foo"}`,
			StartTs:   start,
			EndTs:     end,
			Step:      step,
			Direction: logproto.BACKWARD,
			Path:      "/query",
			Plan: &plan.QueryPlan{
				AST: syntax.MustParseExpr(`{app="foo"}`),
			},
		}
	}

	tests := []struct {
		name           string
		req            queryrangebase.Request
		forMetricQuery bool
		expectedV1Reqs []queryrangebase.Request
		expectedV2Req  queryrangebase.Request
	}{
		{
			// 1s step causes the ms to be rounded up/down
			name:           "splits are aligned to step",
			req:            buildReq(now.Add(-3*24*time.Hour), now.Add(-time.Hour), 1000),
			forMetricQuery: true,
			expectedV1Reqs: []queryrangebase.Request{
				buildReq(
					now.Add(-3*24*time.Hour).Truncate(time.Second),                   // start of query is rounded down
					now.Add(-2*24*time.Hour).Truncate(time.Second).Add(-time.Second), // v2 start rounded down, minus step gap
					1000,
				),
				buildReq(
					now.Add(-2*time.Hour).Truncate(time.Second).Add(time.Second), // v2 end is rounded up
					now.Add(-time.Hour).Truncate(time.Second).Add(time.Second),   // end is rounded up
					1000,
				),
			},
			expectedV2Req: buildReq(
				now.Add(-2*24*time.Hour).Truncate(time.Second), // v2 start is rounded down
				now.Add(-2*time.Hour).Truncate(time.Second),    // v2 end is rounded up, minus step gap
				1000,
			),
		},
		{
			name:           "splits are aligned to step, no split gap for log queries",
			req:            buildReq(now.Add(-3*24*time.Hour), now.Add(-time.Hour), 1000),
			forMetricQuery: false, // no gaps between splits for log queries
			expectedV1Reqs: []queryrangebase.Request{
				buildReq(
					now.Add(-3*24*time.Hour).Truncate(time.Second), // start of query is rounded down
					now.Add(-2*24*time.Hour).Truncate(time.Second), // v2 start rounded down, no step gap
					1000,
				),
				buildReq(
					now.Add(-2*time.Hour).Truncate(time.Second).Add(time.Second), // v2 end is rounded up
					now.Add(-time.Hour).Truncate(time.Second).Add(time.Second),   // end is rounded up
					1000,
				),
			},
			expectedV2Req: buildReq(
				now.Add(-2*24*time.Hour).Truncate(time.Second),               // v2 start is rounded down
				now.Add(-2*time.Hour).Truncate(time.Second).Add(time.Second), // v2 end is rounded up, no step gap
				1000,
			),
		},
		{
			name:           "splits are aligned to 3s step",
			req:            buildReq(now.Add(-3*24*time.Hour), now.Add(-time.Hour), 3000),
			forMetricQuery: true,
			expectedV1Reqs: []queryrangebase.Request{
				buildReq(
					now.Add(-3*24*time.Hour).Truncate(3*time.Second),
					now.Add(-2*24*time.Hour).Truncate(3*time.Second).Add(-3*time.Second), // rounded down, minus step gap
					3000,
				),
				buildReq(
					now.Add(-2*time.Hour).Truncate(3*time.Second).Add(3*time.Second),
					now.Add(-time.Hour).Truncate(3*time.Second).Add(3*time.Second),
					3000,
				),
			},
			expectedV2Req: buildReq(
				now.Add(-2*24*time.Hour).Truncate(3*time.Second),
				now.Add(-2*time.Hour).Truncate(3*time.Second), // rounded up, minus step gap
				3000,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			splitter := &engineRouter{forMetricQuery: tt.forMetricQuery}
			splits := splitter.splitOverlapping(tt.req, v2Start, v2End)

			var (
				gotV1 []queryrangebase.Request
			)
			for _, split := range splits {
				if split.isV2Engine {
					if tt.expectedV2Req != nil {
						require.Equal(t, tt.expectedV2Req, split.req)
					} else {
						require.Fail(t, "v2 req is not expected")
					}
				} else {
					gotV1 = append(gotV1, split.req)
				}
			}

			require.EqualValues(t, tt.expectedV1Reqs, gotV1)
		})
	}
}
