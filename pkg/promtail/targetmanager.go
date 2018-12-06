package promtail

import (
	"context"
	"os"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/helpers"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	sd_config "github.com/prometheus/prometheus/discovery/config"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/relabel"
)

const (
	pathLabel = "__path__"
	hostLabel = "__host__"
)

// TargetManager manages a set of targets.
type TargetManager struct {
	log log.Logger

	quit    context.CancelFunc
	syncers map[string]*syncer
	manager *discovery.Manager
}

// NewTargetManager creates a new TargetManager.
func NewTargetManager(
	logger log.Logger,
	positions *Positions,
	client EntryHandler,
	scrapeConfig []ScrapeConfig,
) (*TargetManager, error) {
	ctx, quit := context.WithCancel(context.Background())
	tm := &TargetManager{
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
	for _, cfg := range scrapeConfig {
		s := &syncer{
			log:           logger,
			positions:     positions,
			relabelConfig: cfg.RelabelConfigs,
			targets:       map[string]*Target{},
			hostname:      hostname,
			entryHandler:  cfg.EntryParser.Wrap(client),
		}
		tm.syncers[cfg.JobName] = s
		config[cfg.JobName] = cfg.ServiceDiscoveryConfig
	}

	go tm.run()
	go helpers.LogError("running target manager", tm.manager.Run)

	return tm, tm.manager.ApplyConfig(config)
}

func (tm *TargetManager) run() {
	for targetGoups := range tm.manager.SyncCh() {
		for jobName, groups := range targetGoups {
			tm.syncers[jobName].Sync(groups)
		}
	}
}

// Stop the TargetManager.
func (tm *TargetManager) Stop() {
	tm.quit()

	for _, s := range tm.syncers {
		s.stop()
	}
}

type syncer struct {
	log          log.Logger
	positions    *Positions
	entryHandler EntryHandler
	hostname     string

	targets       map[string]*Target
	relabelConfig []*config.RelabelConfig
}

func (s *syncer) Sync(groups []*targetgroup.Group) {
	targets := map[string]struct{}{}

	for _, group := range groups {
		for _, t := range group.Targets {
			level.Debug(s.log).Log("msg", "new target", "labels", t)

			labels := group.Labels.Merge(t)
			labels = relabel.Process(labels, s.relabelConfig...)

			// Drop empty targets (drop in relabeling).
			if labels == nil {
				level.Debug(s.log).Log("msg", "dropping target, no labels")
				continue
			}

			host, ok := labels[hostLabel]
			if ok && string(host) != s.hostname {
				level.Debug(s.log).Log("msg", "ignoring target, wrong host", "labels", labels.String(), "hostname", s.hostname)
				continue
			}

			path, ok := labels[pathLabel]
			if !ok {
				level.Info(s.log).Log("msg", "no path for target", "labels", labels.String())
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
				continue
			}

			level.Info(s.log).Log("msg", "Adding target", "key", key)
			t, err := s.newTarget(string(path), labels)
			if err != nil {
				level.Error(s.log).Log("msg", "Failed to create target", "key", key, "error", err)
				continue
			}

			s.targets[key] = t
		}
	}

	for key, target := range s.targets {
		if _, ok := targets[key]; !ok {
			level.Info(s.log).Log("msg", "Removing target", "key", key)
			target.Stop()
			delete(s.targets, key)
		}
	}
}

func (s *syncer) newTarget(path string, labels model.LabelSet) (*Target, error) {
	return NewTarget(s.log, s.entryHandler, s.positions, path, labels)
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
