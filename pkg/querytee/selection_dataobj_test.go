package querytee

import (
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/validation"
)

func TestDataObjStrategy_Select(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	lokiLimits := newTestLokiLimits(t, now.Add(-48*time.Hour))
	dataObjCfg := DataObjConfig{Enabled: true, StorageLag: 3 * time.Hour}

	strategy, err := newDataObjStrategy(dataObjCfg, lokiLimits)
	require.NoError(t, err)
	strategy.now = func() time.Time { return now }

	const eligibleQuery = `sum(count_over_time({app="a"}[5m]))`

	for name, tc := range map[string]struct {
		tenants    []string
		query      string
		start, end time.Time
		expected   Selection
	}{
		"selects a stream-first sum that data objects hold": {
			tenants:  []string{"enabled"},
			query:    eligibleQuery,
			start:    now.Add(-24 * time.Hour),
			end:      now.Add(-12 * time.Hour),
			expected: Selection{Selected: true},
		},
		"does not select a metric query that is not a stream-first sum": {
			tenants:  []string{"enabled"},
			query:    `rate({app="a"}[5m])`,
			start:    now.Add(-24 * time.Hour),
			end:      now.Add(-12 * time.Hour),
			expected: Selection{Selected: false, Reason: "unsupported_query"},
		},
		"does not select a metric query that reads no logs and reports it as an unsupported query": {
			tenants:  []string{"enabled"},
			query:    `vector(1)`,
			start:    now.Add(-24 * time.Hour),
			end:      now.Add(-12 * time.Hour),
			expected: Selection{Selected: false, Reason: "unsupported_query"},
		},
		"does not select a log query and reports it as an unsupported request": {
			tenants:  []string{"enabled"},
			query:    `{app="a"} |= "error"`,
			start:    now.Add(-24 * time.Hour),
			end:      now.Add(-12 * time.Hour),
			expected: Selection{Selected: false, Reason: "unsupported_request"},
		},
		"does not select a query that does not parse": {
			tenants:  []string{"enabled"},
			query:    `{`,
			start:    now.Add(-24 * time.Hour),
			end:      now.Add(-12 * time.Hour),
			expected: Selection{Selected: false, Reason: "unsupported_query"},
		},
		"does not select a tenant that has stream-first off": {
			tenants:  []string{"other"},
			query:    eligibleQuery,
			start:    now.Add(-24 * time.Hour),
			end:      now.Add(-12 * time.Hour),
			expected: Selection{Selected: false, Reason: "stream_first_disabled"},
		},
		"does not select a time range inside the storage lag": {
			tenants:  []string{"enabled"},
			query:    eligibleQuery,
			start:    now.Add(-2 * time.Hour),
			end:      now.Add(-1 * time.Hour),
			expected: Selection{Selected: false, Reason: "outside_data_object_range"},
		},
		"selects a query that data objects hold only a small part of": {
			tenants:  []string{"enabled"},
			query:    eligibleQuery,
			start:    now.Add(-4 * time.Hour),
			end:      now,
			expected: Selection{Selected: true},
		},
	} {
		t.Run(name, func(t *testing.T) {
			req := &queryrange.LokiRequest{Query: tc.query, StartTs: tc.start, EndTs: tc.end}

			require.Equal(t, tc.expected, strategy.Select(tc.tenants, req))
		})
	}

	t.Run("selects an instant query that data objects hold", func(t *testing.T) {
		req := &queryrange.LokiInstantRequest{Query: eligibleQuery, TimeTs: now.Add(-12 * time.Hour)}

		require.Equal(t, Selection{Selected: true}, strategy.Select([]string{"enabled"}, req))
	})

	t.Run("does not select an instant log query and reports it as an unsupported request", func(t *testing.T) {
		req := &queryrange.LokiInstantRequest{Query: `{app="a"}`, TimeTs: now.Add(-12 * time.Hour)}

		require.Equal(t, Selection{Selected: false, Reason: "unsupported_request"}, strategy.Select([]string{"enabled"}, req))
	})

	t.Run("selects a stream-first sum that carries a plan", func(t *testing.T) {
		expr, err := syntax.ParseExpr(eligibleQuery)
		require.NoError(t, err)
		req := &queryrange.LokiRequest{
			Query:   eligibleQuery,
			Plan:    &plan.QueryPlan{AST: expr},
			StartTs: now.Add(-24 * time.Hour),
			EndTs:   now.Add(-12 * time.Hour),
		}

		require.Equal(t, Selection{Selected: true}, strategy.Select([]string{"enabled"}, req))
	})

	t.Run("does not select a log query that carries a plan and reports it as an unsupported request", func(t *testing.T) {
		const logQuery = `{app="a"} |= "error"`
		expr, err := syntax.ParseExpr(logQuery)
		require.NoError(t, err)
		req := &queryrange.LokiRequest{
			Query:   logQuery,
			Plan:    &plan.QueryPlan{AST: expr},
			StartTs: now.Add(-24 * time.Hour),
			EndTs:   now.Add(-12 * time.Hour),
		}

		require.Equal(t, Selection{Selected: false, Reason: "unsupported_request"}, strategy.Select([]string{"enabled"}, req))
	})

	t.Run("does not select a query when data objects are disabled", func(t *testing.T) {
		disabledCfg := DataObjConfig{Enabled: false, StorageLag: dataObjCfg.StorageLag}
		disabledStrategy, err := newDataObjStrategy(disabledCfg, lokiLimits)
		require.NoError(t, err)
		req := &queryrange.LokiRequest{Query: eligibleQuery, StartTs: now.Add(-24 * time.Hour), EndTs: now.Add(-12 * time.Hour)}

		selection := disabledStrategy.Select([]string{"enabled"}, req)

		require.Equal(t, Selection{Selected: false, Reason: "data_objects_disabled"}, selection)
	})

	t.Run("does not select a request that is not a range or instant query", func(t *testing.T) {
		selection := strategy.Select([]string{"enabled"}, &queryrange.LabelRequest{})

		require.Equal(t, Selection{Selected: false, Reason: reasonUnsupportedRequest}, selection)
	})
}

// fixedTenantLimits implements validation.TenantLimits with a fixed map.
type fixedTenantLimits map[string]*validation.Limits

func (f fixedTenantLimits) TenantLimits(userID string) *validation.Limits { return f[userID] }

func (f fixedTenantLimits) AllByUserID() map[string]*validation.Limits { return f }

// newTestLokiLimits returns a config where the tenant "enabled" reads data objects from dataObjStart on,
// and every other tenant has stream-first off.
func newTestLokiLimits(t *testing.T, dataObjStart time.Time) *LokiLimits {
	t.Helper()

	overrides, err := validation.NewOverrides(validation.Limits{}, fixedTenantLimits{
		"enabled": {
			StreamFirstExecutionEnabled: true,
			DataObjQueryStartTime:       flagext.Time(dataObjStart),
		},
	})
	require.NoError(t, err)
	return &LokiLimits{Overrides: overrides}
}
