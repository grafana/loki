package physicalpb

import (
	fmt "fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/expressionpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/ulid"
)

type unmarshaler interface {
	UnmarshalPhysical(node physical.Node) error
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node) UnmarshalPhysical(from physical.Node) error {
	switch from.(type) {
	case *physical.RangeAggregation:
		n.Kind = &Node_AggregateRange{}
	case *physical.VectorAggregation:
		n.Kind = &Node_AggregateVector{}
	case *physical.DataObjScan:
		n.Kind = &Node_Scan{}
	case *physical.Filter:
		n.Kind = &Node_Filter{}
	case *physical.Limit:
		n.Kind = &Node_Limit{}
	case *physical.Projection:
		n.Kind = &Node_Projection{}
	case *physical.ColumnCompat:
		n.Kind = &Node_ColumnCompat{}
	case *physical.ScanSet:
		n.Kind = &Node_ScanSet{}
	case *physical.TopK:
		n.Kind = &Node_TopK{}
	case *physical.Parallelize:
		n.Kind = &Node_Parallelize{}
	case *physical.Join:
		n.Kind = &Node_Join{}
	case *physical.Merge:
		n.Kind = &Node_Merge{}
	case *physical.PointersScan:
		n.Kind = &Node_PointersScan{}
	default:
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	// Set the NodeID from the physical node using the ULID() method
	n.Id = NodeID{Value: ulid.ULID(from.ID())}

	u, ok := n.Kind.(unmarshaler)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}
	return u.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_AggregateRange) UnmarshalPhysical(from physical.Node) error {
	n.AggregateRange = new(AggregateRange)
	return n.AggregateRange.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_AggregateVector) UnmarshalPhysical(from physical.Node) error {
	n.AggregateVector = new(AggregateVector)
	return n.AggregateVector.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_Scan) UnmarshalPhysical(from physical.Node) error {
	n.Scan = new(DataObjScan)
	return n.Scan.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_Filter) UnmarshalPhysical(from physical.Node) error {
	n.Filter = new(Filter)
	return n.Filter.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_Limit) UnmarshalPhysical(from physical.Node) error {
	n.Limit = new(Limit)
	return n.Limit.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_Projection) UnmarshalPhysical(from physical.Node) error {
	n.Projection = new(Projection)
	return n.Projection.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_ColumnCompat) UnmarshalPhysical(from physical.Node) error {
	n.ColumnCompat = new(ColumnCompat)
	return n.ColumnCompat.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_ScanSet) UnmarshalPhysical(from physical.Node) error {
	n.ScanSet = new(ScanSet)
	return n.ScanSet.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_TopK) UnmarshalPhysical(from physical.Node) error {
	n.TopK = new(TopK)
	return n.TopK.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_Parallelize) UnmarshalPhysical(from physical.Node) error {
	n.Parallelize = new(Parallelize)
	return n.Parallelize.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_Join) UnmarshalPhysical(from physical.Node) error {
	n.Join = new(Join)
	return n.Join.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_Merge) UnmarshalPhysical(from physical.Node) error {
	n.Merge = new(Merge)
	return n.Merge.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Node_PointersScan) UnmarshalPhysical(from physical.Node) error {
	n.PointersScan = new(PointersScan)
	return n.PointersScan.UnmarshalPhysical(from)
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *AggregateRange) UnmarshalPhysical(from physical.Node) error {
	rangeAgg, ok := from.(*physical.RangeAggregation)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	grouping, err := unmarshalGrouping(rangeAgg.Grouping)
	if err != nil {
		return err
	}

	var op AggregateRangeOp
	if err := op.unmarshalType(rangeAgg.Operation); err != nil {
		return err
	}

	*n = AggregateRange{
		Grouping:  grouping,
		Operation: op,
		Start:     rangeAgg.Start,
		End:       rangeAgg.End,
		Step:      rangeAgg.Step,
		Range:     rangeAgg.Range,
	}

	return nil
}

func unmarshalGrouping(g physical.Grouping) (*Grouping, error) {
	columns, err := unmarshalColumnExpressions(g.Columns)
	if err != nil {
		return nil, err
	}

	return &Grouping{
		Columns: columns,
		Without: g.Without,
	}, nil
}

func unmarshalColumnExpressions(from []physical.ColumnExpression) ([]*expressionpb.ColumnExpression, error) {
	if from == nil {
		return nil, nil
	}

	out := make([]*expressionpb.ColumnExpression, len(from))
	for i, expr := range from {
		out[i] = new(expressionpb.ColumnExpression)
		if err := out[i].UnmarshalPhysical(expr); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *AggregateVector) UnmarshalPhysical(from physical.Node) error {
	vectorAgg, ok := from.(*physical.VectorAggregation)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	grouping, err := unmarshalGrouping(vectorAgg.Grouping)
	if err != nil {
		return err
	}

	var op AggregateVectorOp
	if err := op.unmarshalType(vectorAgg.Operation); err != nil {
		return err
	}

	*n = AggregateVector{
		Grouping:  grouping,
		Operation: op,
	}
	return nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *DataObjScan) UnmarshalPhysical(from physical.Node) error {
	scan, ok := from.(*physical.DataObjScan)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	projections, err := unmarshalColumnExpressions(scan.Projections)
	if err != nil {
		return err
	}

	predicates, err := unmarshalExpressions(scan.Predicates)
	if err != nil {
		return err
	}

	*n = DataObjScan{
		Location:     string(scan.Location),
		Section:      int64(scan.Section),
		StreamIds:    scan.StreamIDs,
		Projections:  projections,
		Predicates:   predicates,
		MaxTimeRange: unmarshalTimeRange(scan.MaxTimeRange),
	}
	return nil
}

func unmarshalTimeRange(from physical.TimeRange) *TimeRange {
	return &TimeRange{
		Start: from.Start,
		End:   from.End,
	}
}

func unmarshalExpressions(from []physical.Expression) ([]*expressionpb.Expression, error) {
	if from == nil {
		return nil, nil
	}

	out := make([]*expressionpb.Expression, len(from))
	for i, expr := range from {
		out[i] = new(expressionpb.Expression)
		if err := out[i].UnmarshalPhysical(expr); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func unmarshalExpression(from physical.Expression) (*expressionpb.Expression, error) {
	if from == nil {
		return nil, nil
	}

	out := new(expressionpb.Expression)
	if err := out.UnmarshalPhysical(from); err != nil {
		return nil, err
	}
	return out, nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Filter) UnmarshalPhysical(from physical.Node) error {
	filter, ok := from.(*physical.Filter)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	predicates, err := unmarshalExpressions(filter.Predicates)
	if err != nil {
		return err
	}

	*n = Filter{
		Predicates: predicates,
	}
	return nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Limit) UnmarshalPhysical(from physical.Node) error {
	limit, ok := from.(*physical.Limit)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	*n = Limit{
		Skip:  limit.Skip,
		Fetch: limit.Fetch,
	}
	return nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Projection) UnmarshalPhysical(from physical.Node) error {
	projection, ok := from.(*physical.Projection)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	expressions, err := unmarshalExpressions(projection.Expressions)
	if err != nil {
		return err
	}

	*n = Projection{
		Expressions: expressions,
		All:         projection.All,
		Expand:      projection.Expand,
		Drop:        projection.Drop,
	}
	return nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *ColumnCompat) UnmarshalPhysical(from physical.Node) error {
	compat, ok := from.(*physical.ColumnCompat)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	var (
		source      expressionpb.ColumnType
		destination expressionpb.ColumnType
	)

	if err := source.UnmarshalType(compat.Source); err != nil {
		return err
	} else if err := destination.UnmarshalType(compat.Destination); err != nil {
		return err
	}

	collisions := make([]expressionpb.ColumnType, len(compat.Collisions))
	for i, ct := range compat.Collisions {
		if err := collisions[i].UnmarshalType(ct); err != nil {
			return err
		}
	}

	*n = ColumnCompat{
		Source:      source,
		Destination: destination,
		Collisions:  collisions,
	}
	return nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *ScanSet) UnmarshalPhysical(from physical.Node) error {
	scanSet, ok := from.(*physical.ScanSet)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	projections, err := unmarshalColumnExpressions(scanSet.Projections)
	if err != nil {
		return err
	}

	predicates, err := unmarshalExpressions(scanSet.Predicates)
	if err != nil {
		return err
	}

	targets := make([]*ScanTarget, len(scanSet.Targets))
	for i, target := range scanSet.Targets {
		targets[i] = new(ScanTarget)
		if err := targets[i].UnmarshalPhysical(target); err != nil {
			return err
		}
	}

	*n = ScanSet{
		Targets:     targets,
		Projections: projections,
		Predicates:  predicates,
	}
	return nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *ScanTarget) UnmarshalPhysical(from *physical.ScanTarget) error {
	switch from.Type {
	case physical.ScanTypeDataObject:
		if from.DataObject == nil {
			return fmt.Errorf("DataObject is nil for ScanTypeDataObject")
		}
		n.Kind = &ScanTarget_DataObject{DataObject: new(DataObjScan)}
		return n.GetDataObject().UnmarshalPhysical(from.DataObject)
	case physical.ScanTypePointers:
		if from.Pointers == nil {
			return fmt.Errorf("pointers is nil for ScanTypePointers")
		}
		n.Kind = &ScanTarget_Pointers{Pointers: new(PointersScan)}
		return n.GetPointers().UnmarshalPhysical(from.Pointers)
	default:
		return fmt.Errorf("unsupported scan type: %s", from.Type)
	}
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *TopK) UnmarshalPhysical(from physical.Node) error {
	topK, ok := from.(*physical.TopK)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	sortBy := new(expressionpb.ColumnExpression)
	if err := sortBy.UnmarshalPhysical(topK.SortBy); err != nil {
		return err
	}

	*n = TopK{
		SortBy:     sortBy,
		Ascending:  topK.Ascending,
		NullsFirst: topK.NullsFirst,
		K:          int64(topK.K),
	}
	return nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Parallelize) UnmarshalPhysical(from physical.Node) error {
	_, ok := from.(*physical.Parallelize)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	*n = Parallelize{}
	return nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Join) UnmarshalPhysical(from physical.Node) error {
	_, ok := from.(*physical.Join)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	*n = Join{}
	return nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *Merge) UnmarshalPhysical(from physical.Node) error {
	_, ok := from.(*physical.Merge)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	*n = Merge{}
	return nil
}

// UnmarshalPhysical reads from into n. Returns an error if the conversion fails
// or is unsupported.
func (n *PointersScan) UnmarshalPhysical(from physical.Node) error {
	scan, ok := from.(*physical.PointersScan)
	if !ok {
		return fmt.Errorf("unsupported physical node type: %T", from)
	}

	selector, err := unmarshalExpression(scan.Selector)
	if err != nil {
		return err
	}

	predicates, err := unmarshalExpressions(scan.Predicates)
	if err != nil {
		return err
	}

	*n = PointersScan{
		Location:     string(scan.Location),
		Selector:     selector,
		Predicates:   predicates,
		Start:        scan.Start,
		End:          scan.End,
		MaxTimeRange: unmarshalTimeRange(scan.MaxTimeRange),
	}
	return nil
}
