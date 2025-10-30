package physical

import (
	"fmt"
	"slices"
)

// DataObjLocation is a string that uniquely indentifies a data object location in
// object storage.
type DataObjLocation string

// DataObjScan represents a physical plan operation for reading data objects.
// It contains information about the object location, stream IDs, projections,
// predicates for reading data from a data object.
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
	// Projections are used to limit the columns that are read to the ones
	// provided in the column expressions to reduce the amount of data that needs
	// to be processed.
	Projections []ColumnExpression
	// Predicates are used to filter rows to reduce the amount of rows that are
	// returned. Predicates would almost always contain a time range filter to
	// only read the logs for the requested time range.
	Predicates []Expression
}

// ID implements the [Node] interface.
// Returns a string that uniquely identifies the node in the plan.
func (s *DataObjScan) ID() string {
	if s.id == "" {
		return fmt.Sprintf("%p", s)
	}
	return s.id
}

// Clone returns a deep copy of the node (minus its ID).
func (s *DataObjScan) Clone() Node {
	return &DataObjScan{
		Location:    s.Location,
		Section:     s.Section,
		StreamIDs:   slices.Clone(s.StreamIDs),
		Projections: cloneExpressions(s.Projections),
		Predicates:  cloneExpressions(s.Predicates),
	}
}

// Type implements the [Node] interface.
// Returns the type of the node.
func (*DataObjScan) Type() NodeType {
	return NodeTypeDataObjScan
}
