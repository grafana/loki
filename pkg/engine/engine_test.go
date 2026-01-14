package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
)

func TestEngine_BuildLogicalPlan_WithQueryMutator(t *testing.T) {
	now := time.Now()
	startTime := now.Add(-1 * time.Hour)

	tests := []struct {
		name           string
		originalQuery  string
		addedMatchers  []*labels.Matcher // matchers to add via mutator, nil means no mutator
		expectedInPlan []string
	}{
		{
			name:          "mutator adds namespace label matcher to pipeline query",
			originalQuery: `{cluster="prod"} | logfmt | foo="bar"`,
			addedMatchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "namespace", "loki"),
			},
			expectedInPlan: []string{
				`%1 = EQ label.cluster "prod"`,
				`%2 = EQ label.namespace "loki"`,
				`%3 = AND %1 %2`,
				`%4 = MAKETABLE [selector=%3, predicates=[], shard=0_of_1]`,
			},
		},
		{
			name:          "no mutator preserves original query",
			originalQuery: `{cluster="prod"} | logfmt | foo="bar"`,
			addedMatchers: nil, // no mutator
			expectedInPlan: []string{
				`%1 = EQ label.cluster "prod"`,
				`%2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]`,
			},
		},
		// TODO: Add test case for queries with multiple stream selectors (e.g., binary operations)
		// once the V2 engine supports them. Currently, binary operations with two non-literal
		// expressions are not implemented (see planner.go:walkBinOp).
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the original expression
			originalExpr, err := syntax.ParseExpr(tt.originalQuery)
			require.NoError(t, err)

			// Set up the engine with or without a mutator
			var mutator QueryMutator
			if tt.addedMatchers != nil {
				addedMatchers := tt.addedMatchers // capture for closure

				mutator = func(_ context.Context, params logql.Params) logql.Params {

					// Clone the expression to avoid modifying the original
					expr := params.GetExpression()
					clonedExpr, err := syntax.Clone(expr)
					require.NoError(t, err)

					// Use Walk to find the MatchersExpr
					clonedExpr.Walk(func(e syntax.Expr) bool {
						if matchersExpr, ok := e.(*syntax.MatchersExpr); ok {
							// Add the new matchers and stop walking
							matchersExpr.AppendMatchers(addedMatchers)
							return false
						}
						return true
					})

					// Build the mutated query string from the modified expression
					mutatedQuery := clonedExpr.String()

					// Create a new request with the mutated query and expression
					mutatedReq := &queryrange.LokiRequest{
						Query:     mutatedQuery,
						Limit:     params.Limit(),
						StartTs:   params.Start(),
						EndTs:     params.End(),
						Direction: params.Direction(),
						Shards:    params.Shards(),
						Plan: &plan.QueryPlan{
							AST: clonedExpr,
						},
					}

					mutatedParams, err := queryrange.ParamsFromRequest(mutatedReq)
					require.NoError(t, err)

					return mutatedParams
				}
			}

			e := &Engine{
				logger:       log.NewNopLogger(),
				metrics:      newMetrics(prometheus.NewRegistry()),
				queryMutator: mutator,
			}

			// Create the request with the original query
			req := &queryrange.LokiRequest{
				Query:     tt.originalQuery,
				Limit:     1000,
				StartTs:   startTime,
				EndTs:     now,
				Direction: logproto.BACKWARD,
				Shards:    []string{"0_of_1"},
				Plan: &plan.QueryPlan{
					AST: originalExpr,
				},
			}

			params, err := queryrange.ParamsFromRequest(req)
			require.NoError(t, err)

			// Build the logical plan
			ctx := t.Context()
			logicalPlan, _, err := e.buildLogicalPlan(ctx, log.NewNopLogger(), params)
			require.NoError(t, err)

			planStr := logicalPlan.String()
			// t.Logf("Logical plan:\n%s", planStr)

			// Verify expected strings are in the plan
			for _, expected := range tt.expectedInPlan {
				require.True(t, strings.Contains(planStr, expected),
					"Plan should contain %q", expected)
			}
		})
	}
}
