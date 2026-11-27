package querier

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/validation"
)

func TestQuerier_SelectLogsClonesPlanForIngesters(t *testing.T) {
	limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	require.NoError(t, err)

	conf := mockQuerierConfig()
	conf.QueryIngestersWithin = 30 * time.Minute
	ingesterClient, store, q, err := setupIngesterQuerierMocks(conf, limits)
	require.NoError(t, err)

	now := time.Now()
	request := &logproto.QueryRequest{
		Start:     now.Add(-2 * time.Hour),
		End:       now,
		Direction: logproto.FORWARD,
		Plan:      &plan.QueryPlan{AST: syntax.MustParseExpr(`{foo="bar"} |= "a" | json |= "b"`)},
	}

	ctx := user.InjectOrgID(context.Background(), "test")
	it, err := q.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: request})
	require.NoError(t, err)
	t.Cleanup(func() { _ = it.Close() })

	ingesterCalls := ingesterClient.GetMockedCallsByMethod("Query")
	require.Len(t, ingesterCalls, 1)
	ingesterRequest := ingesterCalls[0].Arguments.Get(1).(*logproto.QueryRequest)
	require.NotSame(t, request.Plan, ingesterRequest.Plan)

	before, err := ingesterRequest.Plan.Marshal()
	require.NoError(t, err)

	storeCalls := store.GetMockedCallsByMethod("SelectLogs")
	require.Len(t, storeCalls, 1)
	selector, err := storeCalls[0].Arguments.Get(1).(logql.SelectLogParams).LogSelector()
	require.NoError(t, err)
	_, err = selector.Pipeline()
	require.NoError(t, err)

	after, err := ingesterRequest.Plan.Marshal()
	require.NoError(t, err)
	require.Equal(t, before, after)
}
