package gcplog

import (
	"fmt"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/targets/target"
)

// nolint: golint
type GcplogTargetManager struct {
	logger  log.Logger
	targets map[string]*GcplogTarget
}

func NewGcplogTargetManager(
	metrics *Metrics,
	logger log.Logger,
	client api.EntryHandler,
	scrape []scrapeconfig.Config,
) (*GcplogTargetManager, error) {
	tm := &GcplogTargetManager{
		logger:  logger,
		targets: make(map[string]*GcplogTarget),
	}

	for _, cf := range scrape {
		if cf.GcplogConfig == nil {
			continue
		}
		pipeline, err := stages.NewPipeline(log.With(logger, "component", "pubsub_pipeline"), cf.PipelineStages, &cf.JobName, metrics.reg)
		if err != nil {
			return nil, err
		}

		t, err := NewGcplogTarget(metrics, logger, pipeline.Wrap(client), cf.RelabelConfigs, cf.JobName, cf.GcplogConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create pubsub target: %w", err)
		}
		tm.targets[cf.JobName] = t
	}

	return tm, nil
}

func (tm *GcplogTargetManager) Ready() bool {
	for _, t := range tm.targets {
		if t.Ready() {
			return true
		}
	}
	return false
}

func (tm *GcplogTargetManager) Stop() {
	for name, t := range tm.targets {
		if err := t.Stop(); err != nil {
			level.Error(t.logger).Log("event", "failed to stop pubsub target", "name", name, "cause", err)
		}
	}
}

func (tm *GcplogTargetManager) ActiveTargets() map[string][]target.Target {
	// TODO(kavi): if someway to check if specific topic is active and store the state on the target struct?
	return tm.AllTargets()
}

func (tm *GcplogTargetManager) AllTargets() map[string][]target.Target {
	res := make(map[string][]target.Target, len(tm.targets))
	for k, v := range tm.targets {
		res[k] = []target.Target{v}
	}
	return res
}
