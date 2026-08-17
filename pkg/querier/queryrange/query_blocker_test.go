package queryrange

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

func TestRoundTripper_BlockedQueries(t *testing.T) {
	metricQuery := `sum(rate({app="foo"}[1m]))`
	filterQuery := `{app="foo"} |= "bar"`

	tests := []struct {
		name       string
		req        queryrangebase.Request
		blocked    []*validation.BlockedQuery
		tagsHeader string
		wantErr    bool
	}{
		{
			name: "blocks exact match on range query",
			req:  lokiRangeRequest(metricQuery),
			blocked: []*validation.BlockedQuery{
				{Pattern: metricQuery},
			},
			wantErr: true,
		},
		{
			name: "blocks exact match on instant query",
			req:  lokiInstantRequest(metricQuery),
			blocked: []*validation.BlockedQuery{
				{Pattern: metricQuery},
			},
			wantErr: true,
		},
		{
			name: "blocks by hash of original query",
			req:  lokiRangeRequest(metricQuery),
			blocked: []*validation.BlockedQuery{
				{Hash: util.HashedQuery(metricQuery)},
			},
			wantErr: true,
		},
		{
			name: "blocks regex match",
			req:  lokiRangeRequest(metricQuery),
			blocked: []*validation.BlockedQuery{
				{Pattern: ".*foo.*", Regex: true},
			},
			wantErr: true,
		},
		{
			name: "does not block when pattern matches but type does not",
			req:  lokiRangeRequest(filterQuery),
			blocked: []*validation.BlockedQuery{
				{Pattern: filterQuery, Types: []string{logql.QueryTypeMetric}},
			},
			wantErr: false,
		},
		{
			name: "blocks when type matches",
			req:  lokiRangeRequest(filterQuery),
			blocked: []*validation.BlockedQuery{
				{Pattern: filterQuery, Types: []string{logql.QueryTypeFilter}},
			},
			wantErr: true,
		},
		{
			name: "blocks when query tags match",
			req:  lokiRangeRequest(metricQuery),
			blocked: []*validation.BlockedQuery{
				{Tags: map[string]string{"source": "grafana"}},
			},
			tagsHeader: "Source=grafana,Feature=beta",
			wantErr:    true,
		},
		{
			name: "does not block when query tags mismatch",
			req:  lokiRangeRequest(metricQuery),
			blocked: []*validation.BlockedQuery{
				{Tags: map[string]string{"source": "grafana"}},
			},
			tagsHeader: "Source=other",
			wantErr:    false,
		},
		{
			name:    "passes through when no policies are configured",
			req:     lokiRangeRequest(metricQuery),
			blocked: nil,
			wantErr: false,
		},
		{
			name: "skips metadata requests",
			req: &LokiSeriesRequest{
				Match:   []string{`{app="foo"}`},
				StartTs: time.Unix(0, 0),
				EndTs:   time.Unix(100, 0),
				Path:    "/loki/api/v1/series",
			},
			blocked: []*validation.BlockedQuery{
				{Pattern: ".*", Regex: true},
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			next := queryrangebase.HandlerFunc(func(context.Context, queryrangebase.Request) (queryrangebase.Response, error) {
				called = true
				return &LokiResponse{Status: "success"}, nil
			})

			rt := newRoundTripper(
				log.NewNopLogger(),
				next, next, next, next, next, next, next, next, next, next, next, next,
				fakeLimits{blockedQueries: tc.blocked},
			)
			ctx := user.InjectOrgID(context.Background(), "fake")
			if tc.tagsHeader != "" {
				ctx = httpreq.InjectQueryTags(ctx, tc.tagsHeader)
			}

			_, err := rt.Do(ctx, tc.req)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), logqlmodel.ErrBlocked.Error())
				resp, ok := httpgrpc.HTTPResponseFromError(err)
				require.True(t, ok)
				require.Equal(t, int32(400), resp.Code)
				require.False(t, called)
				return
			}

			require.NoError(t, err)
			require.True(t, called)
		})
	}
}

func lokiRangeRequest(query string) *LokiRequest {
	return &LokiRequest{
		Query:     query,
		StartTs:   time.Unix(0, 0),
		EndTs:     time.Unix(100, 0),
		Step:      1000,
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query_range",
		Plan:      &plan.QueryPlan{AST: syntax.MustParseExpr(query)},
	}
}

func lokiInstantRequest(query string) *LokiInstantRequest {
	return &LokiInstantRequest{
		Query:     query,
		TimeTs:    time.Unix(100, 0),
		Direction: logproto.FORWARD,
		Path:      "/loki/api/v1/query",
		Plan:      &plan.QueryPlan{AST: syntax.MustParseExpr(query)},
	}
}
