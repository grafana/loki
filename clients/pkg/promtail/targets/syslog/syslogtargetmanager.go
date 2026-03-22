package syslog

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/clients/pkg/logentry/stages"
	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"
)

// SyslogTargetManager manages a series of SyslogTargets.
// nolint:revive
type SyslogTargetManager struct {
	logger  log.Logger
	targets map[string]*SyslogTarget
}

// NewSyslogTargetManager creates a new SyslogTargetManager.
func NewSyslogTargetManager(
	metrics *Metrics,
	logger log.Logger,
	client api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config,
) (*SyslogTargetManager, error) {
	reg := metrics.reg
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	tm := &SyslogTargetManager{
		logger:  logger,
		targets: make(map[string]*SyslogTarget),
	}

	for _, cfg := range scrapeConfigs {
		pipeline, err := stages.NewPipeline(log.With(logger, "component", "syslog_pipeline"), cfg.PipelineStages, &cfg.JobName, reg)
		if err != nil {
			return nil, err
		}

		t, err := NewSyslogTarget(metrics, logger, pipeline.Wrap(client), cfg.RelabelConfigs, cfg.SyslogConfig)
		if err != nil {
			return nil, err
		}

		tm.targets[cfg.JobName] = t
	}

	return tm, nil
}

// Ready returns true if at least one SyslogTarget is also ready.
func (tm *SyslogTargetManager) Ready() bool {
	for _, t := range tm.targets {
		if t.Ready() {
			return true
		}
	}
	return false
}

// Stop stops the SyslogTargetManager and all of its SyslogTargets.
func (tm *SyslogTargetManager) Stop() {
	for _, t := range tm.targets {
		if err := t.Stop(); err != nil {
			level.Error(t.logger).Log("msg", "error stopping SyslogTarget", "err", err.Error())
		}
	}
}

// ActiveTargets returns the list of SyslogTargets where syslog data
// is being read. ActiveTargets is an alias to AllTargets as
// SyslogTargets cannot be deactivated, only stopped.
func (tm *SyslogTargetManager) ActiveTargets() map[string][]target.Target {
	return tm.AllTargets()
}

// AllTargets returns the list of all targets where syslog data
// is currently being read.
func (tm *SyslogTargetManager) AllTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.targets))
	for k, v := range tm.targets {
		result[k] = []target.Target{v}
	}
	return result
}
