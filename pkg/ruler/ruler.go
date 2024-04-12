package ruler

import (
	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/config"

	ruler "github.com/grafana/loki/v3/pkg/ruler/base"
	"github.com/grafana/loki/v3/pkg/ruler/rulestore"
)

func NewRuler(cfg Config, evaluator Evaluator, reg prometheus.Registerer, logger log.Logger, ruleStore rulestore.RuleStore, limits RulesLimits, metricsNamespace string) (*ruler.Ruler, error) {
	// For backward compatibility, client and clients are defined in the remote_write config.
	// When both are present, an error is thrown.
	if len(cfg.RemoteWrite.Clients) > 0 && cfg.RemoteWrite.Client != nil {
		return nil, errors.New("ruler remote write config: both 'client' and 'clients' options are defined; 'client' is deprecated, please only use 'clients'")
	}

	if len(cfg.RemoteWrite.Clients) == 0 && cfg.RemoteWrite.Client != nil {
		if cfg.RemoteWrite.Clients == nil {
			cfg.RemoteWrite.Clients = make(map[string]config.RemoteWriteConfig)
		}

		cfg.RemoteWrite.Clients["default"] = *cfg.RemoteWrite.Client
	}

	mgr, err := ruler.NewDefaultMultiTenantManager(
		cfg.Config,
		MultiTenantRuleManager(cfg, evaluator, limits, logger, reg),
		reg,
		logger,
		limits,
		metricsNamespace,
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
		metricsNamespace,
	)
}
