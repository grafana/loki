package physical

type SortOrder int8

const (
	DESC SortOrder = iota - 1
	_
	ASC
)

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

	Column ColumnExpression
	Order  SortOrder
}

func (m *SortMerge) ID() string {
	return m.id
}

func (*SortMerge) Type() NodeType {
	return NodeTypeSortMerge
}

func (m *SortMerge) Accept(v Visitor) error {
	return v.VisitSortMerge(m)
}
