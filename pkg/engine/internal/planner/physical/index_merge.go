package physical

import (
	"github.com/oklog/ulid/v2"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// IndexMerge represents one K-way sort-merge task over INDEX sections
// (postings + stats) for a single tenant within one ToC window. The task
// uploads the merged index object and emits one record that reports it.
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
}

// ID implements the Node interface.
func (n *IndexMerge) ID() ulid.ULID { return n.NodeID }

// Type implements the Node interface.
func (*IndexMerge) Type() NodeType { return NodeTypeIndexMerge }
