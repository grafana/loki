package targets

import (
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
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
	positions      positions.Positions
}

// NewTargetManagers makes a new TargetManagers
func NewTargetManagers(
	app Shutdownable,
	logger log.Logger,
	positionsConfig positions.Config,
	client api.EntryHandler,
	scrapeConfigs []scrape.Config,
	targetConfig *Config,
) (*TargetManagers, error) {
	var targetManagers []targetManager
	var fileScrapeConfigs []scrape.Config
	var journalScrapeConfigs []scrape.Config
	var syslogScrapeConfigs []scrape.Config

	if targetConfig.Stdin {
		level.Debug(util.Logger).Log("msg", "configured to read from stdin")
		stdin, err := newStdinTargetManager(app, client, scrapeConfigs)
		if err != nil {
			return nil, err
		}
		targetManagers = append(targetManagers, stdin)
		return &TargetManagers{targetManagers: targetManagers}, nil
	}

	positions, err := positions.New(util.Logger, positionsConfig)
	if err != nil {
		return nil, err
	}

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

	for _, cfg := range scrapeConfigs {
		if cfg.SyslogConfig != nil {
			syslogScrapeConfigs = append(syslogScrapeConfigs, cfg)
		}
	}
	if len(syslogScrapeConfigs) > 0 {
		syslogTargetManager, err := NewSyslogTargetManager(logger, client, syslogScrapeConfigs)
		if err != nil {
			return nil, errors.Wrap(err, "failed to make syslog target manager")
		}
		targetManagers = append(targetManagers, syslogTargetManager)
	}

	return &TargetManagers{
		targetManagers: targetManagers,
		positions:      positions,
	}, nil

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
	if tm.positions != nil {
		tm.positions.Stop()
	}
}
