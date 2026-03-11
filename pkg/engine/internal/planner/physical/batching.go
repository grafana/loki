package physical

import (
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/oklog/ulid/v2"
)

// Batching is a plan node that controls how records are grouped into output
// batches. It wraps the root of the plan and is added after optimization.
type Batching struct {
	NodeID    ulid.ULID
	BatchSize int64
}

// ID returns the ULID that uniquely identifies the node in the plan.
func (b *Batching) ID() ulid.ULID { return b.NodeID }

// Type returns [NodeTypeBatching].
func (*Batching) Type() NodeType { return NodeTypeBatching }

// Clone returns a deep copy of the node with a new unique ID.
func (b *Batching) Clone() Node {
	return &Batching{NodeID: ulid.Make(), BatchSize: b.BatchSize}
}

// WrapWithBatching inserts a [Batching] node as the new root of plan, with
// the existing root as its only child. It modifies plan in-place and returns it.
func WrapWithBatching(plan *Plan, batchSize int) (*Plan, error) {
	root, err := plan.Root()
	if err != nil {
		return nil, err
	}
	node := &Batching{NodeID: ulid.Make(), BatchSize: int64(batchSize)}
	plan.graph.Add(node)
	return plan, plan.graph.AddEdge(dag.Edge[Node]{Parent: node, Child: root})
}
