package targets

import (
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
)

type targetManager interface {
	Stop()
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
	scrapeConfigs []api.ScrapeConfig,
	targetConfig *api.TargetConfig,
) (*TargetManagers, error) {
	var targetManagers []targetManager
	var fileScrapeConfigs []api.ScrapeConfig

	// for now every scrape config is a file target
	fileScrapeConfigs = append(fileScrapeConfigs, scrapeConfigs...)
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

	return &TargetManagers{targetManagers: targetManagers}, nil

}

// Stop the TargetManagers.
func (tm *TargetManagers) Stop() {
	for _, t := range tm.targetManagers {
		t.Stop()
	}
}
