package pointers

import "time"

type (
	// RowPredicate is an expression used to filter rows in a data object.
	RowPredicate interface{ isRowPredicate() }
)

// Supported predicates.
type (
	// An AndRowPredicate is a RowPredicate which requires both its Left and
	// Right predicate to be true.
	AndRowPredicate struct{ Left, Right RowPredicate }

	// A BloomExistencePredicate is a RowPredicate which requires a bloom filter column named
	// Name to exist, and for the Value to pass the bloom filter.
	BloomExistencePredicate struct{ Name, Value string }

	// A TimeRangePredicate is a RowPredicate which requires the timestamp of
	// the entry to be within the range of StartTime and EndTime.
	TimeRangePredicate struct{ Start, End time.Time }
)

func (AndRowPredicate) isRowPredicate()         {}
func (BloomExistencePredicate) isRowPredicate() {}
func (TimeRangePredicate) isRowPredicate()      {}
