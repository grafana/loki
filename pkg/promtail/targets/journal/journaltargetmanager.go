// +build !linux !cgo

package journal

import (
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/targets/target"
)

// JournalTargetManager manages a series of JournalTargets.
// nolint:golint
type JournalTargetManager struct{}

// NewJournalTargetManager returns nil as JournalTargets are not supported
// on this platform.
func NewJournalTargetManager(
	logger log.Logger,
	positions positions.Positions,
	client api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config,
) (*JournalTargetManager, error) {
	level.Warn(logger).Log("msg", "WARNING!!! Journal target was configured but support for reading the systemd journal is not compiled into this build of promtail!")
	return &JournalTargetManager{}, nil
}

// Ready always returns false for JournalTargetManager on non-Linux
// platforms.
func (tm *JournalTargetManager) Ready() bool {
	return false
}

// Stop is a no-op on non-Linux platforms.
func (tm *JournalTargetManager) Stop() {}

// ActiveTargets always returns nil on non-Linux platforms.
func (tm *JournalTargetManager) ActiveTargets() map[string][]target.Target {
	return nil
}

// AllTargets always returns nil on non-Linux platforms.
func (tm *JournalTargetManager) AllTargets() map[string][]target.Target {
	return nil
}
