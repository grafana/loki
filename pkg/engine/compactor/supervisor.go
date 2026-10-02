package compactor

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

// tenantsSupervisor keeps one worker goroutine running for each tenant that has
// compaction work and is enabled for compaction.
//
// A tenantsSupervisor is single use. Run owns runningWorkers and wg, so they need no
// locking.
type tenantsSupervisor struct {
	logger       log.Logger
	pollInterval time.Duration

	// discover returns the tenants that have work. It returns an error when it
	// cannot read the complete set.
	discover func(ctx context.Context) (map[string]struct{}, error)
	// enabled reports whether tenant may run any compaction phase.
	enabled func(tenant string) bool
	// runTenant runs one tenant's worker until ctx is cancelled.
	runTenant func(ctx context.Context, tenant string)

	runningWorkers map[string]context.CancelFunc
	wg             sync.WaitGroup
}

func newTenantsSupervisor(
	logger log.Logger,
	pollInterval time.Duration,
	discover func(ctx context.Context) (map[string]struct{}, error),
	enabled func(tenant string) bool,
	runTenant func(ctx context.Context, tenant string),
) *tenantsSupervisor {
	return &tenantsSupervisor{
		logger:         logger,
		pollInterval:   pollInterval,
		discover:       discover,
		enabled:        enabled,
		runTenant:      runTenant,
		runningWorkers: make(map[string]context.CancelFunc),
	}
}

// Run reconciles the workers once, then again every pollInterval, until ctx
// is cancelled. It then stops every worker, waits for them to exit, and
// returns ctx.Err().
func (s *tenantsSupervisor) Run(ctx context.Context) error {
	s.reconcile(ctx)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.stopAll()
			return ctx.Err()
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

// reconcile starts and stops runningWorkers to match the current discovery result.
// When discovery fails, reconcile changes nothing and waits for the next pass.
//
// A stopped worker deletes its per-tenant metric series when its goroutine
// exits, not here, so a draining worker cannot recreate a series after stop.
func (s *tenantsSupervisor) reconcile(ctx context.Context) {
	discovered, err := s.discover(ctx)
	if err != nil {
		// A missing ToC is expected after every window boundary, so it is not
		// worth a warning.
		lvl := level.Warn
		if errors.Is(err, errNoToC) {
			lvl = level.Debug
		}
		lvl(s.logger).Log("msg", "tenant discovery failed; leaving workers unchanged", "err", err)
		return
	}
	start, stop := reconcileWorkers(s.running(), discovered, s.enabled)
	for _, tenant := range stop {
		s.stop(tenant)
	}
	for _, tenant := range start {
		s.start(ctx, tenant)
	}
}

func (s *tenantsSupervisor) running() map[string]struct{} {
	out := make(map[string]struct{}, len(s.runningWorkers))
	for tenant := range s.runningWorkers {
		out[tenant] = struct{}{}
	}
	return out
}

func (s *tenantsSupervisor) start(ctx context.Context, tenant string) {
	level.Debug(s.logger).Log("msg", "starting compaction worker", "tenant", tenant)
	wctx, cancel := context.WithCancel(ctx)
	s.runningWorkers[tenant] = cancel
	s.wg.Go(func() { s.runTenant(wctx, tenant) })
}

func (s *tenantsSupervisor) stop(tenant string) {
	level.Debug(s.logger).Log("msg", "stopping compaction worker", "tenant", tenant)
	s.runningWorkers[tenant]()
	delete(s.runningWorkers, tenant)
}

func (s *tenantsSupervisor) stopAll() {
	for tenant := range s.runningWorkers {
		s.stop(tenant)
	}
	s.wg.Wait()
}

// reconcileWorkers returns the tenants to start and the tenants to stop, each
// sorted.
//
// It returns:
//   - start: discovered tenants that are enabled and not running.
//   - stop: running tenants that are disabled or absent from discovered.
//
// A tenant is never in both lists.
func reconcileWorkers(running, discovered map[string]struct{}, enabled func(string) bool) (start, stop []string) {
	for tenant := range running {
		_, present := discovered[tenant]
		if !enabled(tenant) || !present {
			stop = append(stop, tenant)
		}
	}
	for tenant := range discovered {
		if _, ok := running[tenant]; ok {
			continue
		}
		if enabled(tenant) {
			start = append(start, tenant)
		}
	}
	slices.Sort(start)
	slices.Sort(stop)
	return start, stop
}
