package docker

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"

	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/pkg/util"
)

type TargetManager struct {
	logger  log.Logger
	cancel  context.CancelFunc
	targets map[string]*Target
	manager *discovery.Manager
}

func NewTargetManager(
	metrics *Metrics,
	logger log.Logger,
	positions positions.Positions,
	pushClient api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config,
) (*TargetManager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	tm := &TargetManager{
		logger:  logger,
		cancel:  cancel,
		targets: make(map[string]*Target),
		manager: discovery.NewManager(ctx, log.With(logger, "component", "docker_discovery")),
	}
	configs := map[string]discovery.Configs{}
	for _, cfg := range scrapeConfigs {
		if cfg.DockerConfig != nil {
			pipeline, err := stages.NewPipeline(log.With(logger, "component", "docker_pipeline"), cfg.PipelineStages, &cfg.JobName, metrics.reg)
			if err != nil {
				return nil, err
			}
			t, err := NewTarget(metrics, log.With(logger, "target", "docker"), pipeline.Wrap(pushClient), positions, cfg.DockerConfig)
			if err != nil {
				return nil, err
			}
			tm.targets[cfg.JobName] = t
		} else if cfg.DockerSDConfigs != nil {
			sd_configs := make(discovery.Configs, 0)
			for _, sd_config := range cfg.DockerSDConfigs {
				sd_configs = append(sd_configs, sd_config)
			}
			configs[cfg.JobName] = sd_configs
			level.Debug(tm.logger).Log("msg", "add Docker service discovery", "job", cfg.JobName, "configs", len(sd_configs))
		} else {
			level.Debug(tm.logger).Log("msg", "Docker service discovery configs are emtpy")
		}
	}

	go tm.run()
	go util.LogError("running target manager", tm.manager.Run)

	return tm, tm.manager.ApplyConfig(configs)
}

// run listens on the service discovery and adds new targets.
func (tm *TargetManager) run() {
	level.Debug(tm.logger).Log("msg", "start processing target group updates")
	for targetGroups := range tm.manager.SyncCh() {
		level.Debug(tm.logger).Log("msg", "process target group", "groups", len(targetGroups))
		for _, groups := range targetGroups {
			tm.sync(groups)
		}
	}
}

func (tm *TargetManager) sync(groups []*targetgroup.Group) {
	level.Debug(tm.logger).Log("msg", "synchronize groups")
	for _, group := range groups {
		if group.Source != "Docker" {
			continue
		}

		for _, t := range group.Targets {
			level.Debug(tm.logger).Log("msg", "new target", "labels", t)
		}
	}
}

// Ready returns true if at least one cloudflare target is active.
func (tm *TargetManager) Ready() bool {
	for _, t := range tm.targets {
		if t.Ready() {
			return true
		}
	}
	return false
}

func (tm *TargetManager) Stop() {
	tm.cancel()
	for _, t := range tm.targets {
		t.Stop()
	}
}

func (tm *TargetManager) ActiveTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.targets))
	for k, v := range tm.targets {
		if v.Ready() {
			result[k] = []target.Target{v}
		}
	}
	return result
}

func (tm *TargetManager) AllTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.targets))
	for k, v := range tm.targets {
		result[k] = []target.Target{v}
	}
	return result
}
