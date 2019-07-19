package targets

import (
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/scrape"
)

type targetManager interface {
	Ready() bool
	Stop()
	ActiveTargets() map[string][]Target
	AllTargets() map[string][]Target
}

// TargetManagers manages a list of target managers.
type TargetManagers struct {
	targetManagers []targetManager
}

// NewTargetManagers makes a new TargetManagers
func NewTargetManagers(
	logger log.Logger,
	positions *positions.Positions,
	client api.EntryHandler,
	scrapeConfigs []scrape.Config,
	targetConfig *Config,
) (*TargetManagers, error) {
	var targetManagers []targetManager
	var fileScrapeConfigs []scrape.Config
	var journalScrapeConfigs []scrape.Config

	for _, cfg := range scrapeConfigs {
		if cfg.HasServiceDiscoveryConfig() {
			fileScrapeConfigs = append(fileScrapeConfigs, cfg)
		}
	}
	if len(fileScrapeConfigs) > 0 {
		fileTargetManager, err := NewFileTargetManager(
			logger,
			positions,
			client,
			fileScrapeConfigs,
			targetConfig,
		)
		if err != nil {
			return nil, errors.Wrap(err, "failed to make file target manager")
		}
		targetManagers = append(targetManagers, fileTargetManager)
	}

	for _, cfg := range scrapeConfigs {
		if cfg.JournalConfig != nil {
			journalScrapeConfigs = append(journalScrapeConfigs, cfg)
		}
	}
	if len(journalScrapeConfigs) > 0 {
		journalTargetManager, err := NewJournalTargetManager(
			logger,
			positions,
			client,
			journalScrapeConfigs,
		)
		if err != nil {
			return nil, errors.Wrap(err, "failed to make journal target manager")
		}
		targetManagers = append(targetManagers, journalTargetManager)
	}

	return &TargetManagers{targetManagers: targetManagers}, nil

}

// ActiveTargets returns active targets per jobs
func (tm *TargetManagers) ActiveTargets() map[string][]Target {
	result := map[string][]Target{}
	for _, t := range tm.targetManagers {
		for job, targets := range t.ActiveTargets() {
			result[job] = append(result[job], targets...)
		}
	}
	return result
}

// AllTargets returns all targets per jobs
func (tm *TargetManagers) AllTargets() map[string][]Target {
	result := map[string][]Target{}
	for _, t := range tm.targetManagers {
		for job, targets := range t.AllTargets() {
			result[job] = append(result[job], targets...)
		}
	}
	return result
}

// Ready if there's at least one ready target manager.
func (tm *TargetManagers) Ready() bool {
	for _, t := range tm.targetManagers {
		if t.Ready() {
			return true
		}
	}
	return false
}

// Stop the TargetManagers.
func (tm *TargetManagers) Stop() {
	for _, t := range tm.targetManagers {
		t.Stop()
	}
}
