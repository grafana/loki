package streams

import (
	"time"
)

type (
	// RowPredicate is an expression used to filter rows in a data object.
	RowPredicate interface{ isRowPredicate() }
)

// Supported predicates.
type (
	// An AndRowPredicate is a RowPredicate which requires both its Left and
	// Right
	// predicate to be true.
	AndRowPredicate struct{ Left, Right RowPredicate }

	// An OrRowPredicate is a RowPredicate which requires either its Left or
	// Right predicate to be true.
	OrRowPredicate struct{ Left, Right RowPredicate }

	// A NotRowPredicate is a RowPredicate which requires its Inner predicate to be
	// false.
	NotRowPredicate struct{ Inner RowPredicate }

	// A TimeRangeRowPredicate is a RowPredicate which requires the timestamp of
	// the entry to be within the range of StartTime and EndTime.
	TimeRangeRowPredicate struct {
		StartTime, EndTime time.Time
		IncludeStart       bool // Whether StartTime is inclusive.
		IncludeEnd         bool // Whether EndTime is inclusive.
	}

	// A LabelMatcherRowPredicate is a RowPredicate which requires a label named
	// Name to exist with a value of Value.
	LabelMatcherRowPredicate struct{ Name, Value string }

	// A LabelFilterRowPredicate is a RowPredicate that requires that labels with
	// the provided name pass a Keep function.
	//
	// The name is is provided to the keep function to allow the same function to
	// be used for multiple filter predicates.
	//
	// Uses of LabelFilterRowPredicate are not eligible for page filtering and
	// should only be used when a condition cannot be expressed by other basic
	// predicates.
	LabelFilterRowPredicate struct {
		Name string
		Keep func(name, value string) bool
	}
)

func (AndRowPredicate) isRowPredicate()          {}
func (OrRowPredicate) isRowPredicate()           {}
func (NotRowPredicate) isRowPredicate()          {}
func (TimeRangeRowPredicate) isRowPredicate()    {}
func (LabelMatcherRowPredicate) isRowPredicate() {}
func (LabelFilterRowPredicate) isRowPredicate()  {}
