package targets

import (
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/scrape"
	"github.com/prometheus/client_golang/prometheus"
)

// HTTPTargetManager manages a series of HTTPTargets.
type HTTPTargetManager struct {
	logger  log.Logger
	targets map[string]*HTTPTarget
}

// NewHTTPTargetManager creates a new HTTPTargetManager.
func NewHTTPTargetManager(
	logger log.Logger,
	client api.EntryHandler,
	scrapeConfigs []scrape.Config,
) (*HTTPTargetManager, error) {

	tm := &HTTPTargetManager{
		logger:  logger,
		targets: make(map[string]*HTTPTarget),
	}

	if len(scrapeConfigs) > 1 {
		level.Warn(logger).Log("msg", "multiple http scrape_config sections found: only the first will be used")
	}

	for _, cfg := range scrapeConfigs {
		registerer := prometheus.DefaultRegisterer
		pipeline, err := stages.NewPipeline(log.With(logger, "component", "http_pipeline"), cfg.PipelineStages, &cfg.JobName, registerer)
		if err != nil {
			return nil, err
		}

		t, err := NewHTTPTarget(logger, pipeline.Wrap(client), cfg.RelabelConfigs, cfg.HTTPConfig)
		if err != nil {
			return nil, err
		}

		tm.targets[cfg.JobName] = t
		break
	}

	return tm, nil
}

// Ready returns true if at least one HTTPTarget is also ready.
func (tm *HTTPTargetManager) Ready() bool {
	for _, t := range tm.targets {
		if t.Ready() {
			return true
		}
	}
	return false
}

// Stop stops the HTTPTargetManager and all of its HTTPTargets.
func (tm *HTTPTargetManager) Stop() {
	for _, t := range tm.targets {
		if err := t.Stop(); err != nil {
			level.Error(t.logger).Log("msg", "error stopping HTTPTarget", "err", err.Error())
		}
	}
}

// ActiveTargets returns the list of HTTPTargets where journal data
// is being read. ActiveTargets is an alias to AllTargets as
// HTTPTargets cannot be deactivated, only stopped.
func (tm *HTTPTargetManager) ActiveTargets() map[string][]Target {
	return tm.AllTargets()
}

// AllTargets returns the list of all targets where journal data
// is currently being read.
func (tm *HTTPTargetManager) AllTargets() map[string][]Target {
	result := make(map[string][]Target, len(tm.targets))
	for k, v := range tm.targets {
		result[k] = []Target{v}
	}
	return result
}
