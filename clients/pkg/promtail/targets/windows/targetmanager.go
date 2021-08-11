//+build !windows

package windows

import (
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
)

// TargetManager manages a series of windows event targets.
type TargetManager struct{}

// NewTargetManager creates a new Windows managers.
func NewTargetManager(
	reg prometheus.Registerer,
	logger log.Logger,
	client api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config,
) (*TargetManager, error) {
	level.Warn(logger).Log("msg", "WARNING!!! Windows target was configured but support for reading the windows event is not compiled into this build of promtail!")
	return &TargetManager{}, nil
}

// Ready returns true if at least one Windows target is also ready.
func (tm *TargetManager) Ready() bool { return false }

// Stop stops the Windows target manager and all of its targets.
func (tm *TargetManager) Stop() {}

// ActiveTargets returns the list of actuve Windows targets.
func (tm *TargetManager) ActiveTargets() map[string][]target.Target { return nil }

// AllTargets returns the list of all targets.
func (tm *TargetManager) AllTargets() map[string][]target.Target { return nil }
