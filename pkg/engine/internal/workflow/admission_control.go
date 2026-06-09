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

	// taskTypeCompaction is the admission lane for compaction tasks
	taskTypeCompaction taskType = "compaction"
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
		mapping: map[taskType]*admissionLane{
			taskTypeScan:       newAdmissionLane(taskTypeScan, maxScanTasks),
			taskTypeOther:      newAdmissionLane(taskTypeOther, maxOtherTasks),
			taskTypeCompaction: newAdmissionLane(taskTypeCompaction, maxCompactionTasks),
		},
	}
}

// groupByType categorizes a slice of tasks into groups based on their characteristics (scan, other, compaction).
func (ac *admissionControl) groupByType(tasks []*Task) map[taskType][]*Task {
	groups := map[taskType][]*Task{
		taskTypeScan:       make([]*Task, 0, len(tasks)),
		taskTypeOther:      make([]*Task, 0, len(tasks)),
		taskTypeCompaction: make([]*Task, 0, len(tasks)),
	}

	for _, t := range tasks {
		ty := ac.typeFor(t)
		groups[ty] = append(groups[ty], t)
	}

	return groups
}

// typeFor classifies a task by inspecting its fragment.
// Compaction is checked first so hypothetical hybrid plans are constrained
// by the compaction lane rather than the scan lane.
func (ac *admissionControl) typeFor(task *Task) taskType {
	if isCompactionTask(task) {
		return taskTypeCompaction
	}
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

func isPostingsScanTask(task *Task) bool {
	for node := range task.Fragment.Graph().Nodes() {
		if node.Type() == physical.NodeTypePointersScan {
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
