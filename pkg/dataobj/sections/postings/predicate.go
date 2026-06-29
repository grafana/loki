package postings

import (
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/prometheus/prometheus/model/labels"
)

// Predicate is an expression used to filter column values in a [Reader].
//
// Predicates in the postings package mirror those in the pointers package
// by design. The duplication is intentional and
// time-bounded: both copies vanish together when the legacy pointers reader
// is deleted. Do not extract these into a shared internal package.
//
// Bloom-filter and regex matching are modeled as section-level predicates
// ([BloomMatchPredicate], [RegexMatchPredicate]) rather than dataset
// predicates: the dataset layer has no concept of a bloom filter or a regex.
// They translate to a [dataset.FuncPredicate] on pushdown (see mapPredicate),
// keeping that domain knowledge out of the dataset package.
type Predicate interface{ isPredicate() }

// Supported predicates.
type (
	// An AndPredicate is a [Predicate] which asserts that a row may only be
	// included if both the Left and Right Predicate are true.
	AndPredicate struct{ Left, Right Predicate }

	// An OrPredicate is a [Predicate] which asserts that a row may only be
	// included if either the Left or Right Predicate are true.
	OrPredicate struct{ Left, Right Predicate }

	// A NotPredicate is a [Predicate] which asserts that a row may only be
	// included if the inner Predicate is false.
	NotPredicate struct{ Inner Predicate }

	// TruePredicate is a [Predicate] which always returns true.
	TruePredicate struct{}

	// FalsePredicate is a [Predicate] which always returns false.
	FalsePredicate struct{}

	// An EqualPredicate is a [Predicate] which asserts that a row may only be
	// included if the Value of the Column is equal to the Value.
	EqualPredicate struct {
		Column *Column       // Column to check.
		Value  scalar.Scalar // Value to check equality for.
	}

	// An InPredicate is a [Predicate] which asserts that a row may only be
	// included if the Value of the Column is present in the provided Values.
	InPredicate struct {
		Column *Column         // Column to check.
		Values []scalar.Scalar // Values to check for inclusion.
	}

	// A GreaterThanPredicate is a [Predicate] which asserts that a row may only
	// be included if the Value of the Column is greater than the provided Value.
	GreaterThanPredicate struct {
		Column *Column       // Column to check.
		Value  scalar.Scalar // Value for which rows in Column must be greater than.
	}

	// A LessThanPredicate is a [Predicate] which asserts that a row may only be
	// included if the Value of the Column is less than the provided Value.
	LessThanPredicate struct {
		Column *Column       // Column to check.
		Value  scalar.Scalar // Value for which rows in Column must be less than.
	}

	// FuncPredicate is a [Predicate] which asserts that a row may only be
	// included if the Value of the Column passes the Keep function.
	//
	// Instances of FuncPredicate are ineligible for page filtering and should
	// only be used when there isn't a more explicit Predicate implementation.
	FuncPredicate struct {
		Column *Column // Column to check.

		// Keep is invoked with the column and value pair to check. Keep is given
		// the Column instance to allow for reusing the same function across
		// multiple columns, if necessary.
		//
		// If Keep returns true, the row is kept.
		Keep func(column *Column, value scalar.Scalar) bool
	}

	// BloomMatchPredicate is a [Predicate] which asserts that a row may only be
	// included if the bloom filter stored in Column's binary value tests positive
	// for Value.
	//
	// Instances of BloomMatchPredicate are ineligible for page filtering: a bloom
	// blob has no min/max page statistics to prune on. On pushdown it translates
	// to a [dataset.FuncPredicate] that deserializes the bloom blob and tests it.
	BloomMatchPredicate struct {
		Column *Column
		Value  []byte
	}

	// RegexMatchPredicate is a [Predicate] which asserts that a row may only be
	// included if Column's string value matches Matcher.
	//
	// Instances of RegexMatchPredicate are ineligible for page filtering: a regex
	// has no min/max page statistics to prune on. On pushdown it translates to a
	// [dataset.FuncPredicate] that runs the matcher against the column value.
	RegexMatchPredicate struct {
		Column  *Column
		Matcher *labels.FastRegexMatcher
	}
)

func (AndPredicate) isPredicate()         {}
func (OrPredicate) isPredicate()          {}
func (NotPredicate) isPredicate()         {}
func (TruePredicate) isPredicate()        {}
func (FalsePredicate) isPredicate()       {}
func (EqualPredicate) isPredicate()       {}
func (InPredicate) isPredicate()          {}
func (GreaterThanPredicate) isPredicate() {}
func (LessThanPredicate) isPredicate()    {}
func (FuncPredicate) isPredicate()        {}
func (BloomMatchPredicate) isPredicate()  {}
func (RegexMatchPredicate) isPredicate()  {}

// walkPredicate traverses a predicate in depth-first order: it starts by
// calling fn(p). If fn(p) returns true, walkPredicate is invoked recursively
// with fn for each of the non-nil children of p, followed by a call of
// fn(nil).
func walkPredicate(p Predicate, fn func(Predicate) bool) {
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

	case TruePredicate: // No children.
	case FalsePredicate: // No children.
	case EqualPredicate: // No children.
	case InPredicate: // No children.
	case GreaterThanPredicate: // No children.
	case LessThanPredicate: // No children.
	case FuncPredicate: // No children.
	case BloomMatchPredicate: // No children.
	case RegexMatchPredicate: // No children.

	default:
		panic("postings.walkPredicate: unsupported predicate type")
	}

	fn(nil)
}
