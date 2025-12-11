package queryrange

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
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
			expectedV1Reqs: []queryrangebase.Request{
				// Account for step gap on the post-v2 split
				baseReq.WithStartEnd(now.Add(-2*time.Hour).Add(time.Second), now.Add(-time.Hour)), // Post-v2
			},
			expectedV2Req: baseReq.WithStartEnd(now.Add(-24*time.Hour), now.Add(-2*time.Hour)),
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
				baseReq.WithStartEnd(
					now.Add(-2*time.Hour).Add(time.Second), // step gap between splits
					now.Add(-time.Hour),
				),
			},
			expectedV2Req: baseReq.WithStartEnd(now.Add(-2*24*time.Hour), now.Add(-2*time.Hour)),
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

			// Sort the splits by start time to ensure the splits are in the correct order.
			sort.Slice(splits, func(i, j int) bool {
				return splits[i].req.GetStart().Before(splits[j].req.GetStart())
			})

			// Check the splits respect the original query's start and end times.
			require.Equal(t, tt.start, splits[0].req.GetStart())
			require.Equal(t, tt.end, splits[len(splits)-1].req.GetEnd())

			var gotV1 []queryrangebase.Request
			for _, split := range splits {
				if split.isV2Engine {
					if tt.expectedV2Req != nil {
						assert.False(t, split.req.GetStart().Before(v2Start), "start time of split request should not be before v2 start time")
						assert.False(t, split.req.GetEnd().After(v2End), "end time of split request should not be after v2 end time")

						assert.Equal(t, tt.expectedV2Req, split.req, "unexpected v2 split")
					} else {
						assert.Fail(t, "v2 req is not expected")
					}
				} else {
					gotV1 = append(gotV1, split.req)
				}
			}

			assert.EqualValues(t, tt.expectedV1Reqs, gotV1, "unexpected v1 splits")
		})
	}
}

func TestEngineRouter_stepAlignment(t *testing.T) {
	now := time.Date(2025, 1, 15, 4, 30, 10, 500, time.UTC)           // all ts would be off by 500ns when using 1s step.
	v2Start, v2End := now.Add(-2*24*time.Hour), now.Add(-2*time.Hour) // -2d to -2h

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
					now.Add(-3*24*time.Hour),
					now.Add(-2*24*time.Hour).Truncate(time.Second), // v2 start rounded up, minus one step.
					1000,
				),
				buildReq(
					now.Add(-2*time.Hour).Truncate(time.Second).Add(time.Second), // v2 end is rounded up
					now.Add(-time.Hour),
					1000,
				),
			},
			expectedV2Req: buildReq(
				now.Add(-2*24*time.Hour).Truncate(time.Second).Add(time.Second), // v2 start is rounded up
				now.Add(-2*time.Hour).Truncate(time.Second),                     // v2 end is rounded down.
				1000,
			),
		},
		{
			name:           "splits are aligned to step, no split gap for log queries",
			req:            buildReq(now.Add(-3*24*time.Hour), now.Add(-time.Hour), 1000),
			forMetricQuery: false, // no gaps or alignment for log queries
			expectedV1Reqs: []queryrangebase.Request{
				buildReq(
					now.Add(-3*24*time.Hour),
					now.Add(-2*24*time.Hour), // v2 start, no step gap
					1000,
				),
				buildReq(
					now.Add(-2*time.Hour), // v2 end
					now.Add(-time.Hour),
					1000,
				),
			},
			expectedV2Req: buildReq(
				now.Add(-2*24*time.Hour), // v2 start
				now.Add(-2*time.Hour),    // v2 end
				1000,
			),
		},
		{
			name:           "splits are aligned to 3s step",
			req:            buildReq(now.Add(-3*24*time.Hour), now.Add(-time.Hour), 3000),
			forMetricQuery: true,
			expectedV1Reqs: []queryrangebase.Request{
				buildReq(
					now.Add(-3*24*time.Hour),
					now.Add(-2*24*time.Hour).Truncate(3*time.Second), // one rounded step back from v2 step
					3000,
				),
				buildReq(
					now.Add(-2*time.Hour).Truncate(3*time.Second).Add(3*time.Second), // one step after v2 end
					now.Add(-time.Hour),
					3000,
				),
			},
			expectedV2Req: buildReq(
				now.Add(-2*24*time.Hour).Truncate(3*time.Second).Add(3*time.Second), // rounded up
				now.Add(-2*time.Hour).Truncate(3*time.Second),
				3000,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			splitter := &engineRouter{forMetricQuery: tt.forMetricQuery}
			splits := splitter.splitOverlapping(tt.req, v2Start, v2End)

			sort.Slice(splits, func(i, j int) bool {
				return splits[i].req.GetStart().Before(splits[j].req.GetStart())
			})

			// Check the splits respect the original query's start and end times.
			require.Equal(t, tt.req.GetStart(), splits[0].req.GetStart())
			require.Equal(t, tt.req.GetEnd(), splits[len(splits)-1].req.GetEnd())

			var gotV1 []queryrangebase.Request
			for _, split := range splits {
				if split.isV2Engine {
					if tt.expectedV2Req != nil {
						assert.False(t, split.req.GetStart().Before(v2Start), "start time of split request should not be before v2 start time")
						assert.False(t, split.req.GetEnd().After(v2End), "end time of split request should not be after v2 end time")

						assert.Equal(t, tt.expectedV2Req, split.req, "unexpected v2 request")
					} else {
						assert.Fail(t, "v2 req is not expected")
					}
				} else {
					gotV1 = append(gotV1, split.req)
				}
			}

			assert.EqualValues(t, tt.expectedV1Reqs, gotV1, "unexpected v1 requests")
		})
	}
}

func Test_engineRouter_Do(t *testing.T) {
	buildReponse := func(r queryrangebase.Request, engine string) queryrangebase.Response {
		return &LokiResponse{
			Status:    loghttp.QueryStatusSuccess,
			Direction: r.(*LokiRequest).Direction,
			Limit:     r.(*LokiRequest).Limit,
			Version:   uint32(loghttp.VersionV1),
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result: []logproto.Stream{
					{
						Labels: `{foo="bar", level="debug"}`,
						Entries: []logproto.Entry{
							{Timestamp: time.Unix(0, r.(*LokiRequest).StartTs.UnixNano()), Line: fmt.Sprintf("%d: line from %s engine", r.(*LokiRequest).StartTs.UnixNano(), engine)},
						},
					},
				},
			},
		}
	}

	next := queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		return buildReponse(r, "old"), nil
	})
	v2EngineHandler := queryrangebase.HandlerFunc(func(_ context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
		return buildReponse(r, "new"), nil
	})

	clock := quartz.NewMock(t)
	now := clock.Now().Truncate(time.Second)

	routerConfig := RouterConfig{
		Start:    now.Add(-24 * time.Hour),
		Lag:      time.Hour,
		Validate: func(_ logql.Params) bool { return true },
		Handler:  v2EngineHandler,
	}

	router := NewEngineRouterMiddleware(
		routerConfig,
		[]queryrangebase.Middleware{newEntrySuffixTestMiddleware(" [v1-chain-processed]")},
		DefaultCodec,
		false,
		log.NewNopLogger(),
	).Wrap(next)

	routerImpl := router.(*engineRouter)
	routerImpl.clock = clock

	tests := []struct {
		name string
		req  *LokiRequest
		want *LokiResponse
	}{
		{
			"merge responses",
			&LokiRequest{
				StartTs:   now.Add(-30 * time.Hour),
				EndTs:     now,
				Query:     `{foo="bar"}`,
				Limit:     1000,
				Step:      1000,
				Direction: logproto.BACKWARD,
				Path:      "/api/prom/query_range",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`{foo="bar"}`),
				},
			},
			&LokiResponse{
				Status:     loghttp.QueryStatusSuccess,
				Direction:  logproto.BACKWARD,
				Limit:      1000,
				Version:    1,
				Statistics: stats.Result{Summary: stats.Summary{Splits: 3}},
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result: []logproto.Stream{
						{
							Labels: `{foo="bar", level="debug"}`,
							Entries: []logproto.Entry{
								{Timestamp: now.Add(-time.Hour), Line: fmt.Sprintf("%d: line from old engine [v1-chain-processed]", now.Add(-time.Hour).UnixNano())},
								{Timestamp: now.Add(-24 * time.Hour), Line: fmt.Sprintf("%d: line from new engine", now.Add(-24*time.Hour).UnixNano())},
								{Timestamp: now.Add(-30 * time.Hour), Line: fmt.Sprintf("%d: line from old engine [v1-chain-processed]", now.Add(-30*time.Hour).UnixNano())},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := router.Do(context.Background(), tt.req)
			require.NoError(t, err)
			require.Equal(t, tt.want, res)
		})
	}
}

func newEntrySuffixTestMiddleware(suffix string) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
			resp, err := next.Do(ctx, req)
			if err != nil {
				return resp, err
			}

			// Modify the response to append a suffix.
			if lokiResp, ok := resp.(*LokiResponse); ok {
				for i := range lokiResp.Data.Result {
					for j := range lokiResp.Data.Result[i].Entries {
						lokiResp.Data.Result[i].Entries[j].Line += suffix
					}
				}
			}
			return resp, nil
		})
	})
}
