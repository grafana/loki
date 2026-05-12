package physical

import (
	"slices"
	"time"

	"github.com/oklog/ulid/v2"
)

// IndexConsolidate is a physical-plan node representing a single Phase-2
// compaction task: read the streams sections of all CompactedLogObjectPaths,
// build one covering index, upload it to OutputIndexPath, perform the single
// primary-ToC swap, then delete the workflow marker at MarkerPath.
//
// The node is emitted only by the dataobj-compactor coordinator (PR A11). The
// real executor lives in `pkg/engine/internal/executor/` and is delivered in
// A12; a clearly-labelled stub lives there until then.
//
// IndexConsolidate is NEVER part of a cacheable query task fragment.
type IndexConsolidate struct {
	NodeID ulid.ULID

	// Tenant is the single-tenant scope for the task. All compacted log objects
	// and source indexes referenced below belong to this tenant.
	Tenant string

	// ToCWindowStart is the start of the ToC window for this compaction cycle,
	// expressed as nanoseconds since the Unix epoch. Used as a stable
	// scoping/identity field along with Tenant.
	ToCWindowStart int64

	// CompactedLogObjectPaths are the deterministic output paths of all Phase-1
	// CompactionMerge tasks. The executor opens each one's streams section.
	CompactedLogObjectPaths []string

	// SourceIndexPaths are the original index objects that must be removed from
	// the primary ToC as part of the GetAndReplace swap.
	SourceIndexPaths []string

	// OutputIndexPath is the deterministic destination for the consolidated
	// covering index.
	OutputIndexPath string

	// MarkerPath is the workflow marker to delete once the swap completes.
	MarkerPath string

	// TaskTTL bounds the task execution time via context.WithDeadline inside the
	// executor. Sourced from the index_consolidate_task_ttl config knob.
	TaskTTL time.Duration
}

// ID returns the ULID that uniquely identifies the node in the plan.
func (n *IndexConsolidate) ID() ulid.ULID { return n.NodeID }

// Type returns [NodeTypeIndexConsolidate].
func (*IndexConsolidate) Type() NodeType { return NodeTypeIndexConsolidate }

// Clone returns a deep copy of the node with a fresh ULID. Slice fields are
// copied so cloned plans can be mutated independently.
func (n *IndexConsolidate) Clone() Node {
	return &IndexConsolidate{
		NodeID:                  ulid.Make(),
		Tenant:                  n.Tenant,
		ToCWindowStart:          n.ToCWindowStart,
		CompactedLogObjectPaths: slices.Clone(n.CompactedLogObjectPaths),
		SourceIndexPaths:        slices.Clone(n.SourceIndexPaths),
		OutputIndexPath:         n.OutputIndexPath,
		MarkerPath:              n.MarkerPath,
		TaskTTL:                 n.TaskTTL,
	}
}
