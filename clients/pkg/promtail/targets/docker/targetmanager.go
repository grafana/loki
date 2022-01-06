package docker

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"

	"github.com/grafana/loki/pkg/util"
)

const (
	// See github.com/prometheus/prometheus/discovery/moby
	dockerLabel                = model.MetaLabelPrefix + "docker_"
	dockerLabelContainerPrefix = dockerLabel + "container_"
	dockerLabelContainerID     = dockerLabelContainerPrefix + "id"
)

type TargetManager struct {
	metrics    *Metrics
	logger     log.Logger
	positions  positions.Positions
	cancel     context.CancelFunc
	manager    *discovery.Manager
	pushClient api.EntryHandler
	syncers    map[string]*syncer
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
		metrics:    metrics,
		logger:     logger,
		cancel:     cancel,
		positions:  positions,
		manager:    discovery.NewManager(ctx, log.With(logger, "component", "docker_discovery")),
		pushClient: pushClient,
		syncers:    make(map[string]*syncer),
	}
	configs := map[string]discovery.Configs{}
	for _, cfg := range scrapeConfigs {
		if cfg.DockerConfig != nil {

			pipeline, err := stages.NewPipeline(log.With(logger, "component", "docker_pipeline"), cfg.PipelineStages, &cfg.JobName, metrics.reg)
			if err != nil {
				return nil, err
			}

			s := &syncer{
				metrics:       metrics,
				logger:        logger,
				targets:       make(map[string]*Target),
				entryHandler:  pipeline.Wrap(pushClient),
				defaultLabels: cfg.DockerConfig.Labels,
				host:          cfg.DockerConfig.Host,
				port:          cfg.DockerConfig.Port,
			}
			s.addTarget(cfg.DockerConfig.ContainerName, model.LabelSet{})
			tm.syncers[cfg.JobName] = s
		} else if cfg.DockerSDConfigs != nil {
			pipeline, err := stages.NewPipeline(
				log.With(logger, "component", "docker_pipeline"),
				cfg.PipelineStages,
				&cfg.JobName,
				metrics.reg,
			)
			if err != nil {
				return nil, err
			}

			for _, sd_config := range cfg.DockerSDConfigs {
				syncerKey := fmt.Sprintf("%s/%s:%d", cfg.JobName, sd_config.Host, sd_config.Port)
				_, ok := tm.syncers[syncerKey]
				if !ok {
					tm.syncers[syncerKey] = &syncer{
						metrics:       metrics,
						logger:        logger,
						positions:     positions,
						targets:       make(map[string]*Target),
						entryHandler:  pipeline.Wrap(pushClient),
						defaultLabels: model.LabelSet{},
						host:          sd_config.Host,
						port:          sd_config.Port,
					}
				}
				configs[syncerKey] = append(configs[syncerKey], sd_config)
			}
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
	for targetGroups := range tm.manager.SyncCh() {
		for jobName, groups := range targetGroups {
			syncer, ok := tm.syncers[jobName]
			if !ok {
				level.Debug(tm.logger).Log("msg", "unknown target for job", "job", jobName)
				continue
			}
			syncer.sync(groups)
		}
	}
}

// Ready returns true if at least one Docker target is active.
func (tm *TargetManager) Ready() bool {
	for _, s := range tm.syncers {
		if s.Ready() {
			return true
		}
	}
	return false
}

func (tm *TargetManager) Stop() {
	tm.cancel()
	for _, s := range tm.syncers {
		s.Stop()
	}
}

func (tm *TargetManager) ActiveTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.syncers))
	for k, s := range tm.syncers {
		result[k] = s.ActiveTargets()
	}
	return result
}

func (tm *TargetManager) AllTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.syncers))
	for k, s := range tm.syncers {
		result[k] = s.AllTargets()
	}
	return result
}
