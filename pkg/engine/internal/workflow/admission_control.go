package workflow

import (
	"context"
	"math"
)

var defaultAdmissionControlOpts = admissionControlOpts{
	scanBucketCapacity:  32,
	otherBucketCapacity: math.MaxUint,
}

type admissionControlOpts struct {
	scanBucketCapacity  uint
	otherBucketCapacity uint
}

// admissionControl is a control structure to lookup token buckets ("admission lanes")
// for different types of tasks.
type admissionControl struct {
	scan  *batchTokenBucketWithContext
	other *batchTokenBucketWithContext
}

func newAdmissionControl(ctx context.Context, opts admissionControlOpts) *admissionControl {
	return &admissionControl{
		scan:  newBatchTokenBucketWithContext(opts.scanBucketCapacity, ctx),
		other: newBatchTokenBucketWithContext(opts.scanBucketCapacity, ctx),
	}
}

// groupByBucket categorizes a slice of tasks into groups based on their characteristics (scan, other, ...).
func (ac *admissionControl) groupByBucket(tasks []*Task) map[TokenBucket][]*Task {
	tasksPerBucket := map[TokenBucket][]*Task{
		ac.scan:  make([]*Task, 0, len(tasks)),
		ac.other: make([]*Task, 0, len(tasks)),
	}

	for _, t := range tasks {
		bucket := ac.tokenBucketFor(t)
		tasksPerBucket[bucket] = append(tasksPerBucket[bucket], t)
	}

	return tasksPerBucket
}

// tokenBucketFor returns the token bucket ("admission lane") for the given task
// based on its characteristics.
// This function panics if the task is nil.
func (ac *admissionControl) tokenBucketFor(task *Task) TokenBucket {
	// Scan nodes can be differentiated from other nodes because they don't have any sources.
	if len(task.Sources) > 0 {
		return ac.scan
	}
	return ac.other
}
