//go:build !linux || !cgo || !promtail_journal_enabled
// +build !linux !cgo !promtail_journal_enabled

package journal

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"
)

// JournalTargetManager manages a series of JournalTargets.
// nolint:revive
type JournalTargetManager struct{}

// NewJournalTargetManager returns nil as JournalTargets are not supported
// on this platform.
func NewJournalTargetManager(
	_ *Metrics,
	logger log.Logger,
	_ positions.Positions,
	_ api.EntryHandler,
	_ []scrapeconfig.Config,
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
