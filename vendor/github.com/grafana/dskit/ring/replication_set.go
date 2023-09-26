package ring

import (
	"context"
	"errors"
	"sort"
	"time"

	kitlog "github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/dskit/spanlogger"
)

// ReplicationSet describes the instances to talk to for a given key, and how
// many errors to tolerate.
type ReplicationSet struct {
	Instances []InstanceDesc

	// Maximum number of tolerated failing instances. Max errors and max unavailable zones are
	// mutually exclusive.
	MaxErrors int

	// Maximum number of different zones in which instances can fail. Max unavailable zones and
	// max errors are mutually exclusive.
	MaxUnavailableZones int
}

// Do function f in parallel for all replicas in the set, erroring if we exceed
// MaxErrors and returning early otherwise.
// Return a slice of all results from f, or nil if an error occurred.
func (r ReplicationSet) Do(ctx context.Context, delay time.Duration, f func(context.Context, *InstanceDesc) (interface{}, error)) ([]interface{}, error) {
	// Initialise the result tracker, which is use to keep track of successes and failures.
	var tracker replicationSetResultTracker
	if r.MaxUnavailableZones > 0 {
		tracker = newZoneAwareResultTracker(r.Instances, r.MaxUnavailableZones, kitlog.NewNopLogger())
	} else {
		tracker = newDefaultResultTracker(r.Instances, r.MaxErrors, kitlog.NewNopLogger())
	}

	var (
		ch         = make(chan instanceResult[any], len(r.Instances))
		forceStart = make(chan struct{}, r.MaxErrors)
	)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Spawn a goroutine for each instance.
	for i := range r.Instances {
		go func(i int, ing *InstanceDesc) {
			// Wait to send extra requests. Works only when zone-awareness is disabled.
			if delay > 0 && r.MaxUnavailableZones == 0 && i >= len(r.Instances)-r.MaxErrors {
				after := time.NewTimer(delay)
				defer after.Stop()
				select {
				case <-ctx.Done():
					return
				case <-forceStart:
				case <-after.C:
				}
			}
			result, err := f(ctx, ing)
			ch <- instanceResult[any]{
				result:   result,
				err:      err,
				instance: ing,
			}
		}(i, &r.Instances[i])
	}

	results := make([]interface{}, 0, len(r.Instances))

	for !tracker.succeeded() {
		select {
		case res := <-ch:
			tracker.done(res.instance, res.err)
			if res.err != nil {
				if tracker.failed() {
					return nil, res.err
				}

				// force one of the delayed requests to start
				if delay > 0 && r.MaxUnavailableZones == 0 {
					forceStart <- struct{}{}
				}
			} else {
				results = append(results, res.result)
			}

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return results, nil
}

type DoUntilQuorumConfig struct {
	// If true, enable request minimization.
	// See docs for DoUntilQuorum for more information.
	MinimizeRequests bool

	// If non-zero and MinimizeRequests is true, enables hedging.
	// See docs for DoUntilQuorum for more information.
	HedgingDelay time.Duration

	// If non-nil, DoUntilQuorum will emit log lines and span events during the call.
	Logger *spanlogger.SpanLogger
}

func (c DoUntilQuorumConfig) Validate() error {
	if c.HedgingDelay < 0 {
		return errors.New("invalid DoUntilQuorumConfig: HedgingDelay must be non-negative")
	}

	return nil
}

// DoUntilQuorum runs function f in parallel for all replicas in r.
//
// # Result selection
//
// If r.MaxUnavailableZones is greater than zero, DoUntilQuorum operates in zone-aware mode:
//   - DoUntilQuorum returns an error if calls to f for instances in more than r.MaxUnavailableZones zones return errors
//   - Otherwise, DoUntilQuorum returns all results from all replicas in the first zones for which f succeeds
//     for every instance in that zone (eg. if there are 3 zones and r.MaxUnavailableZones is 1, DoUntilQuorum will
//     return the results from all instances in 2 zones, even if all calls to f succeed).
//
// Otherwise, DoUntilQuorum operates in non-zone-aware mode:
//   - DoUntilQuorum returns an error if more than r.MaxErrors calls to f return errors
//   - Otherwise, DoUntilQuorum returns all results from the first len(r.Instances) - r.MaxErrors instances
//     (eg. if there are 6 replicas and r.MaxErrors is 2, DoUntilQuorum will return the results from the first 4
//     successful calls to f, even if all 6 calls to f succeed).
//
// # Request minimization
//
// cfg.MinimizeRequests enables or disables request minimization.
//
// Regardless of the value of cfg.MinimizeRequests, if one of the termination conditions above is satisfied or ctx is
// cancelled before f is called for an instance, f may not be called for that instance at all.
//
// ## When disabled
//
// If request minimization is disabled, DoUntilQuorum will call f for each instance in r. The value of cfg.HedgingDelay
// is ignored.
//
// ## When enabled
//
// If request minimization is enabled, DoUntilQuorum will initially call f for the minimum number of instances needed to
// reach the termination conditions above, and later call f for further instances if required. For example, if
// r.MaxUnavailableZones is 1 and there are three zones, DoUntilQuorum will initially only call f for instances in two
// zones, and only call f for instances in the remaining zone if a request in the initial two zones fails.
//
// DoUntilQuorum will randomly select available zones / instances such that calling DoUntilQuorum multiple times with
// the same ReplicationSet should evenly distribute requests across all zones / instances.
//
// If cfg.HedgingDelay is non-zero, DoUntilQuorum will call f for an additional zone's instances (if zone-aware) / an
// additional instance (if not zone-aware) every cfg.HedgingDelay until one of the termination conditions above is
// reached. For example, if r.MaxUnavailableZones is 2, cfg.HedgingDelay is 4 seconds and there are fives zones,
// DoUntilQuorum will initially only call f for instances in three zones, and unless one of the termination conditions
// is reached earlier, will then call f for instances in a fourth zone approximately 4 seconds later, and then call f
// for instances in the final zone approximately 4 seconds after that (ie. roughly 8 seconds since the call to
// DoUntilQuorum began).
//
// # Cleanup
//
// Any results from successful calls to f that are not returned by DoUntilQuorum will be passed to cleanupFunc,
// including when DoUntilQuorum returns an error or only returns a subset of successful results. cleanupFunc may
// be called both before and after DoUntilQuorum returns.
//
// A call to f is considered successful if it returns a nil error.
//
// # Contexts
//
// The context.Context passed to an invocation of f may be cancelled at any time if the result of that invocation of
// f will not be used.
//
// DoUntilQuorum cancels the context.Context passed to each invocation of f before DoUntilQuorum returns.
func DoUntilQuorum[T any](ctx context.Context, r ReplicationSet, cfg DoUntilQuorumConfig, f func(context.Context, *InstanceDesc) (T, error), cleanupFunc func(T)) ([]T, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wrappedF := func(ctx context.Context, desc *InstanceDesc, _ context.CancelFunc) (T, error) {
		return f(ctx, desc)
	}

	return DoUntilQuorumWithoutSuccessfulContextCancellation(ctx, r, cfg, wrappedF, cleanupFunc)
}

// DoUntilQuorumWithoutSuccessfulContextCancellation behaves the same as DoUntilQuorum, except it does not cancel
// the context.Context passed to invocations of f whose results are returned.
//
// For example, this is useful in situations where DoUntilQuorumWithoutSuccessfulContextCancellation is used
// to establish a set of streams that will be used after DoUntilQuorumWithoutSuccessfulContextCancellation returns.
//
// It is the caller's responsibility to ensure that either of the following are eventually true:
//   - ctx is cancelled, or
//   - the corresponding context.CancelFunc is called for all invocations of f whose results are returned by
//     DoUntilQuorumWithoutSuccessfulContextCancellation
//
// Failing to do this may result in a memory leak.
func DoUntilQuorumWithoutSuccessfulContextCancellation[T any](ctx context.Context, r ReplicationSet, cfg DoUntilQuorumConfig, f func(context.Context, *InstanceDesc, context.CancelFunc) (T, error), cleanupFunc func(T)) ([]T, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	var logger kitlog.Logger = cfg.Logger
	if cfg.Logger == nil {
		logger = kitlog.NewNopLogger()
	}

	resultsChan := make(chan instanceResult[T], len(r.Instances))
	resultsRemaining := len(r.Instances)

	defer func() {
		go func() {
			for resultsRemaining > 0 {
				result := <-resultsChan
				resultsRemaining--

				if result.err == nil {
					cleanupFunc(result.result)
				}
			}
		}()
	}()

	var resultTracker replicationSetResultTracker
	var contextTracker replicationSetContextTracker
	if r.MaxUnavailableZones > 0 {
		resultTracker = newZoneAwareResultTracker(r.Instances, r.MaxUnavailableZones, logger)
		contextTracker = newZoneAwareContextTracker(ctx, r.Instances)
	} else {
		resultTracker = newDefaultResultTracker(r.Instances, r.MaxErrors, logger)
		contextTracker = newDefaultContextTracker(ctx, r.Instances)
	}

	if cfg.MinimizeRequests {
		resultTracker.startMinimumRequests()
	} else {
		resultTracker.startAllRequests()
	}

	for i := range r.Instances {
		instance := &r.Instances[i]
		ctx, ctxCancel := contextTracker.contextFor(instance)

		go func(desc *InstanceDesc) {
			if err := resultTracker.awaitStart(ctx, desc); err != nil {
				// Post to resultsChan so that the deferred cleanup handler above eventually terminates.
				resultsChan <- instanceResult[T]{
					err:      err,
					instance: desc,
				}

				return
			}

			result, err := f(ctx, desc, ctxCancel)
			resultsChan <- instanceResult[T]{
				result:   result,
				err:      err,
				instance: desc,
			}
		}(instance)
	}

	resultsMap := make(map[*InstanceDesc]T, len(r.Instances))
	cleanupResultsAlreadyReceived := func() {
		for _, result := range resultsMap {
			cleanupFunc(result)
		}
	}

	var hedgingTrigger <-chan time.Time

	if cfg.HedgingDelay > 0 {
		ticker := time.NewTicker(cfg.HedgingDelay)
		defer ticker.Stop()
		hedgingTrigger = ticker.C
	}

	for !resultTracker.succeeded() {
		select {
		case <-ctx.Done():
			level.Debug(logger).Log("msg", "parent context done, returning", "err", ctx.Err())

			// No need to cancel individual instance contexts, as they inherit the cancellation from ctx.
			cleanupResultsAlreadyReceived()

			return nil, ctx.Err()
		case <-hedgingTrigger:
			resultTracker.startAdditionalRequests()
		case result := <-resultsChan:
			resultsRemaining--
			resultTracker.done(result.instance, result.err)

			if result.err == nil {
				resultsMap[result.instance] = result.result
			} else {
				contextTracker.cancelContextFor(result.instance)

				if resultTracker.failed() {
					level.Error(logger).Log("msg", "cancelling all requests because quorum cannot be reached")

					if cfg.Logger != nil {
						_ = cfg.Logger.Error(result.err)
					}

					contextTracker.cancelAllContexts()
					cleanupResultsAlreadyReceived()
					return nil, result.err
				}
			}
		}
	}

	level.Debug(logger).Log("msg", "quorum reached")

	results := make([]T, 0, len(r.Instances))

	for i := range r.Instances {
		instance := &r.Instances[i]
		result, haveResult := resultsMap[instance]

		if haveResult {
			if resultTracker.shouldIncludeResultFrom(instance) {
				results = append(results, result)
			} else {
				contextTracker.cancelContextFor(instance)
				cleanupFunc(result)
			}
		} else {
			// Nothing to clean up (yet) - this will be handled by deferred call above.
			contextTracker.cancelContextFor(instance)
		}
	}

	return results, nil
}

type instanceResult[T any] struct {
	result   T
	err      error
	instance *InstanceDesc
}

// Includes returns whether the replication set includes the replica with the provided addr.
func (r ReplicationSet) Includes(addr string) bool {
	for _, instance := range r.Instances {
		if instance.GetAddr() == addr {
			return true
		}
	}

	return false
}

// GetAddresses returns the addresses of all instances within the replication set. Returned slice
// order is not guaranteed.
func (r ReplicationSet) GetAddresses() []string {
	addrs := make([]string, 0, len(r.Instances))
	for _, desc := range r.Instances {
		addrs = append(addrs, desc.Addr)
	}
	return addrs
}

// GetAddressesWithout returns the addresses of all instances within the replication set while
// excluding the specified address. Returned slice order is not guaranteed.
func (r ReplicationSet) GetAddressesWithout(exclude string) []string {
	addrs := make([]string, 0, len(r.Instances))
	for _, desc := range r.Instances {
		if desc.Addr != exclude {
			addrs = append(addrs, desc.Addr)
		}
	}
	return addrs
}

// ZoneCount returns the number of unique zones represented by instances within this replication set.
func (r *ReplicationSet) ZoneCount() int {
	// Why not use a map here? Using a slice is faster for the small number of zones we expect to typically use.
	zones := []string{}

	for _, i := range r.Instances {
		sawZone := false

		for _, z := range zones {
			if z == i.Zone {
				sawZone = true
				break
			}
		}

		if !sawZone {
			zones = append(zones, i.Zone)
		}
	}

	return len(zones)
}

// HasReplicationSetChanged returns false if two replications sets are the same (with possibly different timestamps),
// true if they differ in any way (number of instances, instance states, tokens, zones, ...).
func HasReplicationSetChanged(before, after ReplicationSet) bool {
	return hasReplicationSetChangedExcluding(before, after, func(i *InstanceDesc) {
		i.Timestamp = 0
	})
}

// HasReplicationSetChangedWithoutState returns false if two replications sets
// are the same (with possibly different timestamps and instance states),
// true if they differ in any other way (number of instances, tokens, zones, ...).
func HasReplicationSetChangedWithoutState(before, after ReplicationSet) bool {
	return hasReplicationSetChangedExcluding(before, after, func(i *InstanceDesc) {
		i.Timestamp = 0
		i.State = PENDING
	})
}

// Do comparison of replicasets, but apply a function first
// to be able to exclude (reset) some values
func hasReplicationSetChangedExcluding(before, after ReplicationSet, exclude func(*InstanceDesc)) bool {
	beforeInstances := before.Instances
	afterInstances := after.Instances

	if len(beforeInstances) != len(afterInstances) {
		return true
	}

	sort.Sort(ByAddr(beforeInstances))
	sort.Sort(ByAddr(afterInstances))

	for i := 0; i < len(beforeInstances); i++ {
		b := beforeInstances[i]
		a := afterInstances[i]

		exclude(&a)
		exclude(&b)

		if !b.Equal(a) {
			return true
		}
	}

	return false
}
