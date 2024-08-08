package gcplog

import (
	"fmt"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/relabel"
	"google.golang.org/api/option"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"
)

// Target is a common interface implemented by both GCPLog targets.
type Target interface {
	target.Target
	Stop() error
}

// NewGCPLogTarget creates a GCPLog target either with the push or pull implementation, depending on the configured
// subscription type.
func NewGCPLogTarget(
	metrics *Metrics,
	logger log.Logger,
	handler api.EntryHandler,
	relabel []*relabel.Config,
	jobName string,
	config *scrapeconfig.GcplogTargetConfig,
	clientOptions ...option.ClientOption,
) (Target, error) {
	switch config.SubscriptionType {
	case "pull", "":
		return newPullTarget(metrics, logger, handler, relabel, jobName, config, clientOptions...)
	case "push":
		return newPushTarget(metrics, logger, handler, jobName, config, relabel)
	default:
		return nil, fmt.Errorf("invalid subscription type: %s. valid options are 'push' and 'pull'", config.SubscriptionType)
	}
}
