package lokipush

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/targets/target"
)

// PushTargetManager manages a series of PushTargets.
type PushTargetManager struct {
	logger  log.Logger
	targets map[string]*PushTarget
}

// NewPushTargetManager creates a new PushTargetManager.
func NewPushTargetManager(
	logger log.Logger,
	client api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config,
) (*PushTargetManager, error) {

	tm := &PushTargetManager{
		logger:  logger,
		targets: make(map[string]*PushTarget),
	}

	if err := validateJobName(scrapeConfigs); err != nil {
		return nil, err
	}

	for _, cfg := range scrapeConfigs {
		registerer := prometheus.DefaultRegisterer
		pipeline, err := stages.NewPipeline(log.With(logger, "component", "push_pipeline_"+cfg.JobName), cfg.PipelineStages, &cfg.JobName, registerer)
		if err != nil {
			return nil, err
		}

		t, err := NewPushTarget(logger, pipeline.Wrap(client), cfg.RelabelConfigs, cfg.JobName, cfg.PushConfig)
		if err != nil {
			return nil, err
		}

		tm.targets[cfg.JobName] = t
	}

	return tm, nil
}

func validateJobName(scrapeConfigs []scrapeconfig.Config) error {
	jobNames := map[string]struct{}{}
	for i, cfg := range scrapeConfigs {
		if cfg.JobName == "" {
			return errors.New("`job_name` must be defined for the `push` scrape_config with a " +
				"unique name to properly register metrics, " +
				"at least one `push` scrape_config has no `job_name` defined")
		}
		if _, ok := jobNames[cfg.JobName]; ok {
			return fmt.Errorf("`job_name` must be unique for each `push` scrape_config, "+
				"a duplicate `job_name` of %s was found", cfg.JobName)
		}
		jobNames[cfg.JobName] = struct{}{}

		scrapeConfigs[i].JobName = strings.Replace(cfg.JobName, " ", "_", -1)
	}
	return nil
}

// Ready returns true if at least one PushTarget is also ready.
func (tm *PushTargetManager) Ready() bool {
	for _, t := range tm.targets {
		if t.Ready() {
			return true
		}
	}
	return false
}

// Stop stops the PushTargetManager and all of its PushTargets.
func (tm *PushTargetManager) Stop() {
	for _, t := range tm.targets {
		if err := t.Stop(); err != nil {
			level.Error(t.logger).Log("msg", "error stopping PushTarget", "err", err.Error())
		}
	}
}

// ActiveTargets returns the list of PushTargets where Push data
// is being read. ActiveTargets is an alias to AllTargets as
// PushTargets cannot be deactivated, only stopped.
func (tm *PushTargetManager) ActiveTargets() map[string][]target.Target {
	return tm.AllTargets()
}

// AllTargets returns the list of all targets where Push data
// is currently being read.
func (tm *PushTargetManager) AllTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.targets))
	for k, v := range tm.targets {
		result[k] = []target.Target{v}
	}
	return result
}
