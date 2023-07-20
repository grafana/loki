package ring

import "context"

type replicationSetResultTracker interface {
	// Signals an instance has done the execution, either successful (no error)
	// or failed (with error).
	done(instance *InstanceDesc, err error)

	// Returns true if the minimum number of successful results have been received.
	succeeded() bool

	// Returns true if the maximum number of failed executions have been reached.
	failed() bool

	// Returns true if the result returned by instance is part of the minimal set of all results
	// required to meet the quorum requirements of this tracker.
	// This method should only be called for instances that have returned a successful result,
	// calling this method for an instance that returned an error may return unpredictable results.
	// This method should only be called after succeeded returns true for the first time and before
	// calling done any further times.
	shouldIncludeResultFrom(instance *InstanceDesc) bool
}

type replicationSetContextTracker interface {
	// Returns a context.Context for instance.
	contextFor(instance *InstanceDesc) context.Context

	// Cancels the context for instance previously obtained with contextFor.
	// This method may cancel the context for other instances if those other instances are part of
	// the same zone and this tracker is zone-aware.
	cancelContextFor(instance *InstanceDesc)

	// Cancels all contexts previously obtained with contextFor.
	cancelAllContexts()
}

type defaultResultTracker struct {
	minSucceeded int
	numSucceeded int
	numErrors    int
	maxErrors    int
}

func newDefaultResultTracker(instances []InstanceDesc, maxErrors int) *defaultResultTracker {
	return &defaultResultTracker{
		minSucceeded: len(instances) - maxErrors,
		numSucceeded: 0,
		numErrors:    0,
		maxErrors:    maxErrors,
	}
}

func (t *defaultResultTracker) done(_ *InstanceDesc, err error) {
	if err == nil {
		t.numSucceeded++
	} else {
		t.numErrors++
	}
}

func (t *defaultResultTracker) succeeded() bool {
	return t.numSucceeded >= t.minSucceeded
}

func (t *defaultResultTracker) failed() bool {
	return t.numErrors > t.maxErrors
}

func (t *defaultResultTracker) shouldIncludeResultFrom(_ *InstanceDesc) bool {
	return true
}

type defaultContextTracker struct {
	ctx         context.Context
	cancelFuncs map[*InstanceDesc]context.CancelFunc
}

func newDefaultContextTracker(ctx context.Context, instances []InstanceDesc) *defaultContextTracker {
	return &defaultContextTracker{
		ctx:         ctx,
		cancelFuncs: make(map[*InstanceDesc]context.CancelFunc, len(instances)),
	}
}

func (t *defaultContextTracker) contextFor(instance *InstanceDesc) context.Context {
	ctx, cancel := context.WithCancel(t.ctx)
	t.cancelFuncs[instance] = cancel
	return ctx
}

func (t *defaultContextTracker) cancelContextFor(instance *InstanceDesc) {
	if cancel, ok := t.cancelFuncs[instance]; ok {
		cancel()
		delete(t.cancelFuncs, instance)
	}
}

func (t *defaultContextTracker) cancelAllContexts() {
	for instance, cancel := range t.cancelFuncs {
		cancel()
		delete(t.cancelFuncs, instance)
	}
}

// zoneAwareResultTracker tracks the results per zone.
// All instances in a zone must succeed in order for the zone to succeed.
type zoneAwareResultTracker struct {
	waitingByZone       map[string]int
	failuresByZone      map[string]int
	minSuccessfulZones  int
	maxUnavailableZones int
}

func newZoneAwareResultTracker(instances []InstanceDesc, maxUnavailableZones int) *zoneAwareResultTracker {
	t := &zoneAwareResultTracker{
		waitingByZone:       make(map[string]int),
		failuresByZone:      make(map[string]int),
		maxUnavailableZones: maxUnavailableZones,
	}

	for _, instance := range instances {
		t.waitingByZone[instance.Zone]++
	}

	t.minSuccessfulZones = len(t.waitingByZone) - maxUnavailableZones

	return t
}

func (t *zoneAwareResultTracker) done(instance *InstanceDesc, err error) {
	t.waitingByZone[instance.Zone]--

	if err != nil {
		t.failuresByZone[instance.Zone]++
	}
}

func (t *zoneAwareResultTracker) succeeded() bool {
	successfulZones := 0

	// The execution succeeded once we successfully received a successful result
	// from "all zones - max unavailable zones".
	for zone, numWaiting := range t.waitingByZone {
		if numWaiting == 0 && t.failuresByZone[zone] == 0 {
			successfulZones++
		}
	}

	return successfulZones >= t.minSuccessfulZones
}

func (t *zoneAwareResultTracker) failed() bool {
	failedZones := len(t.failuresByZone)
	return failedZones > t.maxUnavailableZones
}

func (t *zoneAwareResultTracker) shouldIncludeResultFrom(instance *InstanceDesc) bool {
	return t.failuresByZone[instance.Zone] == 0 && t.waitingByZone[instance.Zone] == 0
}

type zoneAwareContextTracker struct {
	contexts    map[string]context.Context
	cancelFuncs map[string]context.CancelFunc
}

func newZoneAwareContextTracker(ctx context.Context, instances []InstanceDesc) *zoneAwareContextTracker {
	t := &zoneAwareContextTracker{
		contexts:    map[string]context.Context{},
		cancelFuncs: map[string]context.CancelFunc{},
	}

	for _, instance := range instances {
		if _, ok := t.contexts[instance.Zone]; !ok {
			zoneCtx, cancel := context.WithCancel(ctx)
			t.contexts[instance.Zone] = zoneCtx
			t.cancelFuncs[instance.Zone] = cancel
		}
	}

	return t
}

func (t *zoneAwareContextTracker) contextFor(instance *InstanceDesc) context.Context {
	return t.contexts[instance.Zone]
}

func (t *zoneAwareContextTracker) cancelContextFor(instance *InstanceDesc) {
	if cancel, ok := t.cancelFuncs[instance.Zone]; ok {
		cancel()
		delete(t.cancelFuncs, instance.Zone)
	}
}

func (t *zoneAwareContextTracker) cancelAllContexts() {
	for zone, cancel := range t.cancelFuncs {
		cancel()
		delete(t.cancelFuncs, zone)
	}
}
