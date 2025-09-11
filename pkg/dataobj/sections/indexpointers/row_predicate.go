package indexpointers

import "time"

type (
	// RowPredicate is an expression used to filter rows in a data object.
	RowPredicate interface{ isRowPredicate() }
)

// Supported predicates.
type (
	// A TimeRangeRowPredicate is a RowPredicate which requires a start and end timestamp column to exist,
	// and for the timestamp to be within the range.
	TimeRangeRowPredicate struct{ Start, End time.Time }
)

func (TimeRangeRowPredicate) isRowPredicate() {}
