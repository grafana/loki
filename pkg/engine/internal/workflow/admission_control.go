package workflow

import (
	"math"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

var defaultAdmissionControlOpts = admissionControlOpts{
	scanBucketCapacity:  32,
	otherBucketCapacity: math.MaxInt,
}

type admissionControlOpts struct {
	scanBucketCapacity  int64
	otherBucketCapacity int64
}

// admissionControl is a control structure to lookup token buckets ("admission lanes")
// for different types of tasks.
type admissionControl struct {
	scan  *weightedSemaphore
	other *weightedSemaphore
}

func newAdmissionControl(opts admissionControlOpts) *admissionControl {
	return &admissionControl{
		scan:  newWeightedSemaphore(opts.scanBucketCapacity, "scan"),
		other: newWeightedSemaphore(opts.otherBucketCapacity, "other"),
	}
}

// groupByBucket categorizes a slice of tasks into groups based on their characteristics (scan, other, ...).
func (ac *admissionControl) groupByBucket(tasks []*Task) map[*weightedSemaphore][]*Task {
	tasksPerBucket := map[*weightedSemaphore][]*Task{
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
func (ac *admissionControl) tokenBucketFor(task *Task) *weightedSemaphore {
	for node := range task.Fragment.Graph().Nodes() {
		if node.Type() == physical.NodeTypeDataObjScan {
			return ac.scan
		}
	}
	return ac.other
}
