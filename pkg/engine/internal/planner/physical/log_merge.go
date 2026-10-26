package physical

import (
	"github.com/oklog/ulid/v2"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// LogMerge represents one K-way sort-merge task over LOG sections for a
// single tenant within one ToC window. The task uploads the compacted
// objects and emits one record that reports the result.
type LogMerge struct {
	NodeID ulid.ULID

	// Tenant the merge is scoped to.
	Tenant string

	// ToCWindowStart is the start (in unix nanos) of the ToC window.
	ToCWindowStart int64

	// Runs are the K piles the merge consumes; each RunRef references LOG
	// sections in object storage.
	Runs []*compactionv2pb.RunRef

	// KeyRange limits the merge to the streams in the range. Sections in Runs
	// can hold streams outside it, and the merge drops their records.
	//
	// The coordinator always sets both bounds. A nil KeyRange holds every
	// stream, for plans from coordinators that predate key ranges.
	KeyRange *compactionv2pb.KeyRange
}

// ID implements the Node interface.
func (n *LogMerge) ID() ulid.ULID { return n.NodeID }

// Type implements the Node interface.
func (*LogMerge) Type() NodeType { return NodeTypeLogMerge }
