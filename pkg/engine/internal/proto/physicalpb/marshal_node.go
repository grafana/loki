package physicalpb

import (
	fmt "fmt"

	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/expressionpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
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

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_AggregateRange) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.AggregateRange.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_AggregateVector) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.AggregateVector.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_Scan) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.Scan.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_Filter) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.Filter.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_Limit) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.Limit.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_Projection) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.Projection.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_ColumnCompat) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.ColumnCompat.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_ScanSet) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.ScanSet.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_TopK) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.TopK.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_Parallelize) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.Parallelize.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_Join) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.Join.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_Merge) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.Merge.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf node into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Node_PointersScan) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return n.PointersScan.MarshalPhysical(nodeID)
}

// MarshalPhysical converts a protobuf AggregateRange into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *AggregateRange) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	operation, err := n.Operation.marshalType()
	if err != nil {
		return nil, err
	}

	grouping, err := marshalGrouping(n.Grouping)
	if err != nil {
		return nil, err
	}

	return &physical.RangeAggregation{
		NodeID: nodeID,

		Grouping:  grouping,
		Operation: operation,
		Start:     n.Start,
		End:       n.End,
		Step:      n.Step,
		Range:     n.Range,
	}, nil
}

func marshalGrouping(g *Grouping) (physical.Grouping, error) {
	if g == nil {
		return physical.Grouping{}, fmt.Errorf("empty grouping")
	}

	return physical.Grouping{
		Columns: marshalColumnExpressions(g.Columns),
		Without: g.Without,
	}, nil
}

func marshalColumnExpressions(exprs []*expressionpb.ColumnExpression) []physical.ColumnExpression {
	if exprs == nil {
		return nil
	}

	out := make([]physical.ColumnExpression, len(exprs))
	for i, expr := range exprs {
		columnExpression, err := expr.MarshalPhysical()
		if err != nil {
			return nil
		}
		out[i] = columnExpression.(physical.ColumnExpression)
	}
	return out
}

// MarshalPhysical converts a protobuf AggregateVector into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *AggregateVector) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	operation, err := n.Operation.marshalType()
	if err != nil {
		return nil, err
	}

	grouping, err := marshalGrouping(n.Grouping)
	if err != nil {
		return nil, err
	}

	return &physical.VectorAggregation{
		NodeID: nodeID,

		Grouping:  grouping,
		Operation: operation,
	}, nil
}

// MarshalPhysical converts a protobuf DataObjScan into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *DataObjScan) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return &physical.DataObjScan{
		NodeID: nodeID,

		Location:     physical.DataObjLocation(n.Location),
		Section:      int(n.Section),
		StreamIDs:    n.StreamIds,
		Projections:  marshalColumnExpressions(n.Projections),
		Predicates:   marshalExpressions(n.Predicates),
		MaxTimeRange: marshalTimeRange(n.MaxTimeRange),
	}, nil
}

func marshalTimeRange(timeRange *TimeRange) physical.TimeRange {
	if timeRange == nil {
		return physical.TimeRange{}
	}

	return physical.TimeRange{
		Start: timeRange.Start,
		End:   timeRange.End,
	}
}

func marshalExpressions(exprs []*expressionpb.Expression) []physical.Expression {
	if exprs == nil {
		return nil
	}

	out := make([]physical.Expression, len(exprs))
	for i, expr := range exprs {
		expression, err := expr.MarshalPhysical()
		if err != nil {
			return nil
		}
		out[i] = expression
	}
	return out
}

func marshalExpression(expr *expressionpb.Expression) (physical.Expression, error) {
	if expr == nil {
		return nil, nil
	}
	return expr.MarshalPhysical()
}

// MarshalPhysical converts a protobuf Filter into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Filter) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return &physical.Filter{
		NodeID: nodeID,

		Predicates: marshalExpressions(n.Predicates),
	}, nil
}

// MarshalPhysical converts a protobuf Limit into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Limit) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return &physical.Limit{
		NodeID: nodeID,

		Skip:  n.Skip,
		Fetch: n.Fetch,
	}, nil
}

// MarshalPhysical converts a protobuf Projection into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Projection) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return &physical.Projection{
		NodeID: nodeID,

		Expressions: marshalExpressions(n.Expressions),
		All:         n.All,
		Expand:      n.Expand,
		Drop:        n.Drop,
	}, nil
}

// MarshalPhysical converts a protobuf ColumnCompat into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *ColumnCompat) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	source, err := n.Source.MarshalType()
	if err != nil {
		return nil, err
	}

	destination, err := n.Destination.MarshalType()
	if err != nil {
		return nil, err
	}

	collisions := make([]types.ColumnType, len(n.Collisions))
	for i, collision := range n.Collisions {
		ct, err := collision.MarshalType()
		if err != nil {
			return nil, err
		}
		collisions[i] = ct
	}

	return &physical.ColumnCompat{
		NodeID: nodeID,

		Source:      source,
		Destination: destination,
		Collisions:  collisions,
	}, nil
}

// MarshalPhysical converts a protobuf ScanSet into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *ScanSet) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	targets := make([]*physical.ScanTarget, len(n.Targets))
	for i, t := range n.Targets {
		target, err := t.MarshalPhysical()
		if err != nil {
			return nil, err
		}
		targets[i] = target
	}

	return &physical.ScanSet{
		NodeID: nodeID,

		Targets:     targets,
		Projections: marshalColumnExpressions(n.Projections),
		Predicates:  marshalExpressions(n.Predicates),
	}, nil
}

// MarshalPhysical converts a protobuf ScanTarget into a physical plan scan target. Returns
// an error if the conversion fails or is unsupported.
func (n *ScanTarget) MarshalPhysical() (*physical.ScanTarget, error) {
	target := &physical.ScanTarget{}

	switch {
	case n.GetDataObject() != nil:
		target.Type = physical.ScanTypeDataObject

		// Targets aren't real nodes, so they don't get a real ULID.
		dataObj, err := n.GetDataObject().MarshalPhysical(ulid.Zero)
		if err != nil {
			return nil, err
		}
		target.DataObject = dataObj.(*physical.DataObjScan)
		return target, nil

	case n.GetPointers() != nil:
		target.Type = physical.ScanTypePointers

		// Targets aren't real nodes, so they don't get a real ULID.
		ptrScan, err := n.GetPointers().MarshalPhysical(ulid.Zero)
		if err != nil {
			return nil, err
		}
		target.Pointers = ptrScan.(*physical.PointersScan)
		return target, nil

	default:
		return nil, fmt.Errorf("unsupported scan target: no kind set")
	}
}

// MarshalPhysical converts a protobuf TopK into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *TopK) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	sortBy, err := n.SortBy.MarshalPhysical()
	if err != nil {
		return nil, err
	}

	return &physical.TopK{
		NodeID: nodeID,

		SortBy:     sortBy.(physical.ColumnExpression),
		Ascending:  n.Ascending,
		NullsFirst: n.NullsFirst,
		K:          int(n.K),
	}, nil
}

// MarshalPhysical converts a protobuf Parallelize into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Parallelize) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return &physical.Parallelize{NodeID: nodeID}, nil
}

// MarshalPhysical converts a protobuf Join into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Join) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return &physical.Join{NodeID: nodeID}, nil
}

// MarshalPhysical converts a protobuf Merge into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *Merge) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	return &physical.Merge{NodeID: nodeID}, nil
}

// MarshalPhysical converts a protobuf PointersScan into a physical plan node. Returns
// an error if the conversion fails or is unsupported.
func (n *PointersScan) MarshalPhysical(nodeID ulid.ULID) (physical.Node, error) {
	selector, err := marshalExpression(n.Selector)
	if err != nil {
		return nil, err
	}

	return &physical.PointersScan{
		NodeID: nodeID,

		Location:     physical.DataObjLocation(n.Location),
		Selector:     selector,
		Predicates:   marshalExpressions(n.Predicates),
		Start:        n.Start,
		End:          n.End,
		MaxTimeRange: marshalTimeRange(n.MaxTimeRange),
	}, nil
}
