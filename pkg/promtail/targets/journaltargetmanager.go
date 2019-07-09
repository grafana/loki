package targets

import (
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/scrape"
	"github.com/prometheus/client_golang/prometheus"
)

// JournalTargetManager manages a series of JournalTargets.
type JournalTargetManager struct {
	targets map[string]*JournalTarget
}

// NewJournalTargetManager creates a new JournalTargetManager.
func NewJournalTargetManager(
	logger log.Logger,
	positions *positions.Positions,
	client api.EntryHandler,
	scrapeConfigs []scrape.Config,
) (*JournalTargetManager, error) {
	tm := &JournalTargetManager{
		targets: make(map[string]*JournalTarget),
	}

	for _, cfg := range scrapeConfigs {
		if cfg.JournalConfig == nil {
			continue
		}

		// TODO(rfratto): DRY the duplicate code from NewFileTargetManager?
		registerer := prometheus.DefaultRegisterer
		pipeline, err := stages.NewPipeline(log.With(logger, "component", "pipeline"), cfg.PipelineStages, &cfg.JobName, registerer)
		if err != nil {
			return nil, err
		}

		// Backwards compatibility with old EntryParser config
		if pipeline.Size() == 0 {
			switch cfg.EntryParser {
			case api.CRI:
				level.Warn(logger).Log("msg", "WARNING!!! entry_parser config is deprecated, please change to pipeline_stages")
				cri, err := stages.NewCRI(logger, registerer)
				if err != nil {
					return nil, err
				}
				pipeline.AddStage(cri)
			case api.Docker:
				level.Warn(logger).Log("msg", "WARNING!!! entry_parser config is deprecated, please change to pipeline_stages")
				docker, err := stages.NewDocker(logger, registerer)
				if err != nil {
					return nil, err
				}
				pipeline.AddStage(docker)
			case api.Raw:
				level.Warn(logger).Log("msg", "WARNING!!! entry_parser config is deprecated, please change to pipeline_stages")
			default:

			}
		}

		t, err := NewJournalTarget(
			logger,
			pipeline.Wrap(client),
			positions,
			cfg.RelabelConfigs,
			cfg.JournalConfig,
		)
		if err != nil {
			return nil, err
		}

		tm.targets[cfg.JobName] = t
	}

	return tm, nil
}

// Ready returns true if at least one JournalTarget is also ready.
func (tm *JournalTargetManager) Ready() bool {
	for _, t := range tm.targets {
		if t.Ready() {
			return true
		}
	}
	return false
}

// Stop stops the JournalTargetManager and all of its JournalTargets.
func (tm *JournalTargetManager) Stop() {
	for _, t := range tm.targets {
		t.Stop()
	}
}

// ActiveTargets returns the list of JournalTargets where journal data
// is being read. ActiveTargets is an alias to AllTargets as
// JournalTargets cannot be deactivated, only stopped.
func (tm *JournalTargetManager) ActiveTargets() map[string][]Target {
	return tm.AllTargets()
}

// AllTargets returns the list of all targets where journal data
// is currently being read.
func (tm *JournalTargetManager) AllTargets() map[string][]Target {
	result := make(map[string][]Target, len(tm.targets))
	for k, v := range tm.targets {
		result[k] = []Target{v}
	}
	return result
}
