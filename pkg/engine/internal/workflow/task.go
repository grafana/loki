package workflow

import (
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
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

	// CachedSources maps each physical node to the pre-fetched encoded Arrow
	// record batch buffers it should decode directly instead of receiving data
	// from a child task over a network stream. Populated at plan time when a
	// child task is eliminated because its cached result is known to be non-empty.
	CachedSources map[physical.Node]CachedSources

	// The maximum boundary of timestamps that the task can possibly emit.
	// Does not account for predicates.
	// MaxTimeRange is not read when executing a task fragment. It can be used
	// as metadata to control execution (such as cancelling ongoing tasks based
	// on their maximum time range).
	MaxTimeRange physical.TimeRange
}

// ID returns the Task's ULID.
func (t *Task) ID() ulid.ULID { return t.ULID }

// CachedSources is a set of pre-fetched encoded Arrow record batch buffers that
// a parent task decodes directly instead of receiving data from a child task
// over a network stream. Each element encodes the result of one cached child
// task and is fetched from the task result cache by the scheduler.
type CachedSources [][]byte

// A Stream is an abstract representation of how data flows across Task
// boundaries. Each Stream has exactly one sender (a Task), and one receiver
// (either another Task or the owning [Workflow]).
type Stream struct {
	// ULID is a unique identifier of the Stream.
	ULID ulid.ULID

	// TenantID is a tenant associated with this stream.
	TenantID string
}
