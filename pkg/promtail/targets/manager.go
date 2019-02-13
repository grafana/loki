package targets

import (
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/scrape"
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
	scrapeConfigs []scrape.Config,
	targetConfig *Config,
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
