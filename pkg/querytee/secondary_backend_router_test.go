package querytee

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	prom_testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func TestSecondaryBackendRouter_shouldRoute(t *testing.T) {
	request := &queryrange.LokiRequest{Query: `sum(count_over_time({app="a"}[5m]))`}
	tenantCtx := user.InjectOrgID(t.Context(), "tenant")

	for name, tc := range map[string]struct {
		ctx              context.Context
		strategy         *fakeStrategy
		expectedSent     bool
		expectedDecision string
	}{
		"keeps a request off the secondary backend when the strategy panics": {
			ctx:              tenantCtx,
			strategy:         &fakeStrategy{panics: true},
			expectedDecision: decisionStrategyError,
		},
		"sends a selected request to the secondary backend": {
			ctx:              tenantCtx,
			strategy:         &fakeStrategy{selection: Selection{Selected: true}},
			expectedSent:     true,
			expectedDecision: decisionSent,
		},
		"keeps a request that the strategy rejects off the secondary backend with the strategy's reason": {
			ctx:              tenantCtx,
			strategy:         &fakeStrategy{selection: Selection{Selected: false, Reason: "outside_data_object_range"}},
			expectedDecision: "outside_data_object_range",
		},
		"keeps a request without a valid tenant off the secondary backend even when the strategy selects it": {
			ctx:              t.Context(),
			strategy:         &fakeStrategy{selection: Selection{Selected: true}},
			expectedDecision: decisionInvalidTenant,
		},
	} {
		t.Run(name, func(t *testing.T) {
			metrics := NewProxyMetrics(prometheus.NewRegistry())
			router := newSecondaryBackendRouter(tc.strategy, metrics, "test_route", log.NewNopLogger())

			require.Equal(t, tc.expectedSent, router.shouldRoute(tc.ctx, request))
			require.Equal(t, 1.0, prom_testutil.ToFloat64(metrics.secondaryBackendDecisions.WithLabelValues("test_route", tc.expectedDecision)))
		})
	}

	t.Run("sends every request to the secondary backend and counts no decision when there is no strategy", func(t *testing.T) {
		metrics := NewProxyMetrics(prometheus.NewRegistry())
		router := newSecondaryBackendRouter(nil, metrics, "test_route", log.NewNopLogger())

		require.True(t, router.shouldRoute(tenantCtx, request))
		require.Zero(t, prom_testutil.CollectAndCount(metrics.secondaryBackendDecisions))
	})
}

// fakeStrategy returns a fixed selection. It panics when panics is set.
type fakeStrategy struct {
	selection Selection
	panics    bool
}

func (s *fakeStrategy) Select(_ []string, _ queryrangebase.Request) Selection {
	if s.panics {
		panic("select failed")
	}
	return s.selection
}
