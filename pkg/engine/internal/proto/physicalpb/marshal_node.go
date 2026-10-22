package physicalpb

import (
	"fmt"
	"slices"

	"github.com/oklog/ulid/v2"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

type marshaler interface {
	MarshalPhysical(nodeID ulid.ULID) (physical.Node, error)
}

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node) MarshalPhysical() (physical.Node, error) {
	m, ok := n.Kind.(marshaler)
	if !ok {
		return nil, fmt.Errorf("unsupported node type: %T", n.Kind)
	}
	return m.MarshalPhysical(ulid.ULID(n.Id.Value))
}

// MarshalPhysical converts a protobuf IndexMerge into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_IndexMerge) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.IndexMerge.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf IndexMerge into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *IndexMerge) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return &physical.IndexMerge{
		NodeID:         nodeID,
		Tenant:         n.Tenant,
		ToCWindowStart: n.TocWindowStartUnixNanos,
		Runs:           copyRunRefs(n.Runs),
	}, nil
}

// copyRunRefs deep-copies a RunRef slice and its nested SectionRefs
func copyRunRefs(in []*compactionv2pb.RunRef) []*compactionv2pb.RunRef {
	runs := make([]*compactionv2pb.RunRef, len(in))
	for i, r := range in {
		if r == nil {
			continue
		}
		sections := make([]*compactionv2pb.SectionRef, len(r.Sections))
		for j, s := range r.Sections {
			if s == nil {
				continue
			}
			sections[j] = &compactionv2pb.SectionRef{
				ObjectPath:       s.ObjectPath,
				SectionIndex:     s.SectionIndex,
				MinKey:           slices.Clone(s.MinKey),
				MaxKey:           slices.Clone(s.MaxKey),
				MinTimestamp:     s.MinTimestamp,
				MaxTimestamp:     s.MaxTimestamp,
				UncompressedSize: s.UncompressedSize,
			}
		}
		runs[i] = &compactionv2pb.RunRef{Sections: sections}
	}
	return runs
}

// MarshalPhysical converts a protobuf LogMerge into a physical plan node.
func (n *Node_LogMerge) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.LogMerge.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf LogMerge into a physical plan node.
func (n *LogMerge) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return &physical.LogMerge{
		NodeID:         nodeID,
		Tenant:         n.Tenant,
		ToCWindowStart: n.TocWindowStartUnixNanos,
		Runs:           copyRunRefs(n.Runs),
	}, nil
}

// MarshalPhysical converts a protobuf IndexFilter into a physical plan node.
func (n *Node_IndexFilter) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.IndexFilter.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf IndexFilter into a physical plan node.
func (n *IndexFilter) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return &physical.IndexFilter{
		NodeID:          nodeID,
		Tenant:          n.Tenant,
		SourceIndexPath: n.SourceIndexPath,
		ObjectPaths:     slices.Clone(n.ObjectPaths),
	}, nil
}
