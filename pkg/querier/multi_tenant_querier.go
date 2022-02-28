package querier

import (
	"github.com/go-kit/log"

	"github.com/grafana/loki/pkg/logql"
)

// MultiTenantQuerier is able to query across different tenants.
type MultiTenantQuerier struct {
	querier
}

// NewMultiTenantQuerier returns a new querier able to query across different tenants.
func NewMultiTenantQuerier(querier querier, logger log.Logger) (*MultiTenantQuerier, error) {
	mtq := &MultiTenantQuerier{querier: querier}

	mtq.querier.engine = logql.NewEngine(mtq.cfg.Engine, mtq, mtq.querier.limits, log.With(logger, "component", "querier"))

	return mtq, nil
}
