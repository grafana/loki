package ring

import (
	"context"
	"sort"
	"time"
)

// ReplicationSet describes the ingesters to talk to for a given key, and how
// many errors to tolerate.
type ReplicationSet struct {
	Ingesters []IngesterDesc

	// Maximum number of tolerated failing instances. Max errors and max unavailable zones are
	// mutually exclusive.
	MaxErrors int

	// Maximum number of different zones in which instances can fail. Max unavailable zones and
	// max errors are mutually exclusive.
	MaxUnavailableZones int
}

// Do function f in parallel for all replicas in the set, erroring is we exceed
// MaxErrors and returning early otherwise.
func (r ReplicationSet) Do(ctx context.Context, delay time.Duration, f func(context.Context, *IngesterDesc) (interface{}, error)) ([]interface{}, error) {
	type instanceResult struct {
		res      interface{}
		err      error
		instance *IngesterDesc
	}

	// Initialise the result tracker, which is use to keep track of successes and failures.
	var tracker replicationSetResultTracker
	if r.MaxUnavailableZones > 0 {
		tracker = newZoneAwareResultTracker(r.Ingesters, r.MaxUnavailableZones)
	} else {
		tracker = newDefaultResultTracker(r.Ingesters, r.MaxErrors)
	}

	var (
		ch         = make(chan instanceResult, len(r.Ingesters))
		forceStart = make(chan struct{}, r.MaxErrors)
	)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Spawn a goroutine for each instance.
	for i := range r.Ingesters {
		go func(i int, ing *IngesterDesc) {
			// Wait to send extra requests. Works only when zone-awareness is disabled.
			if delay > 0 && r.MaxUnavailableZones == 0 && i >= len(r.Ingesters)-r.MaxErrors {
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
		}(i, &r.Ingesters[i])
	}

	results := make([]interface{}, 0, len(r.Ingesters))

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
	for _, instance := range r.Ingesters {
		if instance.GetAddr() == addr {
			return true
		}
	}

	return false
}

// GetAddresses returns the addresses of all instances within the replication set. Returned slice
// order is not guaranteed.
func (r ReplicationSet) GetAddresses() []string {
	addrs := make([]string, 0, len(r.Ingesters))
	for _, desc := range r.Ingesters {
		addrs = append(addrs, desc.Addr)
	}
	return addrs
}

// HasReplicationSetChanged returns true if two replications sets are the same (with possibly different timestamps),
// false if they differ in any way (number of instances, instance states, tokens, zones, ...).
func HasReplicationSetChanged(before, after ReplicationSet) bool {
	beforeInstances := before.Ingesters
	afterInstances := after.Ingesters

	if len(beforeInstances) != len(afterInstances) {
		return true
	}

	sort.Sort(ByAddr(beforeInstances))
	sort.Sort(ByAddr(afterInstances))

	for i := 0; i < len(beforeInstances); i++ {
		b := beforeInstances[i]
		a := afterInstances[i]

		// Exclude the heartbeat timestamp from the comparison.
		b.Timestamp = 0
		a.Timestamp = 0

		if !b.Equal(a) {
			return true
		}
	}

	return false
}
