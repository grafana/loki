package logs

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow/scalar"
)

// Predicate is an expression used to filter column values in a [Reader].
type Predicate interface{ isPredicate() }

// Supported predicates.
type (
	// An AndPredicate is a [Predicate] which asserts that a row may only be
	// included if both the Left and Right Predicate are true.
	AndPredicate struct{ Left, Right Predicate }

	// An OrPredicate is a [Predicate] which asserts that a row may only be
	// included if either the Left or Right Predicate are true.
	OrPredicate struct{ Left, Right Predicate }

	// A NotePredicate is a [Predicate] which asserts that a row may only be
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

	default:
		panic("logs.walkPredicate: unsupported predicate type")
	}

	fn(nil)
}

// predicateColumns returns a slice of all columns referenced in the given predicates.
// It ensures that each column is only included once, even if it appears in multiple predicates.
func predicateColumns(predicates []Predicate) []*Column {
	exists := make(map[*Column]struct{})
	columns := make([]*Column, 0, len(predicates))

	// append column if it is not a duplicate.
	appendColumn := func(c *Column) {
		if _, ok := exists[c]; ok {
			return
		}

		columns = append(columns, c)
		exists[c] = struct{}{}
	}

	for _, p := range predicates {
		walkPredicate(p, func(p Predicate) bool {
			switch p := p.(type) {
			case nil: // End of walk; nothing to do.

			case AndPredicate: // Nothing to do.
			case OrPredicate: // Nothing to do.
			case NotPredicate: // Nothing to do.
			case TruePredicate: // Nothing to do.
			case FalsePredicate: // Nothing to do.

			case EqualPredicate:
				appendColumn(p.Column)
			case InPredicate:
				appendColumn(p.Column)
			case GreaterThanPredicate:
				appendColumn(p.Column)
			case LessThanPredicate:
				appendColumn(p.Column)
			case FuncPredicate:
				appendColumn(p.Column)

			default:
				panic(fmt.Sprintf("logs.predicateColumns: unsupported predicate type %T", p))
			}

			return true
		})
	}

	return columns
}
