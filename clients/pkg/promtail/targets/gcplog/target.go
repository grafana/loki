package gcplog

import (
	"fmt"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
)

type Target interface {
	target.Target
	Stop() error
}

func NewGCPLogTarget(
	metrics *Metrics,
	logger log.Logger,
	handler api.EntryHandler,
	relabel []*relabel.Config,
	jobName string,
	config *scrapeconfig.GCPLogTargetConfig,
) (Target, error) {
	if config.SubscriptionType == "pull" || config.SubscriptionType == "" {
		return newPullTarget(metrics, logger, handler, relabel, jobName, config)
	} else if config.SubscriptionType == "push" {
		return newPushTarget(metrics, logger, handler, jobName, config, relabel)
	} else {
		return nil, fmt.Errorf("invalid subscription type %s", config.SubscriptionType)
	}
}
