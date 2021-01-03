package ruler

import (
	"time"

	"github.com/cortexproject/cortex/pkg/ruler"
	cRules "github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/ruler/manager"
)

type Config struct {
	ruler.Config `yaml:",inline"`
}

// Override the embedded cortex variant which expects a cortex limits struct. Instead copy the relevant bits over.
func (cfg *Config) Validate() error {
	if err := cfg.StoreConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid storage config")
	}
	return nil
}

// Loki does not yet support shuffle sharding or per tenant evaluation delays, so implement what cortex expects.
type passthroughLimits struct{ Config }

func (cfg passthroughLimits) RulerMaxRuleGroupsPerTenant(_ string) int { return 0 }

func (cfg passthroughLimits) RulerMaxRulesPerRuleGroup(_ string) int { return 0 }

func (cfg passthroughLimits) EvaluationDelay(_ string) time.Duration {
	return cfg.Config.EvaluationDelay
}
func (passthroughLimits) RulerTenantShardSize(_ string) int { return 0 }

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
		passthroughLimits{cfg},
	)

}
