package physical

type SortOrder uint8

const (
	ASC SortOrder = iota
	DESC
)

// String returns the string representation of the [SortOrder].
func (o SortOrder) String() string {
	switch o {
	case DESC:
		return "DESC"
	case ASC:
		return "ASC"
	default:
		return "UNDEFINED"
	}
}

// SortMerge represents a sort+merge operation in the physical plan. It
// performs sorting of data based on the specified Column and Order direction.
type SortMerge struct {
	id string

	// Column defines the column expression by which the rows should be sorted.
	// This is almost always the timestamp column, because it is the column
	// by which the results of the DataObjScan node are sorted. This allows
	// for sorting and merging multiple already sorted inputs from the DataObjScan
	// without being a pipeline breaker.
	Column ColumnExpression
	// Order defines whether the column should be sorted in ascending or
	// descending order. Must match the read direction of the DataObjScan that
	// feeds into the SortMerge.
	Order SortOrder
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (m *SortMerge) ID() string {
	return m.id
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*SortMerge) Type() NodeType {
	return NodeTypeSortMerge
}

// Accept implements the [Node] interface.
// Dispatches itself to the provided [Visitor] v
func (m *SortMerge) Accept(v Visitor) error {
	return v.VisitSortMerge(m)
}
