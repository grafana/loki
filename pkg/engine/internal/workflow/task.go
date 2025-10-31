package workflow

import (
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

type TaskCategory uint8

const (
	TaskIsSharded TaskCategory = iota
)

// A Task is a single unit of work within a workflow. Each Task is a partition
// of a local physical plan.
type Task struct {
	// ULID is a unique identifier of the Task.
	ULID ulid.ULID

	// TenantID is a tenant associated with this task.
	TenantID string

	// Fragment is the local physical plan that this Task represents.
	Fragment *physical.Plan

	// Sources defines which Streams physical nodes read from. Sources are only
	// defined for nodes in the Fragment which read data across task boundaries.
	Sources map[physical.Node][]*Stream

	// Sinks defines which Streams physical nodes write to. Sinks are only
	// defined for nodes in the Fragment which write data across task boundaries.
	Sinks map[physical.Node][]*Stream

	// Cost associated with the task.
	Cost uint

	// Category stores flags that categorize the task.
	Category uint32
}

// ID returns the string form of the Task's ULID.
func (t *Task) ID() string { return t.ULID.String() }

// SetCategory sets a category flag using bit-shifting.
// Multiple categories can be set by calling this method multiple times.
func (t *Task) SetCategory(category TaskCategory) {
	t.Category |= (1 << category)
}

// HasCategory checks if a specific category flag is set using bit-shifting.
func (t *Task) HasCategory(category TaskCategory) bool {
	return (t.Category & (1 << category)) != 0
}

// UnsetCategory clears a specific category flag using bit-shifting.
func (t *Task) UnsetCategory(category TaskCategory) {
	t.Category &^= (1 << category)
}

// A Stream is an abstract representation of how data flows across Task
// boundaries. Each Stream has exactly one sender (a Task), and one receiver
// (either another Task or the owning [Workflow]).
type Stream struct {
	// ULID is a unique identifier of the Stream.
	ULID ulid.ULID

	// TenantID is a tenant associated with this stream.
	TenantID string
}
