package ruler

import (
	"github.com/cortexproject/cortex/pkg/ruler"
	cRules "github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/ruler/manager"
	"github.com/prometheus/client_golang/prometheus"
)

type Config struct {
	ruler.Config `yaml:",inline"`
}

func NewRuler(cfg Config, engine *logql.Engine, reg prometheus.Registerer, logger log.Logger, ruleStore cRules.RuleStore) (*ruler.Ruler, error) {

	mgr, err := ruler.NewDefaultMultiTenantManager(
		cfg.Config,
		manager.MemstoreTenantManager(
			cfg.Config,
			engine,
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
	)

}
