//go:build !windows
// +build !windows

package windows

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"
)

// TargetManager manages a series of windows event targets.
type TargetManager struct{}

// NewTargetManager creates a new Windows managers.
func NewTargetManager(
	_ prometheus.Registerer,
	logger log.Logger,
	_ api.EntryHandler,
	_ []scrapeconfig.Config,
) (*TargetManager, error) {
	level.Warn(logger).Log("msg", "WARNING!!! Windows target was configured but support for reading the windows event is not compiled into this build of promtail!")
	return &TargetManager{}, nil
}

// Ready returns true if at least one Windows target is also ready.
func (tm *TargetManager) Ready() bool { return false }

// Stop stops the Windows target manager and all of its targets.
func (tm *TargetManager) Stop() {}

// ActiveTargets returns the list of active Windows targets.
func (tm *TargetManager) ActiveTargets() map[string][]target.Target { return nil }

// AllTargets returns the list of all targets.
func (tm *TargetManager) AllTargets() map[string][]target.Target { return nil }
