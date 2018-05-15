package promtail

import (
	"context"
	"os"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
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

type NewTargetFunc func(path string, labels model.LabelSet) (*Target, error)

type TargetManager struct {
	syncers map[string]*syncer
	manager *discovery.Manager

	log  log.Logger
	quit context.CancelFunc
}

func hostname() (string, error) {
	hostname := os.Getenv("HOSTNAME")
	if hostname != "" {
		return hostname, nil
	}

	return os.Hostname()
}

func NewTargetManager(
	logger log.Logger,
	scrapeConfig []ScrapeConfig,
	fn NewTargetFunc,
) (*TargetManager, error) {
	ctx, quit := context.WithCancel(context.Background())
	tm := &TargetManager{
		log:     logger,
		syncers: map[string]*syncer{},
		manager: discovery.NewManager(ctx, log.With(logger, "component", "discovery")),
		quit:    quit,
	}

	hostname, err := hostname()
	if err != nil {
		return nil, err
	}

	config := map[string]sd_config.ServiceDiscoveryConfig{}

	for _, cfg := range scrapeConfig {
		s := &syncer{
			log:           logger,
			newTarget:     fn,
			relabelConfig: cfg.RelabelConfigs,
			targets:       map[string]*Target{},
			hostname:      hostname,
		}
		tm.syncers[cfg.JobName] = s
		config[cfg.JobName] = cfg.ServiceDiscoveryConfig
	}

	go tm.run()

	return tm, tm.manager.ApplyConfig(config)
}

func (tm *TargetManager) run() {
	for targetGoups := range tm.manager.SyncCh() {
		for jobName, groups := range targetGoups {
			tm.syncers[jobName].Sync(groups)
		}
	}
}

func (tm *TargetManager) Stop() {
	tm.quit()

	for _, s := range tm.syncers {
		s.stop()
	}
}

type syncer struct {
	log           log.Logger
	newTarget     NewTargetFunc
	targets       map[string]*Target
	relabelConfig []*config.RelabelConfig
	hostname      string
}

func (s *syncer) Sync(groups []*targetgroup.Group) {
	targets := map[string]struct{}{}

	for _, group := range groups {
		for _, t := range group.Targets {
			labels := group.Labels.Merge(t)
			labels = relabel.Process(labels, s.relabelConfig...)
			// Drop empty targets (drop in relabeling).
			if labels == nil {
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

func (s *syncer) stop() {
	for key, target := range s.targets {
		level.Info(s.log).Log("msg", "Removing target", "key", key)
		target.Stop()
		delete(s.targets, key)
	}
}
