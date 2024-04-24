package file

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar"
	"github.com/fsnotify/fsnotify"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/kubernetes"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/v3/clients/pkg/logentry/stages"
	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/v3/pkg/util"
)

const (
	pathLabel              = "__path__"
	pathExcludeLabel       = "__path_exclude__"
	hostLabel              = "__host__"
	kubernetesPodNodeField = "spec.nodeName"
)

// FileTargetManager manages a set of targets.
// nolint:revive
type FileTargetManager struct {
	log     log.Logger
	quit    context.CancelFunc
	syncers map[string]*targetSyncer
	manager *discovery.Manager

	watcher            *fsnotify.Watcher
	targetEventHandler chan fileTargetEvent

	wg sync.WaitGroup
}

// NewFileTargetManager creates a new TargetManager.
func NewFileTargetManager(
	metrics *Metrics,
	logger log.Logger,
	positions positions.Positions,
	client api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config,
	targetConfig *Config,
	watchConfig WatchConfig,
) (*FileTargetManager, error) {
	reg := metrics.reg
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	noopRegistry := util.NoopRegistry{}
	noopSdMetrics, err := discovery.CreateAndRegisterSDMetrics(noopRegistry)
	if err != nil {
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	ctx, quit := context.WithCancel(context.Background())
	tm := &FileTargetManager{
		log:                logger,
		quit:               quit,
		watcher:            watcher,
		targetEventHandler: make(chan fileTargetEvent),
		syncers:            map[string]*targetSyncer{},
		manager: discovery.NewManager(
			ctx,
			log.With(logger, "component", "discovery"),
			noopRegistry,
			noopSdMetrics,
		),
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

		pipeline, err := stages.NewPipeline(log.With(logger, "component", "file_pipeline"), cfg.PipelineStages, &cfg.JobName, reg)
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
				kube.Selectors = tm.fulfillKubePodSelector(kube.Selectors, hostname)
			}
		}

		s := &targetSyncer{
			metrics:           metrics,
			log:               logger,
			positions:         positions,
			relabelConfig:     cfg.RelabelConfigs,
			targets:           map[string]*FileTarget{},
			droppedTargets:    []target.Target{},
			hostname:          hostname,
			entryHandler:      pipeline.Wrap(client),
			targetConfig:      targetConfig,
			watchConfig:       watchConfig,
			fileEventWatchers: map[string]chan fsnotify.Event{},
			encoding:          cfg.Encoding,
			decompressCfg:     cfg.DecompressionCfg,
		}
		tm.syncers[cfg.JobName] = s
		configs[cfg.JobName] = cfg.ServiceDiscoveryConfig.Configs()
	}

	tm.wg.Add(3)
	go tm.run(ctx)
	go tm.watchTargetEvents(ctx)
	go tm.watchFsEvents(ctx)

	go util.LogError("running target manager", tm.manager.Run)

	return tm, tm.manager.ApplyConfig(configs)
}

func (tm *FileTargetManager) watchTargetEvents(ctx context.Context) {
	defer tm.wg.Done()

	for {
		select {
		case event := <-tm.targetEventHandler:
			switch event.eventType {
			case fileTargetEventWatchStart:
				if err := tm.watcher.Add(event.path); err != nil {
					level.Error(tm.log).Log("msg", "error adding directory to watcher", "error", err)
				}
			case fileTargetEventWatchStop:
				if err := tm.watcher.Remove(event.path); err != nil {
					if !errors.Is(err, fsnotify.ErrNonExistentWatch) {
						level.Error(tm.log).Log("msg", " failed to remove directory from watcher", "error", err)
					}
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

func (tm *FileTargetManager) watchFsEvents(ctx context.Context) {
	defer tm.wg.Done()

	for {
		select {
		case event := <-tm.watcher.Events:
			// we only care about Create events
			if event.Op == fsnotify.Create {
				level.Info(tm.log).Log("msg", "received file watcher event", "name", event.Name, "op", event.Op.String())
				for _, s := range tm.syncers {
					s.sendFileCreateEvent(event)
				}
			}
		case err := <-tm.watcher.Errors:
			level.Error(tm.log).Log("msg", "error from fswatch", "error", err)
		case <-ctx.Done():
			return
		}
	}
}

func (tm *FileTargetManager) run(ctx context.Context) {
	defer tm.wg.Done()

	for {
		select {
		case targetGroups := <-tm.manager.SyncCh():
			for jobName, groups := range targetGroups {
				tm.syncers[jobName].sync(groups, tm.targetEventHandler)
			}
		case <-ctx.Done():
			return
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
	tm.wg.Wait()

	for _, s := range tm.syncers {
		s.stop()
	}
	util.LogError("closing watcher", tm.watcher.Close)
	close(tm.targetEventHandler)
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

func (tm *FileTargetManager) fulfillKubePodSelector(selectors []kubernetes.SelectorConfig, host string) []kubernetes.SelectorConfig {
	nodeSelector := fmt.Sprintf("%s=%s", kubernetesPodNodeField, host)
	if len(selectors) == 0 {
		return []kubernetes.SelectorConfig{{Role: kubernetes.RolePod, Field: nodeSelector}}
	}

	for _, selector := range selectors {
		if selector.Field == "" {
			selector.Field = nodeSelector
		} else if !strings.Contains(selector.Field, nodeSelector) {
			selector.Field += "," + nodeSelector
		}
		selector.Role = kubernetes.RolePod
	}

	return selectors
}

// targetSyncer sync targets based on service discovery changes.
type targetSyncer struct {
	metrics      *Metrics
	log          log.Logger
	positions    positions.Positions
	entryHandler api.EntryHandler
	hostname     string

	fileEventWatchers map[string]chan fsnotify.Event

	droppedTargets []target.Target
	targets        map[string]*FileTarget
	mtx            sync.Mutex

	relabelConfig []*relabel.Config
	targetConfig  *Config
	watchConfig   WatchConfig

	decompressCfg *scrapeconfig.DecompressionConfig

	encoding string
}

// sync synchronize target based on received target groups received by service discovery
func (s *targetSyncer) sync(groups []*targetgroup.Group, targetEventHandler chan fileTargetEvent) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	targetsSeen := map[string]struct{}{}
	dropped := []target.Target{}

	for _, group := range groups {
		for _, t := range group.Targets {
			level.Debug(s.log).Log("msg", "new target", "labels", t)

			discoveredLabels := group.Labels.Merge(t)
			var labelMap = make(map[string]string)
			for k, v := range discoveredLabels.Clone() {
				labelMap[string(k)] = string(v)
			}

			processedLabels, keep := relabel.Process(labels.FromMap(labelMap), s.relabelConfig...)

			var labels = make(model.LabelSet)
			for k, v := range processedLabels.Map() {
				labels[model.LabelName(k)] = model.LabelValue(v)
			}

			// Drop targets if instructed to drop in relabeling.
			if !keep {
				dropped = append(dropped, target.NewDroppedTarget("dropped target", discoveredLabels))
				level.Debug(s.log).Log("msg", "dropped target")
				s.metrics.failedTargets.WithLabelValues("dropped").Inc()
				continue
			}

			host, ok := labels[hostLabel]
			if ok && string(host) != s.hostname {
				dropped = append(dropped, target.NewDroppedTarget(fmt.Sprintf("ignoring target, wrong host (labels:%s hostname:%s)", labels.String(), s.hostname), discoveredLabels))
				level.Debug(s.log).Log("msg", "ignoring target, wrong host", "labels", labels.String(), "hostname", s.hostname)
				s.metrics.failedTargets.WithLabelValues("wrong_host").Inc()
				continue
			}

			path, ok := labels[pathLabel]
			if !ok {
				dropped = append(dropped, target.NewDroppedTarget("no path for target", discoveredLabels))
				level.Info(s.log).Log("msg", "no path for target", "labels", labels.String())
				s.metrics.failedTargets.WithLabelValues("no_path").Inc()
				continue
			}

			pathExclude := labels[pathExcludeLabel]

			for k := range labels {
				if strings.HasPrefix(string(k), "__") {
					delete(labels, k)
				}
			}

			key := fmt.Sprintf("%s:%s", path, labels.String())
			if pathExclude != "" {
				key = fmt.Sprintf("%s:%s", key, pathExclude)
			}

			targetsSeen[key] = struct{}{}
			if _, ok := s.targets[key]; ok {
				dropped = append(dropped, target.NewDroppedTarget("ignoring target, already exists", discoveredLabels))
				level.Debug(s.log).Log("msg", "ignoring target, already exists", "labels", labels.String())
				s.metrics.failedTargets.WithLabelValues("exists").Inc()
				continue
			}

			level.Info(s.log).Log("msg", "Adding target", "key", key)

			wkey := string(path)
			watcher, ok := s.fileEventWatchers[wkey]
			if !ok {
				watcher = make(chan fsnotify.Event)
				s.fileEventWatchers[wkey] = watcher
			}
			t, err := s.newTarget(wkey, string(pathExclude), labels, discoveredLabels, watcher, targetEventHandler)
			if err != nil {
				dropped = append(dropped, target.NewDroppedTarget(fmt.Sprintf("Failed to create target: %s", err.Error()), discoveredLabels))
				level.Error(s.log).Log("msg", "Failed to create target", "key", key, "error", err)
				s.metrics.failedTargets.WithLabelValues("error").Inc()
				continue
			}

			s.metrics.targetsActive.Add(1.)
			s.targets[key] = t
		}
	}

	s.droppedTargets = dropped

	// keep track of how many targets are using a fileEventWatcher
	watcherUseCount := make(map[string]int, len(s.fileEventWatchers))
	for _, target := range s.targets {
		if _, ok := watcherUseCount[target.path]; !ok {
			watcherUseCount[target.path] = 1
		} else {
			watcherUseCount[target.path]++
		}
	}

	// remove existing targets not seen in groups arg; cleanup unused fileEventWatchers
	for key, target := range s.targets {
		if _, ok := targetsSeen[key]; !ok {
			level.Info(s.log).Log("msg", "Removing target", "key", key)
			target.Stop()
			s.metrics.targetsActive.Add(-1.)
			delete(s.targets, key)

			// close related file event watcher if no other targets are using
			k := target.path
			_, ok := watcherUseCount[k]
			if !ok {
				level.Warn(s.log).Log("msg", "failed to find file event watcher", "path", k)
				continue
			} else { //nolint:revive
				if watcherUseCount[k]--; watcherUseCount[k] > 0 {
					// Multiple targets are using this file watcher, leave it alone
					continue
				}
			}
			if _, ok := s.fileEventWatchers[k]; ok {
				close(s.fileEventWatchers[k])
				delete(s.fileEventWatchers, k)
			} else {
				level.Warn(s.log).Log("msg", "failed to remove file event watcher", "path", k)
			}
		}
	}
}

// sendFileCreateEvent sends file creation events to only the targets with matched path.
func (s *targetSyncer) sendFileCreateEvent(event fsnotify.Event) {
	// Lock the mutex because other threads are manipulating s.fileEventWatchers which can lead to a deadlock
	// where we send events to channels where nobody is listening anymore
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for path, watcher := range s.fileEventWatchers {
		matched, err := doublestar.Match(path, event.Name)
		if err != nil {
			level.Error(s.log).Log("msg", "failed to match file", "error", err, "filename", event.Name)
			continue
		}
		if !matched {
			level.Debug(s.log).Log("msg", "new file does not match glob", "filename", event.Name)
			continue
		}
		watcher <- event
	}
}

func (s *targetSyncer) newTarget(path, pathExclude string, labels model.LabelSet, discoveredLabels model.LabelSet, fileEventWatcher chan fsnotify.Event, targetEventHandler chan fileTargetEvent) (*FileTarget, error) {
	return NewFileTarget(s.metrics, s.log, s.entryHandler, s.positions, path, pathExclude, labels, discoveredLabels, s.targetConfig, s.watchConfig, fileEventWatcher, targetEventHandler, s.encoding, s.decompressCfg)
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

	for key, watcher := range s.fileEventWatchers {
		close(watcher)
		delete(s.fileEventWatchers, key)
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
