package physical

import "fmt"

// DataObjLocation is a string that uniquely indentifies a data object location in
// object storage.
type DataObjLocation string

// DataObjScan represents a physical plan operation for reading data objects.
// It contains information about the object location, stream IDs, projections,
// predicates, scan direction, and result limit for reading data from a data
// object.
type DataObjScan struct {
	id string

	// Location is the unique name of the data object that is used as source for
	// reading streams.
	Location DataObjLocation
	// Section is the section index inside the data object to scan.
	Section int
	// StreamIDs is a set of stream IDs inside the data object. These IDs are
	// only unique in the context of a single data object.
	StreamIDs []int64
	// TimeRange is a pair of time.Time values representing the
	// start and end time of the relevant data.
	TimeRange TimeRange
	// Projections are used to limit the columns that are read to the ones
	// provided in the column expressions to reduce the amount of data that needs
	// to be processed.
	Projections []ColumnExpression
	// Predicates are used to filter rows to reduce the amount of rows that are
	// returned. Predicates would almost always contain a time range filter to
	// only read the logs for the requested time range.
	Predicates []Expression
	// Direction defines in what order columns are read.
	Direction SortOrder
	// Limit is used to stop scanning the data object once it is reached.
	Limit uint32
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (s *DataObjScan) ID() string {
	if s.id == "" {
		return fmt.Sprintf("%p", s)
	}
	return s.id
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*DataObjScan) Type() NodeType {
	return NodeTypeDataObjScan
}

// Accept implements the [Node] interface.
// Dispatches itself to the provided [Visitor] v
func (s *DataObjScan) Accept(v Visitor) error {
	return v.VisitDataObjScan(s)
}
