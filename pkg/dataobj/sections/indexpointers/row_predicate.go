package indexpointers

import "time"

type (
	// RowPredicate is an expression used to filter rows in a data object.
	RowPredicate interface{ isRowPredicate() }
)

// Supported predicates.
type (
	// A TimeRangePredicate is a RowPredicate which requires a min and max timestamp column to exist,
	// and for the timestamp to be within the range.
	TimeRangePredicate struct{ MinTimestamp, MaxTimestamp time.Time }
)

func (TimeRangePredicate) isRowPredicate() {}
