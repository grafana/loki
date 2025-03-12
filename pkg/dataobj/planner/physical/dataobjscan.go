package physical

type DataObjLocation string

type Direction int8

const (
	Forward Direction = iota
	Backwards
)

// DataObjScan represents a physical plan operation for reading data objects.
// It contains information about the object location, stream IDs, projections,
// predicates, scan direction, and result limit for reading data from a data
// object.
type DataObjScan struct {
	id string

	Location   DataObjLocation
	StreamIDs  []int64
	Projection []ColumnExpression
	Predicates []Expression
	Direction  Direction
	Limit      uint32
}

func (s *DataObjScan) ID() string {
	return s.id
}

func (*DataObjScan) Type() NodeType {
	return NodeTypeDataObjScan
}

func (s *DataObjScan) Accept(v Visitor) error {
	return v.VisitDataObjScan(s)
}
