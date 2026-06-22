package postings

import "github.com/apache/arrow-go/v18/arrow/scalar"

// Predicate is an expression used to filter rows in a [Reader]. Every [Column]
// referenced by a predicate must belong to the same [Section] as the projected
// columns.
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

	// A GreaterThanPredicate asserts a row is included only if Column's value is
	// greater than Value.
	GreaterThanPredicate struct {
		Column *Column       // Column to check.
		Value  scalar.Scalar // Value to compare against.
	}

	// A LessThanPredicate asserts a row is included only if Column's value is less
	// than Value.
	LessThanPredicate struct {
		Column *Column       // Column to check.
		Value  scalar.Scalar // Value to compare against.
	}

	// BloomMatchPredicate asserts a row is included only if the bloom filter in
	// Column's binary value tests positive for Value.
	BloomMatchPredicate struct {
		Column *Column // Column holding the bloom filter.
		Value  []byte  // Value to test for membership.
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
func (BloomMatchPredicate) isPredicate()  {}

// walkPredicate traverses a predicate in depth-first order: it starts by
// calling fn(p). If fn(p) returns true, walkPredicate is invoked recursively
// with fn for each of the non-nil children of p, followed by a call of fn(nil).
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
	case BloomMatchPredicate: // No children.
	default:
		panic("postings.walkPredicate: unsupported predicate type")
	}

	fn(nil)
}

// predicateColumns returns the de-duplicated set of columns referenced by the
// given predicates.
func predicateColumns(predicates []Predicate) []*Column {
	exists := make(map[*Column]struct{})
	columns := make([]*Column, 0, len(predicates))

	appendColumn := func(c *Column) {
		if c == nil {
			return
		}
		if _, ok := exists[c]; ok {
			return
		}
		columns = append(columns, c)
		exists[c] = struct{}{}
	}

	for _, p := range predicates {
		walkPredicate(p, func(p Predicate) bool {
			switch p := p.(type) {
			case EqualPredicate:
				appendColumn(p.Column)
			case InPredicate:
				appendColumn(p.Column)
			case GreaterThanPredicate:
				appendColumn(p.Column)
			case LessThanPredicate:
				appendColumn(p.Column)
			case BloomMatchPredicate:
				appendColumn(p.Column)
			}
			return true
		})
	}

	return columns
}
