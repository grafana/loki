// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/pkg/ring/batch.go

package ring

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/atomic"
	"google.golang.org/grpc/status"
)

type batchTracker struct {
	rpcsPending atomic.Int32
	rpcsFailed  atomic.Int32
	done        chan struct{}
	err         chan error
}

type instance struct {
	desc         InstanceDesc
	itemTrackers []*itemTracker
	indexes      []int
}

type itemTracker struct {
	minSuccess  int
	maxFailures int
	succeeded   atomic.Int32
	failed4xx   atomic.Int32
	failed5xx   atomic.Int32
	remaining   atomic.Int32
	err         atomic.Error
}

func (i *itemTracker) recordError(err error) int32 {
	i.err.Store(err)

	if s, ok := status.FromError(err); ok && s.Code()/100 == 4 {
		return i.failed4xx.Inc()
	}

	return i.failed5xx.Inc()
}

// DoBatch request against a set of keys in the ring, handling replication and
// failures. For example if we want to write N items where they may all
// hit different instances, and we want them all replicated R ways with
// quorum writes, we track the relationship between batch RPCs and the items
// within them.
//
// Callback is passed the instance to target, and the indexes of the keys
// to send to that instance.
//
// cleanup() is always called, either on an error before starting the batches or after they all finish.
//
// Not implemented as a method on Ring so we can test separately.
func DoBatch(ctx context.Context, op Operation, r ReadRing, keys []uint32, callback func(InstanceDesc, []int) error, cleanup func()) error {
	if r.InstancesCount() <= 0 {
		cleanup()
		return fmt.Errorf("DoBatch: InstancesCount <= 0")
	}
	expectedTrackers := len(keys) * (r.ReplicationFactor() + 1) / r.InstancesCount()
	itemTrackers := make([]itemTracker, len(keys))
	instances := make(map[string]instance, r.InstancesCount())

	var (
		bufDescs [GetBufferSize]InstanceDesc
		bufHosts [GetBufferSize]string
		bufZones [GetBufferSize]string
	)
	for i, key := range keys {
		replicationSet, err := r.Get(key, op, bufDescs[:0], bufHosts[:0], bufZones[:0])
		if err != nil {
			cleanup()
			return err
		}
		itemTrackers[i].minSuccess = len(replicationSet.Instances) - replicationSet.MaxErrors
		itemTrackers[i].maxFailures = replicationSet.MaxErrors
		itemTrackers[i].remaining.Store(int32(len(replicationSet.Instances)))

		for _, desc := range replicationSet.Instances {
			curr, found := instances[desc.Addr]
			if !found {
				curr.itemTrackers = make([]*itemTracker, 0, expectedTrackers)
				curr.indexes = make([]int, 0, expectedTrackers)
			}
			instances[desc.Addr] = instance{
				desc:         desc,
				itemTrackers: append(curr.itemTrackers, &itemTrackers[i]),
				indexes:      append(curr.indexes, i),
			}
		}
	}

	tracker := batchTracker{
		done: make(chan struct{}, 1),
		err:  make(chan error, 1),
	}
	tracker.rpcsPending.Store(int32(len(itemTrackers)))

	var wg sync.WaitGroup

	wg.Add(len(instances))
	for _, i := range instances {
		go func(i instance) {
			err := callback(i.desc, i.indexes)
			tracker.record(i.itemTrackers, err)
			wg.Done()
		}(i)
	}

	// Perform cleanup at the end.
	go func() {
		wg.Wait()

		cleanup()
	}()

	select {
	case err := <-tracker.err:
		return err
	case <-tracker.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *batchTracker) record(itemTrackers []*itemTracker, err error) {
	// If we reach the required number of successful puts on this item, then decrement the
	// number of pending items by one.
	//
	// The use of atomic increments here is needed as:
	// * rpcsPending and rpcsFailed guarantee only a single goroutine will write to either channel
	// * succeeded, failed4xx, failed5xx and remaining guarantee that the "return decision" is made atomically
	// avoiding race condition
	for i := range itemTrackers {
		if err != nil {
			// Track the number of errors by error family, and if it exceeds maxFailures
			// shortcut the waiting rpc.
			errCount := itemTrackers[i].recordError(err)
			// We should return an error if we reach the maxFailure (quorum) on a given error family OR
			// we don't have any remaining instances to try.
			//
			// Ex: 2xx, 4xx, 5xx -> return 5xx
			// Ex: 4xx, 4xx, _ -> return 4xx
			// Ex: 5xx, _, 5xx -> return 5xx
			//
			// The reason for searching for quorum in 4xx and 5xx errors separately is to give a more accurate
			// response to the initial request. So if a quorum of instances rejects the request with 4xx, then the request should be rejected
			// even if less-than-quorum instances indicated a failure to process the request (via 5xx).
			// The speculation is that had the unavailable instances been available,
			// they would have rejected the request with a 4xx as well.
			// Conversely, if a quorum of instances failed to process the request via 5xx and less-than-quorum
			// instances rejected it with 4xx, then we do not have quorum to reject the request as a 4xx. Instead,
			// we return the last 5xx error for debuggability.
			if errCount > int32(itemTrackers[i].maxFailures) || itemTrackers[i].remaining.Dec() == 0 {
				if b.rpcsFailed.Inc() == 1 {
					b.err <- err
				}
			}
		} else {
			// If we successfully process items in minSuccess instances,
			// then wake up the waiting rpc, so it can return early.
			if itemTrackers[i].succeeded.Inc() >= int32(itemTrackers[i].minSuccess) {
				if b.rpcsPending.Dec() == 0 {
					b.done <- struct{}{}
				}
				continue
			}

			// If we successfully called this particular instance, but we don't have any remaining instances to try,
			// and we failed to call minSuccess instances, then we need to return the last error
			// Ex: 4xx, 5xx, 2xx
			if itemTrackers[i].remaining.Dec() == 0 {
				if b.rpcsFailed.Inc() == 1 {
					b.err <- itemTrackers[i].err.Load()
				}
			}
		}
	}
}
