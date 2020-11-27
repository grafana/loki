package file

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/kubernetes"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/targets/target"
)

const (
	pathLabel              = "__path__"
	hostLabel              = "__host__"
	kubernetesPodNodeField = "spec.nodeName"
)

var (
	failedTargets = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "targets_failed_total",
		Help:      "Number of failed targets.",
	}, []string{"reason"})
	targetsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "targets_active_total",
		Help:      "Number of active total.",
	})
)

// FileTargetManager manages a set of targets.
// nolint:golint
type FileTargetManager struct {
	log     log.Logger
	quit    context.CancelFunc
	syncers map[string]*targetSyncer
	manager *discovery.Manager
}

// NewFileTargetManager creates a new TargetManager.
func NewFileTargetManager(
	logger log.Logger,
	positions positions.Positions,
	client api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config,
	targetConfig *Config,
) (*FileTargetManager, error) {
	ctx, quit := context.WithCancel(context.Background())
	tm := &FileTargetManager{
		log:     logger,
		quit:    quit,
		syncers: map[string]*targetSyncer{},
		manager: discovery.NewManager(ctx, log.With(logger, "component", "discovery")),
	}

	hostname, err := hostname()
	if err != nil {
		return nil, err
	}

	configs := map[string]discovery.Configs{}
	for _, cfg := range scrapeConfigs {
		if !cfg.HasServiceDiscoveryConfig() {
			continue
		}

		registerer := prometheus.DefaultRegisterer
		pipeline, err := stages.NewPipeline(log.With(logger, "component", "file_pipeline"), cfg.PipelineStages, &cfg.JobName, registerer)
		if err != nil {
			return nil, err
		}

		// Add Source value to the static config target groups for unique identification
		// within scrape pool. Also, default target label to localhost if target is not
		// defined in promtail config.
		// Just to make sure prometheus target group sync works fine.
		for i, tg := range cfg.ServiceDiscoveryConfig.StaticConfigs {
			tg.Source = fmt.Sprintf("%d", i)
			if len(tg.Targets) == 0 {
				tg.Targets = []model.LabelSet{
					{model.AddressLabel: "localhost"},
				}
			}
		}

		// Add an additional api-level node filtering, so we only fetch pod metadata for
		// all the pods from the current node. Without this filtering we will have to
		// download metadata for all pods running on a cluster, which may be a long operation.
		for _, kube := range cfg.ServiceDiscoveryConfig.KubernetesSDConfigs {
			if kube.Role == kubernetes.RolePod {
				selector := fmt.Sprintf("%s=%s", kubernetesPodNodeField, hostname)
				kube.Selectors = []kubernetes.SelectorConfig{
					{Role: kubernetes.RolePod, Field: selector},
				}
			}
		}

		s := &targetSyncer{
			log:            logger,
			positions:      positions,
			relabelConfig:  cfg.RelabelConfigs,
			targets:        map[string]*FileTarget{},
			droppedTargets: []target.Target{},
			hostname:       hostname,
			entryHandler:   pipeline.Wrap(client),
			targetConfig:   targetConfig,
		}
		tm.syncers[cfg.JobName] = s
		configs[cfg.JobName] = cfg.ServiceDiscoveryConfig.Configs()
	}

	go tm.run()
	go helpers.LogError("running target manager", tm.manager.Run)

	return tm, tm.manager.ApplyConfig(configs)
}

func (tm *FileTargetManager) run() {
	for targetGroups := range tm.manager.SyncCh() {
		for jobName, groups := range targetGroups {
			tm.syncers[jobName].sync(groups)
		}
	}
}

// Ready if there's at least one file target
func (tm *FileTargetManager) Ready() bool {
	for _, s := range tm.syncers {
		if s.ready() {
			return true
		}
	}
	return false
}

// Stop the TargetManager.
func (tm *FileTargetManager) Stop() {
	tm.quit()

	for _, s := range tm.syncers {
		s.stop()
	}

}

// ActiveTargets returns the active targets currently being scraped.
func (tm *FileTargetManager) ActiveTargets() map[string][]target.Target {
	result := map[string][]target.Target{}
	for jobName, syncer := range tm.syncers {
		result[jobName] = append(result[jobName], syncer.ActiveTargets()...)
	}
	return result
}

// AllTargets returns all targets, active and dropped.
func (tm *FileTargetManager) AllTargets() map[string][]target.Target {
	result := map[string][]target.Target{}
	for jobName, syncer := range tm.syncers {
		result[jobName] = append(result[jobName], syncer.ActiveTargets()...)
		result[jobName] = append(result[jobName], syncer.DroppedTargets()...)
	}
	return result
}

// targetSyncer sync targets based on service discovery changes.
type targetSyncer struct {
	log          log.Logger
	positions    positions.Positions
	entryHandler api.EntryHandler
	hostname     string

	droppedTargets []target.Target
	targets        map[string]*FileTarget
	mtx            sync.Mutex

	relabelConfig []*relabel.Config
	targetConfig  *Config
}

// sync synchronize target based on received target groups received by service discovery
func (s *targetSyncer) sync(groups []*targetgroup.Group) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	targets := map[string]struct{}{}
	dropped := []target.Target{}

	for _, group := range groups {
		for _, t := range group.Targets {
			level.Debug(s.log).Log("msg", "new target", "labels", t)

			discoveredLabels := group.Labels.Merge(t)
			var labelMap = make(map[string]string)
			for k, v := range discoveredLabels.Clone() {
				labelMap[string(k)] = string(v)
			}

			processedLabels := relabel.Process(labels.FromMap(labelMap), s.relabelConfig...)

			var labels = make(model.LabelSet)
			for k, v := range processedLabels.Map() {
				labels[model.LabelName(k)] = model.LabelValue(v)
			}

			// Drop empty targets (drop in relabeling).
			if processedLabels == nil {
				dropped = append(dropped, target.NewDroppedTarget("dropping target, no labels", discoveredLabels))
				level.Debug(s.log).Log("msg", "dropping target, no labels")
				failedTargets.WithLabelValues("empty_labels").Inc()
				continue
			}

			host, ok := labels[hostLabel]
			if ok && string(host) != s.hostname {
				dropped = append(dropped, target.NewDroppedTarget(fmt.Sprintf("ignoring target, wrong host (labels:%s hostname:%s)", labels.String(), s.hostname), discoveredLabels))
				level.Debug(s.log).Log("msg", "ignoring target, wrong host", "labels", labels.String(), "hostname", s.hostname)
				failedTargets.WithLabelValues("wrong_host").Inc()
				continue
			}

			path, ok := labels[pathLabel]
			if !ok {
				dropped = append(dropped, target.NewDroppedTarget("no path for target", discoveredLabels))
				level.Info(s.log).Log("msg", "no path for target", "labels", labels.String())
				failedTargets.WithLabelValues("no_path").Inc()
				continue
			}

			for k := range labels {
				if strings.HasPrefix(string(k), "__") {
					delete(labels, k)
				}
			}

			key := labels.String()
			targets[key] = struct{}{}
			if _, ok := s.targets[key]; ok {
				dropped = append(dropped, target.NewDroppedTarget("ignoring target, already exists", discoveredLabels))
				level.Debug(s.log).Log("msg", "ignoring target, already exists", "labels", labels.String())
				failedTargets.WithLabelValues("exists").Inc()
				continue
			}

			level.Info(s.log).Log("msg", "Adding target", "key", key)
			t, err := s.newTarget(string(path), labels, discoveredLabels)
			if err != nil {
				dropped = append(dropped, target.NewDroppedTarget(fmt.Sprintf("Failed to create target: %s", err.Error()), discoveredLabels))
				level.Error(s.log).Log("msg", "Failed to create target", "key", key, "error", err)
				failedTargets.WithLabelValues("error").Inc()
				continue
			}

			targetsActive.Add(1.)
			s.targets[key] = t
		}
	}

	for key, target := range s.targets {
		if _, ok := targets[key]; !ok {
			level.Info(s.log).Log("msg", "Removing target", "key", key)
			target.Stop()
			targetsActive.Add(-1.)
			delete(s.targets, key)
		}
	}
	s.droppedTargets = dropped
}

func (s *targetSyncer) newTarget(path string, labels model.LabelSet, discoveredLabels model.LabelSet) (*FileTarget, error) {
	return NewFileTarget(s.log, s.entryHandler, s.positions, path, labels, discoveredLabels, s.targetConfig)
}

func (s *targetSyncer) DroppedTargets() []target.Target {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return append([]target.Target(nil), s.droppedTargets...)
}

func (s *targetSyncer) ActiveTargets() []target.Target {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	actives := []target.Target{}
	for _, t := range s.targets {
		actives = append(actives, t)
	}
	return actives
}

func (s *targetSyncer) ready() bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for _, target := range s.targets {
		if target.Ready() {
			return true
		}
	}
	return false
}
func (s *targetSyncer) stop() {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for key, target := range s.targets {
		level.Info(s.log).Log("msg", "Removing target", "key", key)
		target.Stop()
		delete(s.targets, key)
	}
	s.entryHandler.Stop()
}

func hostname() (string, error) {
	hostname := os.Getenv("HOSTNAME")
	if hostname != "" {
		return hostname, nil
	}

	return os.Hostname()
}
