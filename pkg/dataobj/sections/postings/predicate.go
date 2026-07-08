package postings

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/arrowconv"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// Predicate is an expression used to filter rows in a [Reader].
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

	// RegexMatchPredicate asserts a row is included only if Column's string value
	// matches Matcher.
	RegexMatchPredicate struct {
		Column  *Column                  // Column to check.
		Matcher *labels.FastRegexMatcher // Compiled regex to match against.
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
func (RegexMatchPredicate) isPredicate()  {}

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
	case RegexMatchPredicate: // No children.
	default:
		panic("postings.walkPredicate: unsupported predicate type")
	}

	fn(nil)
}

// predicateColumn returns the column referenced by a leaf predicate. ok is true
// for column-bearing leaf predicates (even when the column is nil) and false for
// composite or constant predicates that carry no column.
func predicateColumn(p Predicate) (col *Column, ok bool) {
	switch p := p.(type) {
	case EqualPredicate:
		return p.Column, true
	case InPredicate:
		return p.Column, true
	case GreaterThanPredicate:
		return p.Column, true
	case LessThanPredicate:
		return p.Column, true
	case BloomMatchPredicate:
		return p.Column, true
	case RegexMatchPredicate:
		return p.Column, true
	}
	return nil, false
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
			col, _ := predicateColumn(p)
			appendColumn(col)
			return true
		})
	}

	return columns
}

// mapPredicates converts postings predicates into dataset predicates, resolving
// each referenced [Column] to its [dataset.Column] via columnLookup.
func mapPredicates(ps []Predicate, columnLookup map[*Column]dataset.Column) ([]dataset.Predicate, error) {
	predicates := make([]dataset.Predicate, 0, len(ps))
	for _, p := range ps {
		mapped, err := mapPredicate(p, columnLookup)
		if err != nil {
			return nil, err
		}
		predicates = append(predicates, mapped)
	}
	return predicates, nil
}

func mapPredicate(p Predicate, columnLookup map[*Column]dataset.Column) (dataset.Predicate, error) {
	switch p := p.(type) {
	case AndPredicate:
		left, err := mapPredicate(p.Left, columnLookup)
		if err != nil {
			return nil, err
		}
		right, err := mapPredicate(p.Right, columnLookup)
		if err != nil {
			return nil, err
		}
		return dataset.AndPredicate{Left: left, Right: right}, nil
	case OrPredicate:
		left, err := mapPredicate(p.Left, columnLookup)
		if err != nil {
			return nil, err
		}
		right, err := mapPredicate(p.Right, columnLookup)
		if err != nil {
			return nil, err
		}
		return dataset.OrPredicate{Left: left, Right: right}, nil
	case NotPredicate:
		inner, err := mapPredicate(p.Inner, columnLookup)
		if err != nil {
			return nil, err
		}
		return dataset.NotPredicate{Inner: inner}, nil
	case TruePredicate:
		return dataset.TruePredicate{}, nil
	case FalsePredicate:
		return dataset.FalsePredicate{}, nil
	case EqualPredicate:
		value, err := convertScalar(p.Value)
		if err != nil {
			return nil, err
		}
		return dataset.EqualPredicate{Column: lookupColumn(p.Column, columnLookup), Value: value}, nil
	case InPredicate:
		col := lookupColumn(p.Column, columnLookup)
		vals := make([]dataset.Value, len(p.Values))
		for i := range p.Values {
			value, err := convertScalar(p.Values[i])
			if err != nil {
				return nil, err
			}
			vals[i] = value
		}

		var valueSet dataset.ValueSet
		switch physical := col.ColumnDesc().Type.Physical; physical {
		case datasetmd.PHYSICAL_TYPE_INT64:
			valueSet = dataset.NewInt64ValueSet(vals)
		case datasetmd.PHYSICAL_TYPE_UINT64:
			valueSet = dataset.NewUint64ValueSet(vals)
		case datasetmd.PHYSICAL_TYPE_BINARY:
			valueSet = dataset.NewBinaryValueSet(vals)
		default:
			return nil, fmt.Errorf("InPredicate not supported for physical type %s", physical)
		}
		return dataset.InPredicate{Column: col, Values: valueSet}, nil
	case GreaterThanPredicate:
		value, err := convertScalar(p.Value)
		if err != nil {
			return nil, err
		}
		return dataset.GreaterThanPredicate{Column: lookupColumn(p.Column, columnLookup), Value: value}, nil
	case LessThanPredicate:
		value, err := convertScalar(p.Value)
		if err != nil {
			return nil, err
		}
		return dataset.LessThanPredicate{Column: lookupColumn(p.Column, columnLookup), Value: value}, nil
	case BloomMatchPredicate:
		return dataset.BloomMatchPredicate{Column: lookupColumn(p.Column, columnLookup), Value: p.Value}, nil
	case RegexMatchPredicate:
		return dataset.RegexMatchPredicate{Column: lookupColumn(p.Column, columnLookup), Matcher: p.Matcher}, nil
	default:
		panic(fmt.Sprintf("unsupported predicate type %T", p))
	}
}

func lookupColumn(col *Column, columnLookup map[*Column]dataset.Column) dataset.Column {
	dc, ok := columnLookup[col]
	if !ok {
		panic(fmt.Sprintf("column %p not found in column lookup", col))
	}
	return dc
}

func convertScalar(s scalar.Scalar) (dataset.Value, error) {
	toType, ok := arrowconv.DatasetType(s.DataType())
	if !ok {
		return dataset.Value{}, fmt.Errorf("unsupported dataset type %s", s.DataType())
	}
	return arrowconv.FromScalar(s, toType), nil
}
