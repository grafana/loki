package dataset

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// compiledPredicate is a [Predicate] with its column references resolved to
// indexes into a [Row]'s Values.
type compiledPredicate interface {
	// eval reports whether row passes the predicate.
	eval(row Row) bool
}

// compilePredicate resolves the column of every leaf in p to its index in
// lookup and returns an equivalent compiledPredicate. It returns an error if
// a leaf references a column absent from lookup; callers are expected to
// validate predicates before compiling, so a missing column indicates a caller bug.
func compilePredicate(p Predicate, lookup map[Column]int) (compiledPredicate, error) {
	switch p := p.(type) {
	case nil:
		return compiledConst(true), nil

	case AndPredicate:
		left, err := compilePredicate(p.Left, lookup)
		if err != nil {
			return nil, err
		}
		right, err := compilePredicate(p.Right, lookup)
		if err != nil {
			return nil, err
		}
		return compiledAnd{left: left, right: right}, nil

	case OrPredicate:
		left, err := compilePredicate(p.Left, lookup)
		if err != nil {
			return nil, err
		}
		right, err := compilePredicate(p.Right, lookup)
		if err != nil {
			return nil, err
		}
		return compiledOr{left: left, right: right}, nil

	case NotPredicate:
		inner, err := compilePredicate(p.Inner, lookup)
		if err != nil {
			return nil, err
		}
		return compiledNot{inner: inner}, nil

	case TruePredicate:
		return compiledConst(true), nil

	case FalsePredicate:
		return compiledConst(false), nil

	case EqualPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return compiledEqual{columnIndex: idx, value: p.Value}, nil

	case InPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return compiledIn{
			columnIndex: idx,
			physical:    p.Column.ColumnDesc().Type.Physical,
			values:      p.Values,
		}, nil

	case GreaterThanPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return compiledGreaterThan{columnIndex: idx, value: p.Value}, nil

	case LessThanPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return compiledLessThan{columnIndex: idx, value: p.Value}, nil

	case FuncPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return compiledFunc{columnIndex: idx, column: p.Column, keep: p.Keep}, nil

	default:
		panic(fmt.Sprintf("dataset.compilePredicate: unsupported predicate type %T", p))
	}
}

func lookupColumnIndex(lookup map[Column]int, c Column) (int, error) {
	idx, ok := lookup[c]
	if !ok {
		return 0, fmt.Errorf("predicate column %v not found in RowReader columns", c)
	}
	return idx, nil
}

type compiledAnd struct {
	left, right compiledPredicate
}

func (p compiledAnd) eval(row Row) bool {
	return p.left.eval(row) && p.right.eval(row)
}

type compiledOr struct {
	left, right compiledPredicate
}

func (p compiledOr) eval(row Row) bool {
	return p.left.eval(row) || p.right.eval(row)
}

type compiledNot struct {
	inner compiledPredicate
}

func (p compiledNot) eval(row Row) bool {
	return !p.inner.eval(row)
}

// compiledConst is a predicate with a fixed result. It represents
// TruePredicate, FalsePredicate, and a nil predicate.
type compiledConst bool

func (p compiledConst) eval(Row) bool {
	return bool(p)
}

type compiledEqual struct {
	columnIndex int
	value       Value
}

func (p compiledEqual) eval(row Row) bool {
	return CompareValues(&row.Values[p.columnIndex], &p.value) == 0
}

type compiledIn struct {
	columnIndex int
	physical    datasetmd.PhysicalType
	values      ValueSet
}

func (p compiledIn) eval(row Row) bool {
	value := row.Values[p.columnIndex]
	if value.IsNil() || value.Type() != p.physical {
		return false
	}
	return p.values.Contains(value)
}

type compiledGreaterThan struct {
	columnIndex int
	value       Value
}

func (p compiledGreaterThan) eval(row Row) bool {
	return CompareValues(&row.Values[p.columnIndex], &p.value) > 0
}

type compiledLessThan struct {
	columnIndex int
	value       Value
}

func (p compiledLessThan) eval(row Row) bool {
	return CompareValues(&row.Values[p.columnIndex], &p.value) < 0
}

type compiledFunc struct {
	columnIndex int
	column      Column
	keep        func(column Column, value Value) bool
}

func (p compiledFunc) eval(row Row) bool {
	return p.keep(p.column, row.Values[p.columnIndex])
}
