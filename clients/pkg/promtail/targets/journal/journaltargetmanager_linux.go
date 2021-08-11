// +build cgo

package journal

import (
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
)

// JournalTargetManager manages a series of JournalTargets.
// nolint
type JournalTargetManager struct {
	logger  log.Logger
	targets map[string]*JournalTarget
}

// NewJournalTargetManager creates a new JournalTargetManager.
func NewJournalTargetManager(
	reg prometheus.Registerer,
	logger log.Logger,
	positions positions.Positions,
	client api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config,
) (*JournalTargetManager, error) {
	tm := &JournalTargetManager{
		logger:  logger,
		targets: make(map[string]*JournalTarget),
	}

	for _, cfg := range scrapeConfigs {
		if cfg.JournalConfig == nil {
			continue
		}

		pipeline, err := stages.NewPipeline(log.With(logger, "component", "journal_pipeline"), cfg.PipelineStages, &cfg.JobName, reg)
		if err != nil {
			return nil, err
		}

		t, err := NewJournalTarget(
			logger,
			pipeline.Wrap(client),
			positions,
			cfg.JobName,
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
		if err := t.Stop(); err != nil {
			level.Error(t.logger).Log("msg", "error stopping JournalTarget", "err", err.Error())
		}
	}
}

// ActiveTargets returns the list of JournalTargets where journal data
// is being read. ActiveTargets is an alias to AllTargets as
// JournalTargets cannot be deactivated, only stopped.
func (tm *JournalTargetManager) ActiveTargets() map[string][]target.Target {
	return tm.AllTargets()
}

// AllTargets returns the list of all targets where journal data
// is currently being read.
func (tm *JournalTargetManager) AllTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.targets))
	for k, v := range tm.targets {
		result[k] = []target.Target{v}
	}
	return result
}
