package kubernetes

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"

	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/pkg/util"
)

const (
	// See vendor/github.com/prometheus/prometheus/discovery/kubernetes/pod.go:183
	kubernetesLabel                = model.MetaLabelPrefix + "kubernetes_"
	kubernetesLabelPodPrefix       = kubernetesLabel + "pod_"
	kubernetesLabelPodName         = kubernetesLabelPodPrefix + "name"
	kubernetesLabelContainerPrefix = kubernetesLabelPodPrefix + "container_"
	kubernetesLabelContainerName   = kubernetesLabelContainerPrefix + "name"
	kubernetesLabelNamespace       = kubernetesLabel + "namespace"
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
) (*TargetManager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	tm := &TargetManager{
		metrics:    metrics,
		logger:     logger,
		cancel:     cancel,
		done:       make(chan struct{}),
		positions:  positions,
		manager:    discovery.NewManager(ctx, log.With(logger, "component", "kubernetes_discovery")),
		pushClient: pushClient,
		groups:     make(map[string]*targetGroup),
	}
	configs := map[string]discovery.Configs{}
	for _, cfg := range scrapeConfigs {
		if cfg.KubernetesSDConfigs != nil {
			pipeline, err := stages.NewPipeline(
				log.With(logger, "component", "kubernetes_pipeline"),
				cfg.PipelineStages,
				&cfg.JobName,
				metrics.reg,
			)
			if err != nil {
				return nil, err
			}

			for _, sdConfig := range cfg.KubernetesSDConfigs {

				syncerKey := fmt.Sprintf("%s/%s", cfg.JobName, sdConfig.Name())
				_, ok := tm.groups[syncerKey]
				if !ok {
					client, err := newKubernetesClient(logger, sdConfig)
					if err != nil {
						return nil, errors.Wrapf(err, "failed to create kubernetes client for SDConfig %s", sdConfig.Name())
					}
					tm.groups[syncerKey] = &targetGroup{
						metrics:       metrics,
						logger:        logger,
						positions:     positions,
						targets:       make(map[string]*Target),
						entryHandler:  pipeline.Wrap(pushClient),
						defaultLabels: model.LabelSet{},
						relabelConfig: cfg.RelabelConfigs,
						client:        client,
					}
				}
				configs[syncerKey] = append(configs[syncerKey], sdConfig)
			}
		} else {
			level.Debug(tm.logger).Log("msg", "Kubernetes service discovery configs are empty")
		}
	}

	go tm.run(ctx)
	go util.LogError("running target manager", tm.manager.Run)

	return tm, tm.manager.ApplyConfig(configs)
}

// run listens on the service discovery and adds new targets.
func (tm *TargetManager) run(ctx context.Context) {
	defer close(tm.done)
	level.Info(tm.logger).Log("msg", "waiting for sync")
	for {
		select {
		case targetGroups := <-tm.manager.SyncCh():
			level.Info(tm.logger).Log("msg", "Got target groups")
			for jobName, groups := range targetGroups {
				tg, ok := tm.groups[jobName]
				if !ok {
					level.Warn(tm.logger).Log("msg", "unknown target for job", "job", jobName)
					continue
				}
				tg.sync(groups)
			}
		case <-ctx.Done():
			level.Info(tm.logger).Log("msg", "Sync is completely done")
			return
		}
	}
}

// Ready returns true if at least one Kubernetes target is active.
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
