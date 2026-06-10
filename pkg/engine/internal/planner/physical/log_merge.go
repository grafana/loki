package physical

import (
	"context"
	"slices"
	"time"

	"github.com/oklog/ulid/v2"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// LogMerge represents one K-way sort-merge task over LOG sections for a
// single tenant within one ToC window. The node is a mutation node: it
// has no Arrow output and is not cacheable.
type LogMerge struct {
	NodeID ulid.ULID

	// Tenant the merge is scoped to.
	Tenant string

	// ToCWindowStart is the start (in unix nanos) of the ToC window.
	ToCWindowStart int64

	// Runs are the K piles the merge consumes; each RunRef references LOG
	// sections in object storage.
	Runs []compactionv2pb.RunRef

	// SourceIndexPaths is the set of unique source-index paths referenced across
	// all Runs. Used by the consolidation step to know which indexes the merge's
	// outputs replace.
	SourceIndexPaths []string

	// OutputPath is the deterministic object-storage key where the
	// executor writes the compacted log object.
	OutputPath string

	// TaskTTL is the per-task execution deadline.
	TaskTTL time.Duration
}

// ID implements the Node interface.
func (n *LogMerge) ID() ulid.ULID { return n.NodeID }

// Type implements the Node interface.
func (*LogMerge) Type() NodeType { return NodeTypeLogMerge }

// Clone implements the Node interface.
func (n *LogMerge) Clone() Node {
	return &LogMerge{
		NodeID:           ulid.Make(),
		Tenant:           n.Tenant,
		ToCWindowStart:   n.ToCWindowStart,
		Runs:             cloneRuns(n.Runs),
		SourceIndexPaths: slices.Clone(n.SourceIndexPaths),
		OutputPath:       n.OutputPath,
		TaskTTL:          n.TaskTTL,
	}
}

// CacheKey implements the Node interface. LogMerge is a mutation node;
// returning "" disables Cache wrapping.
func (*LogMerge) CacheKey(_ context.Context) string { return "" }
