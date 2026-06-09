package workflow

import (
	"math"

	"golang.org/x/sync/semaphore"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// TaskType identifies the admission lane a task is governed by. It is derived
// from the task's physical fragment via [Task.Type].
type TaskType string

const (
	TaskTypeScan  TaskType = "scan"
	TaskTypeOther TaskType = "other"

	// TaskTypeCompaction is the admission lane for compaction tasks
	TaskTypeCompaction TaskType = "compaction"
)

type admissionLane struct {
	*semaphore.Weighted
	capacity int64
	lane     TaskType
}

func newAdmissionLane(lane TaskType, capacity int64) *admissionLane {
	return &admissionLane{
		Weighted: semaphore.NewWeighted(capacity),
		capacity: capacity,
		lane:     lane,
	}
}

// admissionControl is a control structure to lookup "admission lanes" for different types of tasks.
// It is a lightweight wrapper around a mapping of task type to admission lane.
type admissionControl struct {
	mapping map[TaskType]*admissionLane
}

func newAdmissionControl(maxScanTasks, maxOtherTasks, maxCompactionTasks int64) *admissionControl {
	if maxScanTasks < 1 {
		maxScanTasks = math.MaxInt64
	}
	if maxOtherTasks < 1 {
		maxOtherTasks = math.MaxInt64
	}
	if maxCompactionTasks < 1 {
		maxCompactionTasks = math.MaxInt64
	}

	return &admissionControl{
		mapping: map[TaskType]*admissionLane{
			TaskTypeScan:       newAdmissionLane(TaskTypeScan, maxScanTasks),
			TaskTypeOther:      newAdmissionLane(TaskTypeOther, maxOtherTasks),
			TaskTypeCompaction: newAdmissionLane(TaskTypeCompaction, maxCompactionTasks),
		},
	}
}

// groupByType categorizes a slice of tasks into groups based on their characteristics (scan, other, compaction).
func (ac *admissionControl) groupByType(tasks []*Task) map[TaskType][]*Task {
	groups := map[TaskType][]*Task{
		TaskTypeScan:       make([]*Task, 0, len(tasks)),
		TaskTypeOther:      make([]*Task, 0, len(tasks)),
		TaskTypeCompaction: make([]*Task, 0, len(tasks)),
	}

	for _, t := range tasks {
		groups[t.Type()] = append(groups[t.Type()], t)
	}

	return groups
}

// Type classifies a task by inspecting its physical fragment.
// Compaction is checked first so hypothetical hybrid plans are constrained
// by the compaction lane rather than the scan lane.
func (t *Task) Type() TaskType {
	if isCompactionTask(t) {
		return TaskTypeCompaction
	}
	if isScanTask(t) {
		return TaskTypeScan
	}
	return TaskTypeOther
}

func (ac *admissionControl) laneFor(task *Task) *admissionLane {
	return ac.mapping[task.Type()]
}

func (ac *admissionControl) get(ty TaskType) *admissionLane {
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

func isCompactionTask(task *Task) bool {
	for node := range task.Fragment.Graph().Nodes() {
		switch node.Type() {
		case physical.NodeTypeIndexMerge, physical.NodeTypeLogMerge:
			return true
		}
	}
	return false
}
