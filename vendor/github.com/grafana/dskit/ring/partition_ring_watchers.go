package ring

import (
	"context"
	"fmt"

	"github.com/grafana/dskit/services"
)

// PartitionRingWatchers groups one or more PartitionRingWatcher and exposes them through a single
// service, indexed by their position in the group. It runs the watchers as subservices, so starting
// or stopping the group starts or stops all of them, and a failure in any watcher surfaces as a
// failure of the group.
//
// It is useful when a process watches several partition rings at once (for example one ring per
// shard, tenant group, or logical partition set) and wants to manage their lifecycle together.
type PartitionRingWatchers struct {
	services.Service

	// watchers is indexed by position in the group. It always has at least one element.
	watchers []*PartitionRingWatcher

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
}

// NewPartitionRingWatchers groups the given watchers into a single service. At least one watcher
// must be provided. The watchers are indexed by the order in which they are passed and must not be
// started by the caller: the returned service starts and stops them.
func NewPartitionRingWatchers(watchers ...*PartitionRingWatcher) (*PartitionRingWatchers, error) {
	if len(watchers) == 0 {
		return nil, fmt.Errorf("at least one partition ring watcher must be provided")
	}

	subservices := make([]services.Service, len(watchers))
	for i, w := range watchers {
		subservices[i] = w
	}
	manager, err := services.NewManager(subservices...)
	if err != nil {
		return nil, fmt.Errorf("creating partition ring watchers service manager: %w", err)
	}

	w := &PartitionRingWatchers{
		watchers:           watchers,
		subservices:        manager,
		subservicesWatcher: services.NewFailureWatcher(),
	}
	w.Service = services.NewBasicService(w.starting, w.running, w.stopping).WithName("partition-ring-watchers")
	return w, nil
}

// Count returns the number of watchers in the group.
func (w *PartitionRingWatchers) Count() int {
	return len(w.watchers)
}

// Watcher returns the watcher at the given index.
func (w *PartitionRingWatchers) Watcher(idx int) *PartitionRingWatcher {
	return w.watchers[idx]
}

// PartitionRing returns the partition ring of the watcher at the given index.
func (w *PartitionRingWatchers) PartitionRing(idx int) *PartitionRing {
	return w.watchers[idx].PartitionRing()
}

// All returns the watchers indexed by position in the group. The returned slice must not be modified.
func (w *PartitionRingWatchers) All() []*PartitionRingWatcher {
	return w.watchers
}

func (w *PartitionRingWatchers) starting(ctx context.Context) error {
	w.subservicesWatcher.WatchManager(w.subservices)
	return services.StartManagerAndAwaitHealthy(ctx, w.subservices)
}

func (w *PartitionRingWatchers) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-w.subservicesWatcher.Chan():
		return fmt.Errorf("partition ring watcher subservice failed: %w", err)
	}
}

func (w *PartitionRingWatchers) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), w.subservices)
}
