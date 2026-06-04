package physical

import (
	"context"
	"slices"
	"time"

	"github.com/oklog/ulid/v2"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// IndexMerge represents one K-way sort-merge task over INDEX sections
// (postings + stats) for a single tenant within one ToC window. The node
// is a mutation node: it has no Arrow output and is not cacheable.
type IndexMerge struct {
	NodeID ulid.ULID

	// Tenant the merge is scoped to.
	Tenant string

	// ToCWindowStart is the start (in unix nanos) of the ToC window being
	// compacted.
	ToCWindowStart int64

	// Runs are the K piles (K ≤ max_runs_per_task) the merge consumes.
	// Each RunRef is a sorted, non-overlapping sequence of SectionRefs
	// pointing at INDEX sections (postings + stats) in object storage.
	// The executor sources source-index paths from Runs[*].Sections[*].ObjectPath;
	// there is no separate SourceIndexPaths field on IndexMerge.
	Runs []*compactionv2pb.RunRef

	// OutputIndexPath is the deterministic object-storage key where the
	// executor writes the merged index object. Existence-check at executor
	// start short-circuits if a previous task already produced this object.
	OutputIndexPath string

	// TaskTTL is the per-task execution deadline enforced by the executor
	// via context.WithDeadline.
	TaskTTL time.Duration
}

// ID implements the Node interface.
func (n *IndexMerge) ID() ulid.ULID { return n.NodeID }

// Type implements the Node interface.
func (*IndexMerge) Type() NodeType { return NodeTypeIndexMerge }

// Clone implements the Node interface.
func (n *IndexMerge) Clone() Node {
	return &IndexMerge{
		NodeID:          ulid.Make(),
		Tenant:          n.Tenant,
		ToCWindowStart:  n.ToCWindowStart,
		Runs:            cloneRuns(n.Runs),
		OutputIndexPath: n.OutputIndexPath,
		TaskTTL:         n.TaskTTL,
	}
}

// CacheKey implements the Node interface. IndexMerge is a mutation node;
// its result is an object-storage upload, not an Arrow batch. Returning
// "" disables Cache wrapping.
func (*IndexMerge) CacheKey(_ context.Context) string { return "" }

// cloneRuns deep-copies a RunRef slice including the nested SectionRef
// slices (whose MinKey / MaxKey are themselves slices). Used by both
// IndexMerge.Clone and LogMerge.Clone. Nil RunRef and nil SectionRef
// entries are tolerated and pass through as nil.
func cloneRuns(runs []*compactionv2pb.RunRef) []*compactionv2pb.RunRef {
	if runs == nil {
		return nil
	}
	out := make([]*compactionv2pb.RunRef, len(runs))
	for i, r := range runs {
		if r == nil {
			continue
		}
		sections := make([]*compactionv2pb.SectionRef, len(r.Sections))
		for j, s := range r.Sections {
			if s == nil {
				continue
			}
			cp := *s
			cp.MinKey = slices.Clone(s.MinKey)
			cp.MaxKey = slices.Clone(s.MaxKey)
			sections[j] = &cp
		}
		out[i] = &compactionv2pb.RunRef{Sections: sections}
	}
	return out
}
