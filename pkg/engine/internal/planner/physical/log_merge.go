package physical

import (
	"context"
	"slices"

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

	// SortSchema is the tenant's resolved sort schema as ordered FQN sort keys
	// (e.g. "label:service_name")
	SortSchema []string

	// OutputIndexPath is the deterministic object-storage key of the index
	// object the worker builds from the newly-created compacted log object and
	// returns to the planner for the ToC swap
	OutputIndexPath string
}

// ID implements the Node interface.
func (n *LogMerge) ID() ulid.ULID { return n.NodeID }

// Type implements the Node interface.
func (*LogMerge) Type() NodeType { return NodeTypeLogMerge }

// Clone implements the Node interface.
func (n *LogMerge) Clone() Node {
	return &LogMerge{
		NodeID:          ulid.Make(),
		Tenant:          n.Tenant,
		ToCWindowStart:  n.ToCWindowStart,
		Runs:            cloneRuns(n.Runs),
		SortSchema:      slices.Clone(n.SortSchema),
		OutputIndexPath: n.OutputIndexPath,
	}
}

// CacheKey implements the Node interface. LogMerge is a mutation node;
// returning "" disables Cache wrapping.
func (*LogMerge) CacheKey(_ context.Context) string { return "" }
