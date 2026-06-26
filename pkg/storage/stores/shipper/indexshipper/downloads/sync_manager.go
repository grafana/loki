package downloads

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	indexstore "github.com/grafana/loki/v3/pkg/storage/stores/index"
)

// syncManager serializes index syncs and tracks their status. It guarantees at
// most one sync (periodic or manual) runs at a time and records the timing the
// /sync-indexes status endpoint reports. The actual sync work is injected by the
// owner (see newSyncManager), so syncManager is agnostic to what a sync does.
type syncManager struct {
	logger log.Logger
	// sync performs one sync pass for the given trigger. Injected by the owner;
	// on a manual trigger it also refreshes the object-listing cache first.
	sync func(ctx context.Context, trigger string) error

	// runMtx is held for the duration of a sync; TryLock on it serializes the
	// periodic loop and a manual trigger so at most one sync runs at a time.
	runMtx sync.Mutex

	// statusMtx guards the status bookkeeping below.
	statusMtx    sync.Mutex
	inProgress   bool
	startedAt    time.Time
	lastDuration time.Duration
	lastTrigger  string

	// wg tracks in-flight asynchronous (manual) syncs so the owner's Stop can
	// drain them.
	wg sync.WaitGroup
}

func newSyncManager(logger log.Logger, sync func(ctx context.Context, trigger string) error) *syncManager {
	return &syncManager{
		logger: logger,
		sync:   sync,
	}
}

// RunPeriodic runs a periodic sync unless one is already in progress, returning
// any error from the sync (nil if it was skipped). It is meant to be called from
// the owner's loop on each tick, which logs the error.
func (s *syncManager) RunPeriodic(ctx context.Context) error {
	// Serialize with any in-flight manual sync so we never run two syncs
	// concurrently, which would redundantly download the same files.
	if !s.runMtx.TryLock() {
		level.Info(s.logger).Log("msg", "skipping index sync, one is already in progress", "trigger", syncTriggerPeriodic)
		return nil
	}
	defer s.runMtx.Unlock()

	return s.run(ctx, syncTriggerPeriodic, nil)
}

// TriggerManual starts an asynchronous manual sync if none is already in
// progress, returning true if a new sync was started. It returns only once the
// sync has been marked in progress, so a Status call right after a true return
// observes the running sync rather than racing the goroutine's startup
// (markStarted runs on the spawned goroutine, not the caller's). The sync runs on
// ctx so the owner's Stop drains it via Wait.
func (s *syncManager) TriggerManual(ctx context.Context) bool {
	// Claiming runMtx synchronously here means a concurrent trigger (or the next
	// periodic tick) immediately sees a sync in progress and backs off. The
	// spawned goroutine runs the sync and releases the lock (a sync.Mutex may be
	// unlocked by a different goroutine than the one that locked it).
	if !s.runMtx.TryLock() {
		return false
	}

	level.Info(s.logger).Log("msg", "manual index sync triggered")
	// started is closed by run once the sync is marked in progress; waiting on it
	// before returning makes the in-progress status visible to the caller without
	// racing the goroutine below (which is where markStarted actually runs).
	started := make(chan struct{})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.runMtx.Unlock()
		if err := s.run(ctx, syncTriggerManual, started); err != nil {
			level.Error(s.logger).Log("msg", "manual index sync failed", "err", err)
		}
	}()
	<-started
	return true
}

// Status reports the current/last sync status.
func (s *syncManager) Status() indexstore.SyncStatus {
	s.statusMtx.Lock()
	defer s.statusMtx.Unlock()

	status := indexstore.SyncStatus{
		InProgress:   s.inProgress,
		LastDuration: s.lastDuration,
		LastTrigger:  s.lastTrigger,
	}
	if s.inProgress {
		status.CurrentDuration = time.Since(s.startedAt)
	}
	return status
}

// Wait blocks until any in-flight asynchronous (manual) sync has finished.
func (s *syncManager) Wait() {
	s.wg.Wait()
}

// run executes one sync pass under runMtx (held by the caller): it marks the sync
// started, runs the injected work, then (deferred) records the duration and logs.
// If started is non-nil it is closed right after the sync is marked in progress,
// letting an asynchronous caller (TriggerManual) return only once Status reflects
// the running sync.
func (s *syncManager) run(ctx context.Context, trigger string, started chan struct{}) error {
	s.markStarted(trigger)
	if started != nil {
		close(started)
	}
	defer s.markFinished()
	return s.sync(ctx, trigger)
}

func (s *syncManager) markStarted(trigger string) {
	s.statusMtx.Lock()
	defer s.statusMtx.Unlock()
	s.inProgress = true
	s.startedAt = time.Now()
	s.lastTrigger = trigger
}

func (s *syncManager) markFinished() {
	s.statusMtx.Lock()
	duration := time.Since(s.startedAt)
	s.inProgress = false
	s.lastDuration = duration
	trigger := s.lastTrigger
	s.statusMtx.Unlock()

	level.Info(s.logger).Log("msg", "index sync finished", "trigger", trigger, "duration", duration)
}
