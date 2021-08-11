package ring

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/atomic"
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
	failed      atomic.Int32
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
// Not implemented as a method on Ring so we can test separately.
func DoBatch(ctx context.Context, op Operation, r ReadRing, keys []uint32, callback func(InstanceDesc, []int) error, cleanup func()) error {
	if r.InstancesCount() <= 0 {
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
			return err
		}
		itemTrackers[i].minSuccess = len(replicationSet.Instances) - replicationSet.MaxErrors
		itemTrackers[i].maxFailures = replicationSet.MaxErrors

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

func (b *batchTracker) record(sampleTrackers []*itemTracker, err error) {
	// If we succeed, decrement each sample's pending count by one.  If we reach
	// the required number of successful puts on this sample, then decrement the
	// number of pending samples by one.  If we successfully push all samples to
	// min success instances, wake up the waiting rpc so it can return early.
	// Similarly, track the number of errors, and if it exceeds maxFailures
	// shortcut the waiting rpc.
	//
	// The use of atomic increments here guarantees only a single sendSamples
	// goroutine will write to either channel.
	for i := range sampleTrackers {
		if err != nil {
			if sampleTrackers[i].failed.Inc() <= int32(sampleTrackers[i].maxFailures) {
				continue
			}
			if b.rpcsFailed.Inc() == 1 {
				b.err <- err
			}
		} else {
			if sampleTrackers[i].succeeded.Inc() != int32(sampleTrackers[i].minSuccess) {
				continue
			}
			if b.rpcsPending.Dec() == 0 {
				b.done <- struct{}{}
			}
		}
	}
}
