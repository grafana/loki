package workflow

import (
	"fmt"

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

	// The maximum boundary of timestamps that the task can possibly emit.
	// Does not account for predicates.
	// MaxTimeRange is not read when executing a task fragment. It can be used
	// as metadata to control execution (such as cancelling ongoing tasks based
	// on their maximum time range).
	MaxTimeRange physical.TimeRange
}

// ID returns the Task's ULID.
func (t *Task) ID() ulid.ULID { return t.ULID }

// String returns a human-readable representation of the Task.
func (t *Task) String() string {
	sourcesCount := 0
	for _, streams := range t.Sources {
		sourcesCount += len(streams)
	}
	sinksCount := 0
	for _, streams := range t.Sinks {
		sinksCount += len(streams)
	}

	fragmentInfo := "empty"
	if t.Fragment != nil && t.Fragment.Len() > 0 {
		fragmentInfo = fmt.Sprintf("%d nodes", t.Fragment.Len())
	}

	return fmt.Sprintf("Task[%s](tenant=%s, fragment=%s, sources=%d, sinks=%d, timeRange=%s-%s)",
		t.ULID, t.TenantID, fragmentInfo, sourcesCount, sinksCount,
		t.MaxTimeRange.Start.Format("15:04:05"), t.MaxTimeRange.End.Format("15:04:05"))
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

// String returns a human-readable representation of the Stream.
func (s *Stream) String() string {
	return fmt.Sprintf("Stream[%s](tenant=%s)", s.ULID, s.TenantID)
}
