package workflow

import (
	"math"

	"golang.org/x/sync/semaphore"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

type taskType string

const (
	taskTypeScan  taskType = "scan"
	taskTypeOther taskType = "other"
)

type admissionLane struct {
	*semaphore.Weighted
	capacity int64
	lane     taskType
}

func newAdmissionLane(lane taskType, capacity int64) *admissionLane {
	return &admissionLane{
		Weighted: semaphore.NewWeighted(capacity),
		capacity: capacity,
		lane:     lane,
	}
}

// admissionControl is a control structure to lookup "admission lanes" for different types of tasks.
// It is a lightweight wrapper around a mapping of task type to admission lane.
type admissionControl struct {
	mapping map[taskType]*admissionLane
}

func newAdmissionControl(maxScanTasks, maxOtherTasks int64) *admissionControl {
	if maxScanTasks < 1 {
		maxScanTasks = math.MaxInt64
	}
	if maxOtherTasks < 1 {
		maxOtherTasks = math.MaxInt64
	}

	return &admissionControl{
		mapping: map[taskType]*admissionLane{
			taskTypeScan:  newAdmissionLane(taskTypeScan, maxScanTasks),
			taskTypeOther: newAdmissionLane(taskTypeOther, maxOtherTasks),
		},
	}
}

// groupByBucket categorizes a slice of tasks into groups based on their characteristics (scan, other, ...).
func (ac *admissionControl) groupByType(tasks []*Task) map[taskType][]*Task {
	groups := map[taskType][]*Task{
		taskTypeScan:  make([]*Task, 0, len(tasks)),
		taskTypeOther: make([]*Task, 0, len(tasks)),
	}

	for _, t := range tasks {
		ty := ac.typeFor(t)
		groups[ty] = append(groups[ty], t)
	}

	return groups
}

func (ac *admissionControl) typeFor(task *Task) taskType {
	if isScanTask(task) {
		return taskTypeScan
	}
	return taskTypeOther
}

func (ac *admissionControl) laneFor(task *Task) *admissionLane {
	return ac.mapping[ac.typeFor(task)]
}

func (ac *admissionControl) get(ty taskType) *admissionLane {
	return ac.mapping[ty]
}

func isScanTask(task *Task) bool {
	for node := range task.Fragment.Graph().Nodes() {
		if node.Type() == physical.NodeTypeDataObjScan || node.Type() == physical.NodeTypePointersScan {
			return true
		}
	}
	return false
}
