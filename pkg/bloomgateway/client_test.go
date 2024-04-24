package bloomgateway

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

func TestBloomGatewayClient(t *testing.T) {
	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

	limits := newLimits()

	cfg := ClientConfig{}
	flagext.DefaultValues(&cfg)

	t.Run("FilterChunks returns response", func(t *testing.T) {
		c, err := NewClient(cfg, limits, reg, logger, nil, false)
		require.NoError(t, err)
		expr, err := syntax.ParseExpr(`{foo="bar"}`)
		require.NoError(t, err)
		res, err := c.FilterChunks(context.Background(), "tenant", bloomshipper.NewInterval(0, 0), nil, plan.QueryPlan{AST: expr})
		require.NoError(t, err)
		require.Equal(t, 0, len(res))
	})
}
