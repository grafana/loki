package compactor

import (
	"context"
	"fmt"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/deletion"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// sweepScheduler dispatches one deletion sweep per enabled tenant every hour.
// The minute of the sweep comes from the tenant ID. This spreads the sweeps
// of many tenants over the hour.
//
// A failed sweep does not run again until the next hour. The worker skips
// the scan if the tenant's file list is recent, so most hourly sweeps are
// cheap.
//
// sweepScheduler is safe for concurrent use.
type sweepScheduler struct {
	logger log.Logger
	// dataBucket is the root of the data object storage.
	dataBucket objstore.BucketReader
	limits     Limits
	dispatcher *planDispatcher
	// slots holds one token for each sweep that runs. Its capacity caps the
	// sweeps that run at the same time, across all tenants.
	slots chan struct{}
	clock func() time.Time
	sleep func(ctx context.Context, d time.Duration)
}

// newSweepScheduler returns a sweepScheduler that runs plans with run. It
// counts sweep attempts in attempts, with the actor "deletion-sweep".
func newSweepScheduler(
	cfg DeletionSweepConfig,
	logger log.Logger,
	dataBucket objstore.BucketReader,
	limits Limits,
	run runFunc,
	attempts *prometheus.CounterVec,
) *sweepScheduler {
	return &sweepScheduler{
		logger:     logger,
		dataBucket: dataBucket,
		limits:     limits,
		// The next hourly sweep is the retry, so the dispatcher does not
		// retry.
		dispatcher: newPlanDispatcher(logger, run, 1, 0, backoff.Config{}, attempts),
		slots:      make(chan struct{}, cfg.MaxConcurrentSweeps),
		clock:      time.Now,
		sleep:      sleepUntil,
	}
}

// supervisor returns a tenantsSupervisor that keeps one sweep loop running
// for each discovered tenant with the sweep enabled. It discovers tenants
// every pollInterval.
func (s *sweepScheduler) supervisor(pollInterval time.Duration) *tenantsSupervisor {
	return newTenantsSupervisor(s.logger, pollInterval, s.discover, s.enabled, s.runTenant)
}

// discover returns the tenants with compacted objects. Compaction runs for
// every tenant, so a tenant with data has compacted objects soon after its
// first write. The set also keeps a tenant that stopped writing data.
func (s *sweepScheduler) discover(ctx context.Context) (map[string]struct{}, error) {
	tenants, err := deletion.ListCompactedTenants(ctx, s.dataBucket, s.logger)
	if err != nil {
		return nil, fmt.Errorf("list tenants with compacted objects: %w", err)
	}
	return tenants, nil
}

func (s *sweepScheduler) enabled(tenant string) bool {
	return s.limits.DeletionSweepEnabled(tenant)
}

// runTenant sweeps tenant at its minute of every hour until ctx is cancelled.
// It waits for each sweep to finish, so sweeps of one tenant never overlap.
// A sweep that ends after the next slot moves to the slot after it.
func (s *sweepScheduler) runTenant(ctx context.Context, tenant string) {
	minute := sweepMinute(tenant)
	for {
		now := s.clock()
		s.sleep(ctx, nextSweepTime(now, minute).Sub(now))
		if ctx.Err() != nil {
			return
		}
		s.sweep(ctx, tenant)
	}
}

func (s *sweepScheduler) sweep(ctx context.Context, tenant string) {
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-s.slots }()

	plan := buildDeletionSweepPlan(tenant)
	if _, err := s.dispatcher.Run(ctx, tenant, []*physical.Plan{plan}); err != nil {
		level.Warn(s.logger).Log("msg", "deletion sweep failed; the next one starts in the next hour", "tenant", tenant, "err", err)
	}
}

// sweepMinute returns the minute of the hour, from 0 to 59, at which tenant
// is swept. It does not change across restarts.
func sweepMinute(tenant string) int {
	return int(xxhash.Sum64String(tenant) % 60)
}

// nextSweepTime returns the first time after now at the given minute of an
// hour.
func nextSweepTime(now time.Time, minute int) time.Time {
	next := now.Truncate(time.Hour).Add(time.Duration(minute) * time.Minute)
	if !next.After(now) {
		next = next.Add(time.Hour)
	}
	return next
}
