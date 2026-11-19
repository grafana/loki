package physical

import (
	"github.com/oklog/ulid/v2"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
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

	// KeySpan limits the merge to the postings rows whose posting key is in
	// the span. The zero value holds every key.
	KeySpan postings.KeySpan

	// StatsSources are the source index objects whose stats rows a merge with
	// a bounded KeySpan merges. Each source of a split merge is a stats source
	// of one task, so the run of the merge holds each stats row once. A merge
	// over every key merges the stats rows of every source and has no
	// StatsSources.
	StatsSources []string
}

// ID implements the Node interface.
func (n *IndexMerge) ID() ulid.ULID { return n.NodeID }

// Type implements the Node interface.
func (*IndexMerge) Type() NodeType { return NodeTypeIndexMerge }
