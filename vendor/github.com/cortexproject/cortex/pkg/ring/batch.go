package ring

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

type batchTracker struct {
	rpcsPending int32
	rpcsFailed  int32
	done        chan struct{}
	err         chan error
}

type ingester struct {
	desc         IngesterDesc
	itemTrackers []*itemTracker
	indexes      []int
}

type itemTracker struct {
	minSuccess  int
	maxFailures int
	succeeded   int32
	failed      int32
}

// DoBatch request against a set of keys in the ring, handling replication and
// failures. For example if we want to write N items where they may all
// hit different ingesters, and we want them all replicated R ways with
// quorum writes, we track the relationship between batch RPCs and the items
// within them.
//
// Callback is passed the ingester to target, and the indexes of the keys
// to send to that ingester.
//
// Not implemented as a method on Ring so we can test separately.
func DoBatch(ctx context.Context, r ReadRing, keys []uint32, callback func(IngesterDesc, []int) error, cleanup func()) error {
	if r.IngesterCount() <= 0 {
		return fmt.Errorf("DoBatch: IngesterCount <= 0")
	}
	expectedTrackers := len(keys) * (r.ReplicationFactor() + 1) / r.IngesterCount()
	itemTrackers := make([]itemTracker, len(keys))
	ingesters := make(map[string]ingester, r.IngesterCount())

	const maxExpectedReplicationSet = 5 // Typical replication factor 3, plus one for inactive plus one for luck.
	var descs [maxExpectedReplicationSet]IngesterDesc
	for i, key := range keys {
		replicationSet, err := r.Get(key, Write, descs[:0])
		if err != nil {
			return err
		}
		itemTrackers[i].minSuccess = len(replicationSet.Ingesters) - replicationSet.MaxErrors
		itemTrackers[i].maxFailures = replicationSet.MaxErrors

		for _, desc := range replicationSet.Ingesters {
			curr, found := ingesters[desc.Addr]
			if !found {
				curr.itemTrackers = make([]*itemTracker, 0, expectedTrackers)
				curr.indexes = make([]int, 0, expectedTrackers)
			}
			ingesters[desc.Addr] = ingester{
				desc:         desc,
				itemTrackers: append(curr.itemTrackers, &itemTrackers[i]),
				indexes:      append(curr.indexes, i),
			}
		}
	}

	tracker := batchTracker{
		rpcsPending: int32(len(itemTrackers)),
		done:        make(chan struct{}, 1),
		err:         make(chan error, 1),
	}

	var wg sync.WaitGroup

	wg.Add(len(ingesters))
	for _, i := range ingesters {
		go func(i ingester) {
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
	// min success ingesters, wake up the waiting rpc so it can return early.
	// Similarly, track the number of errors, and if it exceeds maxFailures
	// shortcut the waiting rpc.
	//
	// The use of atomic increments here guarantees only a single sendSamples
	// goroutine will write to either channel.
	for i := range sampleTrackers {
		if err != nil {
			if atomic.AddInt32(&sampleTrackers[i].failed, 1) <= int32(sampleTrackers[i].maxFailures) {
				continue
			}
			if atomic.AddInt32(&b.rpcsFailed, 1) == 1 {
				b.err <- err
			}
		} else {
			if atomic.AddInt32(&sampleTrackers[i].succeeded, 1) != int32(sampleTrackers[i].minSuccess) {
				continue
			}
			if atomic.AddInt32(&b.rpcsPending, -1) == 0 {
				b.done <- struct{}{}
			}
		}
	}
}
