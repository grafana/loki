package targets

import (
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
)

type GenericTargetManager interface {
	Stop()
}

type TargetManager struct {
	targetManagers []GenericTargetManager
}

func NewTargetManager(
	logger log.Logger,
	positions *positions.Positions,
	client api.EntryHandler,
	scrapeConfigs []api.ScrapeConfig,
) (*TargetManager, error) {
	var targetManagers []GenericTargetManager
	var fileScrapeConfigs []api.ScrapeConfig

	for _, cfg := range scrapeConfigs {
		// for now every scrape config is a file target
		fileScrapeConfigs = append(
			fileScrapeConfigs,
			cfg,
		)
	}

	fileTargetManager, err := NewTargetManager(
		logger,
		positions,
		client,
		fileScrapeConfigs,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make file target manager")
	}
	targetManagers = append(targetManagers, fileTargetManager)

	return &TargetManager{targetManagers: targetManagers}, nil

}

func (tm *TargetManager) Stop() {
	for _, t := range tm.targetManagers {
		go func() {
			t.Stop()
		}()
	}
}
