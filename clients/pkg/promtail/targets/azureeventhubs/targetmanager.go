package azureeventhubs

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/kafka"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"
)

// TargetManager manages a series of kafka targets.
type TargetManager struct {
	logger        log.Logger
	targetSyncers map[string]*kafka.TargetSyncer
}

// NewTargetManager creates a new Kafka managers.
func NewTargetManager(
	reg prometheus.Registerer,
	logger log.Logger,
	pushClient api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config,
) (*TargetManager, error) {
	tm := &TargetManager{
		logger:        logger,
		targetSyncers: make(map[string]*kafka.TargetSyncer),
	}
	for _, cfg := range scrapeConfigs {
		t, err := NewSyncer(reg, logger, cfg, pushClient)
		if err != nil {
			return nil, err
		}
		tm.targetSyncers[cfg.JobName] = t
	}

	return tm, nil
}

// Ready returns true if at least one Kafka target is active.
func (tm *TargetManager) Ready() bool {
	for _, t := range tm.targetSyncers {
		if len(t.ActiveTargets()) > 0 {
			return true
		}
	}
	return false
}

func (tm *TargetManager) Stop() {
	for _, t := range tm.targetSyncers {
		if err := t.Stop(); err != nil {
			level.Error(tm.logger).Log("msg", "error stopping azureeventhub target", "err", err)
		}
	}
}

func (tm *TargetManager) ActiveTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.targetSyncers))
	for k, v := range tm.targetSyncers {
		result[k] = v.ActiveTargets()
	}
	return result
}

func (tm *TargetManager) AllTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.targetSyncers))
	for k, v := range tm.targetSyncers {
		result[k] = append(v.ActiveTargets(), v.DroppedTargets()...)
	}
	return result
}
