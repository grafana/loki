package physicalpb

import (
	fmt "fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/ulid"
)

type unmarshaler interface {
	UnmarshalPhysical(node physical.Node) error
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node) UnmarshalPhysical(from physical.Node) error {
	switch from.(type) {
	case *physical.IndexMerge:
		n.Kind = &Node_IndexMerge{}
	case *physical.LogMerge:
		n.Kind = &Node_LogMerge{}
	case *physical.IndexFilter:
		n.Kind = &Node_IndexFilter{}
	default:
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	n.Id = NodeID{Value: ulid.ULID(from.ID())}

	u, ok := n.Kind.(unmarshaler)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}
	return u.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_IndexMerge) UnmarshalPhysical(from physical.Node) error {
	n.IndexMerge = new(IndexMerge)
	return n.IndexMerge.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *IndexMerge) UnmarshalPhysical(from physical.Node) error {
	indexMerge, ok := from.(*physical.IndexMerge)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	*n = IndexMerge{
		Tenant:                  indexMerge.Tenant,
		TocWindowStartUnixNanos: indexMerge.ToCWindowStart,
		Runs:                    copyRunRefs(indexMerge.Runs),
	}
	return nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_LogMerge) UnmarshalPhysical(from physical.Node) error {
	n.LogMerge = new(LogMerge)
	return n.LogMerge.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *LogMerge) UnmarshalPhysical(from physical.Node) error {
	logMerge, ok := from.(*physical.LogMerge)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	*n = LogMerge{
		Tenant:                  logMerge.Tenant,
		TocWindowStartUnixNanos: logMerge.ToCWindowStart,
		Runs:                    copyRunRefs(logMerge.Runs),
	}
	return nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_IndexFilter) UnmarshalPhysical(from physical.Node) error {
	n.IndexFilter = new(IndexFilter)
	return n.IndexFilter.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *IndexFilter) UnmarshalPhysical(from physical.Node) error {
	indexFilter, ok := from.(*physical.IndexFilter)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	*n = IndexFilter{
		Tenant:          indexFilter.Tenant,
		SourceIndexPath: indexFilter.SourceIndexPath,
		ObjectPaths:     slices.Clone(indexFilter.ObjectPaths),
	}
	return nil
}
