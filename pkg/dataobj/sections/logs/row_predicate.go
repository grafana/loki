package logs

import (
	"time"
)

// RowPredicate is an expression used to filter rows in a data object.
type RowPredicate interface {
	isRowPredicate()
}

// Supported predicates.
type (
	// An AndRowPredicate is a RowPredicate which requires both its Left and
	// Right predicate to be true.
	AndRowPredicate struct{ Left, Right RowPredicate }

	// An OrRowPredicate is a RowPredicate which requires either its Left or
	// Right predicate to be true.
	OrRowPredicate struct{ Left, Right RowPredicate }

	// A NotRowPredicate is a RowPredicate which requires its Inner predicate to
	// be false.
	NotRowPredicate struct{ Inner RowPredicate }

	// A TimeRangeRowPredicate is a RowPredicate which requires the timestamp of
	// the entry to be within the range of StartTime and EndTime.
	TimeRangeRowPredicate struct {
		StartTime, EndTime time.Time
		IncludeStart       bool // Whether StartTime is inclusive.
		IncludeEnd         bool // Whether EndTime is inclusive.
	}

	// A LogMessageFilterRowPredicate is a RowPredicate that requires the log
	// message of the entry to pass a Keep function.
	LogMessageFilterRowPredicate struct {
		Keep func(line []byte) bool
	}

	// A MetadataMatcherRowPredicate is a RowPredicate that requires a metadata
	// key named Key to exist with a value of Value.
	MetadataMatcherRowPredicate struct{ Key, Value string }

	// A MetadataFilterRowPredicate is a RowPredicate that requires that metadata
	// with the provided key pass a Keep function.
	//
	// The key is provided to the keep function to allow the same function to be
	// used for multiple filter predicates.
	//
	// Uses of MetadataFilterRowPredicate are not eligible for page filtering and
	// should only be used when a condition cannot be expressed by other basic
	// predicates.
	MetadataFilterRowPredicate struct {
		Key  string
		Keep func(key, value string) bool
	}
)

func (AndRowPredicate) isRowPredicate()              {}
func (OrRowPredicate) isRowPredicate()               {}
func (NotRowPredicate) isRowPredicate()              {}
func (TimeRangeRowPredicate) isRowPredicate()        {}
func (MetadataMatcherRowPredicate) isRowPredicate()  {}
func (MetadataFilterRowPredicate) isRowPredicate()   {}
func (LogMessageFilterRowPredicate) isRowPredicate() {}
