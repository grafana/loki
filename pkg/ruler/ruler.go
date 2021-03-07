package ruler

import (
	"github.com/cortexproject/cortex/pkg/ruler"
	cRules "github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/pkg/ruler/config"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/ruler/manager"
)

func NewRuler(cfg config.Config, engine *logql.Engine, reg prometheus.Registerer, logger log.Logger, ruleStore cRules.RuleStore, limits ruler.RulesLimits) (*ruler.Ruler, error) {
	mgr, err := ruler.NewDefaultMultiTenantManager(
		cfg.Config,
		manager.MemstoreTenantManager(
			cfg,
			engine,
			limits,
		),
		prometheus.DefaultRegisterer,
		logger,
	)
	if err != nil {
		return nil, err
	}
	return ruler.NewRuler(
		cfg.Config,
		manager.MultiTenantManagerAdapter(mgr),
		reg,
		logger,
		ruleStore,
		limits,
	)
}
