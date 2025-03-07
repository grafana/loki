package dataobj

import (
	"time"
)

type (
	// Predicate is an expression used to filter entries in a data object.
	Predicate interface {
		isPredicate()
	}

	// StreamsPredicate is a [Predicate] that can be used to filter streams in a
	// [StreamsReader].
	StreamsPredicate interface {
		Predicate
		predicateKind(StreamsPredicate)
	}

	// LogsPredicate is a [Predicate] that can be used to filter logs in a
	// [LogsReader].
	LogsPredicate interface {
		Predicate
		predicateKind(LogsPredicate)
	}
)

// Supported predicates.
type (
	// An AndPredicate is a Predicate which requires both its Left and Right
	// predicate to be true.
	AndPredicate[P Predicate] struct{ Left, Right P }

	// An OrPredicate is a Predicate which requires either its Left or Right
	// predicate to be true.
	OrPredicate[P Predicate] struct{ Left, Right P }

	// A NotPredicate is a Predicate which requires its Inner predicate to be
	// false.
	NotPredicate[P Predicate] struct{ Inner P }

	// A TimeRangePredicate is a Predicate which requires the timestamp of the
	// entry to be within the range of StartTime and EndTime.
	TimeRangePredicate[P Predicate] struct {
		StartTime, EndTime time.Time
		IncludeStart       bool // Whether StartTime is inclusive.
		IncludeEnd         bool // Whether EndTime is inclusive.
	}

	// A LabelMatcherPredicate is a [StreamsPredicate] which requires a label
	// named Name to exist with a value of Value.
	LabelMatcherPredicate struct{ Name, Value string }

	// A LabelFilterPredicate is a [StreamsPredicate] that requires that labels
	// with the provided name pass a Keep function.
	//
	// The name is is provided to the keep function to allow the same function to
	// be used for multiple filter predicates.
	//
	// Uses of LabelFilterPredicate are not eligible for page filtering and
	// should only be used when a condition cannot be expressed by other basic
	// predicates.
	LabelFilterPredicate struct {
		Name string
		Keep func(name, value string) bool
	}

	// A MetadataMatcherPredicate is a [LogsPredicate] that requires a metadata
	// key named Key to exist with a value of Value.
	MetadataMatcherPredicate struct{ Key, Value string }

	// A MetadataFilterPredicate is a [LogsPredicate] that requires that metadata
	// with the provided key pass a Keep function.
	//
	// The key is provided to the keep function to allow the same function to be used
	// for multiple filter predicates.
	//
	// Uses of MetadataFilterPredicate are not eligible for page filtering and
	// should only be used when a condition cannot be expressed by other basic
	// predicates.
	MetadataFilterPredicate struct {
		Key  string
		Keep func(key, value string) bool
	}
)

func (AndPredicate[P]) isPredicate()          {}
func (OrPredicate[P]) isPredicate()           {}
func (NotPredicate[P]) isPredicate()          {}
func (TimeRangePredicate[P]) isPredicate()    {}
func (LabelMatcherPredicate) isPredicate()    {}
func (LabelFilterPredicate) isPredicate()     {}
func (MetadataMatcherPredicate) isPredicate() {}
func (MetadataFilterPredicate) isPredicate()  {}

func (AndPredicate[P]) predicateKind(P)                      {}
func (OrPredicate[P]) predicateKind(P)                       {}
func (NotPredicate[P]) predicateKind(P)                      {}
func (TimeRangePredicate[P]) predicateKind(P)                {}
func (LabelMatcherPredicate) predicateKind(StreamsPredicate) {}
func (LabelFilterPredicate) predicateKind(StreamsPredicate)  {}
func (MetadataMatcherPredicate) predicateKind(LogsPredicate) {}
func (MetadataFilterPredicate) predicateKind(LogsPredicate)  {}
