package dataobj

import (
	"fmt"
	"time"
)

// Predicate is an expression used to filter entries in a [LogsReader] or
// [StreamsReader].
type Predicate interface{ isPredicate() }

// Supported predicates.
type (
	// An AndPredicate is a Predicate which requires both its Left and Right
	// predicate to be true.
	AndPredicate struct{ Left, Right Predicate }

	// An OrPredicate is a Predicate which requires either its Left or Right
	// predicate to be true.
	OrPredicate struct{ Left, Right Predicate }

	// A NotPredicate is a Predicate which requires its Inner predicate to be
	// false.
	NotPredicate struct{ Inner Predicate }

	// A TimeRangePredicate is a Predicate which requires the timestamp of the
	// entry to be within the range of StartTime and EndTime.
	TimeRangePredicate struct {
		StartTime, EndTime time.Time
		IncludeStart       bool // Whether StartTime is inclusive.
		IncludeEnd         bool // Whether EndTime is inclusive.
	}

	// A LabelMatcherPredicate requires a label named Name to exist with a value
	// of Value.
	//
	// LabelMatcherPredicate can only be used with [StreamsReader].
	LabelMatcherPredicate struct{ Name, Value string }

	// A LabelFilterPredicate requires that labels with the provided name pass a
	// Keep function.
	//
	// The name is is provided to the keep function to allow the same function to
	// be used for multiple filter predicates.
	//
	// LabelFilterPredicate can only be used with [StreamsReader].
	LabelFilterPredicate struct {
		Name string
		Keep func(name, value string) bool
	}

	// A MetadataMatcherPredicate requires a metadata key named Key to exist with
	// a value of Value.
	//
	// MetadataMatcherPredicate can only be used with [LogsReader].
	MetadataMatcherPredicate struct{ Key, Value string }

	// A MetadataFilterPredicate requires that metadata with the provided key pass a
	// Keep function.
	//
	// The key is provided to the keep function to allow the same function to be used
	// for multiple filter predicates.
	//
	// MetadataFilterPredicate can only be used with [LogsReader].
	MetadataFilterPredicate struct {
		Key  string
		Keep func(key, value string) bool
	}
)

func (AndPredicate) isPredicate()             {}
func (OrPredicate) isPredicate()              {}
func (NotPredicate) isPredicate()             {}
func (TimeRangePredicate) isPredicate()       {}
func (LabelMatcherPredicate) isPredicate()    {}
func (LabelFilterPredicate) isPredicate()     {}
func (MetadataMatcherPredicate) isPredicate() {}
func (MetadataFilterPredicate) isPredicate()  {}

// walkPredicate traverses a predicate in depth-first order: it starts by
// calling fn(p). If fn(p) returns true, walkPredicate is invoked recursively
// with fn for each of the non-nil children of p, followed by a call of
// fn(nil).
func walkPredicate(p Predicate, fn func(p Predicate) bool) {
	if p == nil || !fn(p) {
		return
	}

	switch p := p.(type) {
	case AndPredicate:
		walkPredicate(p.Left, fn)
		walkPredicate(p.Right, fn)

	case OrPredicate:
		walkPredicate(p.Left, fn)
		walkPredicate(p.Right, fn)

	case NotPredicate:
		walkPredicate(p.Inner, fn)

	case TimeRangePredicate: // No children.
	case LabelMatcherPredicate: // No children.
	case LabelFilterPredicate: // No children.
	case MetadataMatcherPredicate: // No children.
	case MetadataFilterPredicate: // No children.

	default:
		panic(fmt.Sprintf("dataobj.walkPredicate: unsupported predicate type %T", p))
	}

	fn(nil)
}
