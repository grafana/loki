package targets

import (
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

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
	logRegistry prometheus.Registerer,
) (*TargetManagers, error) {
	var targetManagers []targetManager
	var fileScrapeConfigs []scrape.Config

	// for now every scrape config is a file target
	fileScrapeConfigs = append(fileScrapeConfigs, scrapeConfigs...)
	fileTargetManager, err := NewFileTargetManager(
		logger,
		positions,
		client,
		fileScrapeConfigs,
		targetConfig,
		logRegistry,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make file target manager")
	}
	targetManagers = append(targetManagers, fileTargetManager)

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

// Ready if there's at least one ready FileTargetManager
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
