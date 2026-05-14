package physical

import (
	"slices"
	"time"

	"github.com/oklog/ulid/v2"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// CompactionMerge represents one K-way sort-merge task: it consumes K piles
// of section references (Runs) for a single tenant within one ToC window and
// produces a compacted log object at OutputPath. The node is a mutation node:
// it has no Arrow output and is not cacheable.
type CompactionMerge struct {
	NodeID ulid.ULID

	// Tenant the merge is scoped to. Used by the workflow scope and by the
	// executor for output-object labeling.
	Tenant string

	// ToCWindowStart is the start (in unix nanos) of the ToC window being
	// compacted. Used by the executor and by deterministic output-path
	// generation upstream.
	ToCWindowStart int64

	// Runs are the K piles (K ≤ max_runs_per_task) the merge consumes.
	// Each RunRef is a sorted, non-overlapping sequence of SectionRefs.
	Runs []*compactionv2pb.RunRef

	// SourceIndexPaths is the set of unique source-index paths referenced
	// across all Runs. Used by the consolidation step to know which indexes
	// the merge's outputs replace.
	SourceIndexPaths []string

	// OutputPath is the deterministic object-storage key where the executor
	// writes the compacted log object. If the merge output exceeds the
	// configured target object size the executor may write additional log
	// objects with keys derived from OutputPath; the derivation scheme is
	// owned by the executor. CompactionMerge writes log objects only; index
	// rebuild is handled separately by IndexConsolidate.
	OutputPath string

	// TaskTTL is the per-task execution deadline enforced by the executor
	// via context.WithDeadline.
	TaskTTL time.Duration
}

// ID implements the Node interface.
func (n *CompactionMerge) ID() ulid.ULID { return n.NodeID }

// Type implements the Node interface.
func (*CompactionMerge) Type() NodeType { return NodeTypeCompactionMerge }

// Clone implements the Node interface. The returned node has a fresh ULID
// and deep-copied Runs / SourceIndexPaths so callers can mutate either copy
// without affecting the other.
func (n *CompactionMerge) Clone() Node {
	return &CompactionMerge{
		NodeID:           ulid.Make(),
		Tenant:           n.Tenant,
		ToCWindowStart:   n.ToCWindowStart,
		Runs:             cloneRuns(n.Runs),
		SourceIndexPaths: slices.Clone(n.SourceIndexPaths),
		OutputPath:       n.OutputPath,
		TaskTTL:          n.TaskTTL,
	}
}


