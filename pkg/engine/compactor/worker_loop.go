package compactor

import (
	"context"
	"errors"
	"time"

	"github.com/go-kit/log/level"
)

// phase is the current step of a tenant's flip-flop worker.
type phase int

const (
	phaseIndexMerge phase = iota
	phaseLogMerge
)

func (p phase) flip() phase {
	if p == phaseIndexMerge {
		return phaseLogMerge
	}
	return phaseIndexMerge
}

// phaseOutcome is the result of running one phase; it drives the flip-vs-retry
// decision.
type phaseOutcome int

const (
	phaseOutcomeError   phaseOutcome = iota // re-arm same phase
	phaseOutcomeNoWork                      // success, nothing to do; flip
	phaseOutcomeSwapped                     // ToC swap applied/observed; flip
)

// runIndexMergePhase runs IndexMerge for the tenant's current window and swaps
// the ToC.
func (c *coordinator) runIndexMergePhase(ctx context.Context, tenant string, window time.Time) phaseOutcome {
	start := c.clock()
	entries, ok := c.tenantEntries(ctx, tenant, window)
	if !ok {
		return phaseOutcomeError
	}

	c.metrics.observeEntries(tenant, entries, c.clock())

	if len(entries) <= 1 {
		c.metrics.observeTenantCycle(tenant, "converged", c.clock().Sub(start), compactionStats{})
		return phaseOutcomeNoWork
	}

	stats, err := c.compactTenant(ctx, tenant, window, entries)
	dur := c.clock().Sub(start)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return phaseOutcomeError
		}
		level.Warn(c.logger).Log("msg", "index-merge phase failed",
			"tenant", tenant, "window", window, "err", err)
		c.metrics.observeTenantCycle(tenant, "failed", dur, compactionStats{})
		return phaseOutcomeError
	}
	// compactTenant returns zero stats for every no-op success; a real swap sets
	// added > 0.
	if stats.added == 0 {
		c.metrics.observeTenantCycle(tenant, "converged", dur, compactionStats{})
		return phaseOutcomeNoWork
	}
	c.metrics.observeTenantCycle(tenant, "compacted", dur, stats)
	return phaseOutcomeSwapped
}

// tenantEntries reads the current-window ToC and returns the tenant's entries.
// A missing ToC yields (nil, true) — no work, not an error. Any other read
// error yields (nil, false).
func (c *coordinator) tenantEntries(ctx context.Context, tenant string, window time.Time) ([]indexEntry, bool) {
	indexes, err := loadTenantIndexes(ctx, c.bucket, window)
	if err != nil {
		if c.bucket.IsObjNotFoundErr(err) {
			level.Debug(c.logger).Log("msg", "bucket not found",
				"tenant", tenant, "window", window, "bucket", c.bucket.Name(), "err", err)
			return nil, true
		}
		level.Warn(c.logger).Log("msg", "phase: load tenant indexes failed",
			"tenant", tenant, "window", window, "err", err)
		return nil, false
	}
	return indexes[tenant], true
}

// runLogMergePhase schedules one LogMerge task per index file for the [tenant]
// in the current [window]. Any error retries. Retries are safe because swapping
// an index that already swapped is a no-op. Context cancellation is not an
// error.
func (c *coordinator) runLogMergePhase(ctx context.Context, tenant string, window time.Time) phaseOutcome {
	start := c.clock()
	entries, ok := c.tenantEntries(ctx, tenant, window)
	if !ok {
		return phaseOutcomeError
	}
	if len(entries) == 0 {
		c.metrics.observeTenantLogCycle(tenant, "converged", c.clock().Sub(start), compactionStats{})
		return phaseOutcomeNoWork
	}

	var agg compactionStats
	anySwapped := false
	anyError := false
	for _, entry := range entries {
		if ctx.Err() != nil {
			return phaseOutcomeError
		}
		_, stats, err := c.compactTenantLogs(ctx, tenant, window, entry)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return phaseOutcomeError
			}
			level.Warn(c.logger).Log("msg", "log-merge phase: index failed",
				"tenant", tenant, "window", window, "index", entry.Path, "err", err)
			anyError = true
			continue
		}
		if stats.added > 0 {
			anySwapped = true
			agg.removed += stats.removed
			agg.added += stats.added
			agg.dispatched += stats.dispatched
		}
	}

	dur := c.clock().Sub(start)
	switch {
	case anyError:
		c.metrics.observeTenantLogCycle(tenant, "failed", dur, compactionStats{})
		return phaseOutcomeError
	case anySwapped:
		c.metrics.observeTenantLogCycle(tenant, "compacted", dur, agg)
		return phaseOutcomeSwapped
	default:
		c.metrics.observeTenantLogCycle(tenant, "converged", dur, compactionStats{})
		return phaseOutcomeNoWork
	}
}
