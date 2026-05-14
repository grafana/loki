package physical

import (
	"slices"
	"time"

	"github.com/oklog/ulid/v2"
)

// TableOfContentsConsolidate is the Phase-2 physical-plan node of the
// dataobj-compactor v1.0 workflow. Its sole job is to perform the single
// primary-ToC swap that adds the freshly-merged indexes (produced by Phase-1
// IndexMerge tasks) and removes the indexes they replaced.
//
// In v1.0 the node does not open any objects, build any index, or upload
// anything to the bucket. It calls
//
//	metastoreWriter.ReplaceIndexPointers(ctx, ToCWindowStart, Tenant,
//	                                     RemoveIndexPaths, makeEntries(AddIndexPaths))
//
// and reports Completed. The real executor is delivered in PR A12; a
// clearly-labelled stub lives in the executor package until then.
//
// The node is emitted only by the dataobj-compactor coordinator (PR A11).
// TableOfContentsConsolidate is NEVER part of a cacheable query task
// fragment.
type TableOfContentsConsolidate struct {
	NodeID ulid.ULID

	// Tenant is the single-tenant scope for the swap. All paths below belong
	// to this tenant.
	Tenant string

	// ToCWindowStart is the start of the ToC window for this compaction
	// cycle, expressed as nanoseconds since the Unix epoch. Identifies the
	// primary ToC the swap is applied to.
	ToCWindowStart int64

	// RemoveIndexPaths are existing ToC entries to remove — the indexes
	// being consolidated. Sized ⌈P/K⌉ … N depending on cycle.
	RemoveIndexPaths []string

	// AddIndexPaths are new ToC entries to add — the merged indexes
	// produced by Phase-1 IndexMerge tasks. Sized ⌈P/K⌉.
	AddIndexPaths []string

	// TaskTTL bounds the task execution time via context.WithDeadline inside
	// the executor. Sourced from the table_of_contents_consolidate_task_ttl
	// config knob.
	TaskTTL time.Duration
}

// ID returns the ULID that uniquely identifies the node in the plan.
func (n *TableOfContentsConsolidate) ID() ulid.ULID { return n.NodeID }

// Type returns [NodeTypeTableOfContentsConsolidate].
func (*TableOfContentsConsolidate) Type() NodeType { return NodeTypeTableOfContentsConsolidate }

// Clone returns a deep copy of the node with a fresh ULID. Slice fields are
// copied so cloned plans can be mutated independently.
func (n *TableOfContentsConsolidate) Clone() Node {
	return &TableOfContentsConsolidate{
		NodeID:           ulid.Make(),
		Tenant:           n.Tenant,
		ToCWindowStart:   n.ToCWindowStart,
		RemoveIndexPaths: slices.Clone(n.RemoveIndexPaths),
		AddIndexPaths:    slices.Clone(n.AddIndexPaths),
		TaskTTL:          n.TaskTTL,
	}
}
