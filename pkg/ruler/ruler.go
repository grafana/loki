package ruler

import (
	"github.com/cortexproject/cortex/pkg/ruler"
	"github.com/cortexproject/cortex/pkg/ruler/rulestore"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/logql"
)

func NewRuler(cfg Config, engine *logql.Engine, reg prometheus.Registerer, logger log.Logger, ruleStore rulestore.RuleStore, limits RulesLimits) (*ruler.Ruler, error) {
	mgr, err := ruler.NewDefaultMultiTenantManager(
		cfg.Config,
		MultiTenantRuleManager(cfg, engine, limits, logger, reg),
		reg,
		logger,
	)
	if err != nil {
		return nil, err
	}
	return ruler.NewRuler(
		cfg.Config,
		MultiTenantManagerAdapter(mgr),
		reg,
		logger,
		ruleStore,
		limits,
	)
}
