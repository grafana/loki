package docker

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"

	"github.com/grafana/loki/v3/clients/pkg/logentry/stages"
	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	// See github.com/prometheus/prometheus/discovery/moby
	dockerLabel                = model.MetaLabelPrefix + "docker_"
	dockerLabelContainerPrefix = dockerLabel + "container_"
	dockerLabelContainerID     = dockerLabelContainerPrefix + "id"
	dockerLabelLogStream       = dockerLabelContainerPrefix + "log_stream"
)

type TargetManager struct {
	metrics    *Metrics
	logger     log.Logger
	positions  positions.Positions
	cancel     context.CancelFunc
	done       chan struct{}
	manager    *discovery.Manager
	pushClient api.EntryHandler
	groups     map[string]*targetGroup
}

func NewTargetManager(
	metrics *Metrics,
	logger log.Logger,
	positions positions.Positions,
	pushClient api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config,
	maxLineSize int,
) (*TargetManager, error) {
	noopRegistry := util.NoopRegistry{}
	noopSdMetrics, err := discovery.CreateAndRegisterSDMetrics(noopRegistry)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	tm := &TargetManager{
		metrics:   metrics,
		logger:    logger,
		cancel:    cancel,
		done:      make(chan struct{}),
		positions: positions,
		manager: discovery.NewManager(
			ctx,
			util_log.SlogFromGoKit(log.With(logger, "component", "docker_discovery")),
			noopRegistry,
			noopSdMetrics,
		),
		pushClient: pushClient,
		groups:     make(map[string]*targetGroup),
	}
	configs := map[string]discovery.Configs{}
	for _, cfg := range scrapeConfigs {
		if cfg.DockerSDConfigs != nil {
			pipeline, err := stages.NewPipeline(
				log.With(logger, "component", "docker_pipeline"),
				cfg.PipelineStages,
				&cfg.JobName,
				metrics.reg,
			)
			if err != nil {
				return nil, err
			}

			for _, sdConfig := range cfg.DockerSDConfigs {
				syncerKey := fmt.Sprintf("%s/%s:%d", cfg.JobName, sdConfig.Host, sdConfig.Port)
				_, ok := tm.groups[syncerKey]
				if !ok {
					tm.groups[syncerKey] = &targetGroup{
						metrics:          metrics,
						logger:           logger,
						positions:        positions,
						targets:          make(map[string]*Target),
						entryHandler:     pipeline.Wrap(pushClient),
						defaultLabels:    model.LabelSet{},
						relabelConfig:    cfg.RelabelConfigs,
						host:             sdConfig.Host,
						httpClientConfig: sdConfig.HTTPClientConfig,
						refreshInterval:  sdConfig.RefreshInterval,
						maxLineSize:      maxLineSize,
					}
				}
				configs[syncerKey] = append(configs[syncerKey], sdConfig)
			}
		} else {
			level.Debug(tm.logger).Log("msg", "Docker service discovery configs are empty")
		}
	}

	go tm.run(ctx)
	go util.LogError("running target manager", tm.manager.Run)

	return tm, tm.manager.ApplyConfig(configs)
}

// run listens on the service discovery and adds new targets.
func (tm *TargetManager) run(ctx context.Context) {
	defer close(tm.done)
	for {
		select {
		case targetGroups := <-tm.manager.SyncCh():
			for jobName, groups := range targetGroups {
				tg, ok := tm.groups[jobName]
				if !ok {
					level.Debug(tm.logger).Log("msg", "unknown target for job", "job", jobName)
					continue
				}
				tg.sync(groups)
			}
		case <-ctx.Done():
			return
		}
	}
}

// Ready returns true if at least one Docker target is active.
func (tm *TargetManager) Ready() bool {
	for _, s := range tm.groups {
		if s.Ready() {
			return true
		}
	}
	return false
}

func (tm *TargetManager) Stop() {
	tm.cancel()
	<-tm.done
	for _, s := range tm.groups {
		s.Stop()
	}
}

func (tm *TargetManager) ActiveTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.groups))
	for k, s := range tm.groups {
		result[k] = s.ActiveTargets()
	}
	return result
}

func (tm *TargetManager) AllTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.groups))
	for k, s := range tm.groups {
		result[k] = s.AllTargets()
	}
	return result
}
