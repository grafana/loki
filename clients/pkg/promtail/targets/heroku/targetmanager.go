package heroku

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/clients/pkg/logentry/stages"
	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"
)

type TargetManager struct {
	logger  log.Logger
	targets map[string]*Target
}

func NewHerokuDrainTargetManager(
	metrics *Metrics,
	reg prometheus.Registerer,
	logger log.Logger,
	client api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config) (*TargetManager, error) {

	tm := &TargetManager{
		logger:  logger,
		targets: make(map[string]*Target),
	}

	for _, cfg := range scrapeConfigs {
		pipeline, err := stages.NewPipeline(log.With(logger, "component", "heroku_drain_pipeline_"+cfg.JobName), cfg.PipelineStages, &cfg.JobName, reg)
		if err != nil {
			return nil, err
		}

		t, err := NewTarget(metrics, logger, pipeline.Wrap(client), cfg.JobName, cfg.HerokuDrainConfig, cfg.RelabelConfigs)
		if err != nil {
			return nil, err
		}

		tm.targets[cfg.JobName] = t
	}

	return tm, nil
}

func (hm *TargetManager) Ready() bool {
	for _, t := range hm.targets {
		if t.Ready() {
			return true
		}
	}
	return false
}

func (hm *TargetManager) Stop() {
	for name, t := range hm.targets {
		if err := t.Stop(); err != nil {
			level.Error(t.logger).Log("event", "failed to stop heroku drain target", "name", name, "cause", err)
		}
	}
}

func (hm *TargetManager) ActiveTargets() map[string][]target.Target {
	return hm.AllTargets()
}

func (hm *TargetManager) AllTargets() map[string][]target.Target {
	res := make(map[string][]target.Target, len(hm.targets))
	for k, v := range hm.targets {
		res[k] = []target.Target{v}
	}
	return res
}
