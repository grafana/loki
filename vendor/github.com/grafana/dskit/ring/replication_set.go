package ring

import (
	"context"
	"sort"
	"time"
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
	type instanceResult struct {
		res      interface{}
		err      error
		instance *InstanceDesc
	}

	// Initialise the result tracker, which is use to keep track of successes and failures.
	var tracker replicationSetResultTracker
	if r.MaxUnavailableZones > 0 {
		tracker = newZoneAwareResultTracker(r.Instances, r.MaxUnavailableZones)
	} else {
		tracker = newDefaultResultTracker(r.Instances, r.MaxErrors)
	}

	var (
		ch         = make(chan instanceResult, len(r.Instances))
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
			ch <- instanceResult{
				res:      result,
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
				results = append(results, res.res)
			}

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return results, nil
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

// HasReplicationSetChanged returns true if two replications sets are the same (with possibly different timestamps),
// false if they differ in any way (number of instances, instance states, tokens, zones, ...).
func HasReplicationSetChanged(before, after ReplicationSet) bool {
	return hasReplicationSetChangedExcluding(before, after, func(i *InstanceDesc) {
		i.Timestamp = 0
	})
}

// HasReplicationSetChangedWithoutState returns true if two replications sets
// are the same (with possibly different timestamps and instance states),
// false if they differ in any other way (number of instances, tokens, zones, ...).
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
