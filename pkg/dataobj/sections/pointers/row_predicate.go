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

	// A BloomExistenceRowPredicate is a RowPredicate which requires a bloom filter column named
	// Name to exist, and for the Value to pass the bloom filter.
	BloomExistenceRowPredicate struct{ Name, Value string }

	// A TimeRangeRowPredicate is a RowPredicate which requires the timestamp of
	// the entry to be within the range of StartTime and EndTime.
	TimeRangeRowPredicate struct{ Start, End time.Time }
)

func (AndRowPredicate) isRowPredicate()            {}
func (BloomExistenceRowPredicate) isRowPredicate() {}
func (TimeRangeRowPredicate) isRowPredicate()      {}
