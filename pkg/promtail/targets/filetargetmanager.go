package targets

import (
	"context"
	"os"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"
	sd_config "github.com/prometheus/prometheus/discovery/config"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	pkgrelabel "github.com/prometheus/prometheus/pkg/relabel"
	"github.com/prometheus/prometheus/relabel"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
)

const (
	pathLabel = "__path__"
	hostLabel = "__host__"
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
type FileTargetManager struct {
	log log.Logger

	quit    context.CancelFunc
	syncers map[string]*syncer
	manager *discovery.Manager
}

// NewFileTargetManager creates a new TargetManager.
func NewFileTargetManager(
	logger log.Logger,
	positions *positions.Positions,
	client api.EntryHandler,
	scrapeConfigs []api.ScrapeConfig,
	targetConfig *api.TargetConfig,
) (*FileTargetManager, error) {
	ctx, quit := context.WithCancel(context.Background())
	tm := &FileTargetManager{
		log: logger,

		quit:    quit,
		syncers: map[string]*syncer{},
		manager: discovery.NewManager(ctx, log.With(logger, "component", "discovery")),
	}

	hostname, err := hostname()
	if err != nil {
		return nil, err
	}

	config := map[string]sd_config.ServiceDiscoveryConfig{}
	for _, cfg := range scrapeConfigs {
		s := &syncer{
			log:           logger,
			positions:     positions,
			relabelConfig: cfg.RelabelConfigs,
			targets:       map[string]*FileTarget{},
			hostname:      hostname,
			entryHandler:  cfg.EntryParser.Wrap(client),
			targetConfig:  targetConfig,
		}
		tm.syncers[cfg.JobName] = s
		config[cfg.JobName] = cfg.ServiceDiscoveryConfig
	}

	go tm.run()
	go helpers.LogError("running target manager", tm.manager.Run)

	return tm, tm.manager.ApplyConfig(config)
}

func (tm *FileTargetManager) run() {
	for targetGoups := range tm.manager.SyncCh() {
		for jobName, groups := range targetGoups {
			tm.syncers[jobName].sync(groups)
		}
	}
}

// Stop the TargetManager.
func (tm *FileTargetManager) Stop() {
	tm.quit()

	for _, s := range tm.syncers {
		s.stop()
	}

}

type syncer struct {
	log          log.Logger
	positions    *positions.Positions
	entryHandler api.EntryHandler
	hostname     string

	targets       map[string]*FileTarget
	relabelConfig []*pkgrelabel.Config

	targetConfig *api.TargetConfig
}

func (s *syncer) sync(groups []*targetgroup.Group) {
	targets := map[string]struct{}{}

	for _, group := range groups {
		for _, t := range group.Targets {
			level.Debug(s.log).Log("msg", "new target", "labels", t)

			labels := group.Labels.Merge(t)
			labels = relabel.Process(labels, s.relabelConfig...)

			// Drop empty targets (drop in relabeling).
			if labels == nil {
				level.Debug(s.log).Log("msg", "dropping target, no labels")
				failedTargets.WithLabelValues("empty_labels").Inc()
				continue
			}

			host, ok := labels[hostLabel]
			if ok && string(host) != s.hostname {
				level.Debug(s.log).Log("msg", "ignoring target, wrong host", "labels", labels.String(), "hostname", s.hostname)
				failedTargets.WithLabelValues("wrong_host").Inc()
				continue
			}

			path, ok := labels[pathLabel]
			if !ok {
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
				level.Debug(s.log).Log("msg", "ignoring target, already exists", "labels", labels.String())
				failedTargets.WithLabelValues("exists").Inc()
				continue
			}

			level.Info(s.log).Log("msg", "Adding target", "key", key)
			t, err := s.newTarget(string(path), labels)
			if err != nil {
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
}

func (s *syncer) newTarget(path string, labels model.LabelSet) (*FileTarget, error) {
	return NewFileTarget(s.log, s.entryHandler, s.positions, path, labels, s.targetConfig)
}

func (s *syncer) stop() {
	for key, target := range s.targets {
		level.Info(s.log).Log("msg", "Removing target", "key", key)
		target.Stop()
		delete(s.targets, key)
	}
}

func hostname() (string, error) {
	hostname := os.Getenv("HOSTNAME")
	if hostname != "" {
		return hostname, nil
	}

	return os.Hostname()
}
