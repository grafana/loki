package targets

import (
	"github.com/grafana/logish/pkg/promtail"
	"github.com/grafana/logish/pkg/promtail/targets/file"
	"github.com/grafana/logish/pkg/promtail/targets/journal"
	"github.com/pkg/errors"
	"github.com/weaveworks/cortex/pkg/util"
)

type Target interface {
	Stop() error
}

type TargetManager struct {
	targets []Target
}

func NewTargetManager(
	scfg []promtail.ScrapeConfig,
	client *promtail.Client,
	positions *promtail.Positions,
) (*TargetManager, error) {
	targets := make([]Target, 0)

	tm, err := file.NewTargetManager(util.Logger, scfg, client, positions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to make file target manager")
	}
	targets = append(targets, tm)

	for _, cfg := range scfg {
		if cfg.JournalConfig == nil {
			continue
		}

		jcfg := cfg.JournalConfig
		jt, err := journal.NewJournalTarget(
			client,
			positions,
			jcfg.Path,
			cfg.JobName,
			cfg.RelabelConfigs,
		)
		if err != nil {
			return nil, errors.Wrap(err, "failed to make journal target")
		}

		targets = append(targets, jt)
	}

	return &TargetManager{targets: targets}, nil
}

func (tm *TargetManager) Stop() error {
	var lastError error
	for _, t := range tm.targets {
		lastError = t.Stop()
	}

	return lastError
}
