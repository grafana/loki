package kafka

import (
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/clients/pkg/promtail/client"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
)

// TargetManager manages a series of kafka targets.
type TargetManager struct {
	logger        log.Logger
	targetSyncers map[string]*TargetSyncer
}

// NewTargetManager creates a new Kafka managers.
func NewTargetManager(
	reg prometheus.Registerer,
	logger log.Logger,
	scrapeConfigs []scrapeconfig.Config,
	clientConfigs ...client.Config,
) (*TargetManager, error) {
	tm := &TargetManager{
		logger:        logger,
		targetSyncers: make(map[string]*TargetSyncer),
	}
	for _, cfg := range scrapeConfigs {
		t, err := NewSyncer(reg, logger, cfg, clientConfigs...)
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
		if len(t.getActiveTargets()) > 0 {
			return true
		}
	}
	return false
}

func (tm *TargetManager) Stop() {
	for _, t := range tm.targetSyncers {
		if err := t.Stop(); err != nil {
			level.Error(t.logger).Log("msg", "error stopping kafka target", "err", err)
		}
	}
}

func (tm *TargetManager) ActiveTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.targetSyncers))
	for k, v := range tm.targetSyncers {
		result[k] = v.getActiveTargets()
	}
	return result
}

func (tm *TargetManager) AllTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.targetSyncers))
	for k, v := range tm.targetSyncers {
		result[k] = append(v.getActiveTargets(), v.getDroppedTargets()...)
	}
	return result
}
