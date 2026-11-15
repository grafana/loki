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
// a leaf references a column absent from lookup.
func compilePredicate(p Predicate, lookup map[Column]int) (compiledPredicate, error) {
	switch p := p.(type) {
	case nil:
		return compiledConstPredicate(true), nil

	case AndPredicate:
		left, err := compilePredicate(p.Left, lookup)
		if err != nil {
			return nil, err
		}
		right, err := compilePredicate(p.Right, lookup)
		if err != nil {
			return nil, err
		}
		return compiledAndPredicate{left: left, right: right}, nil

	case OrPredicate:
		left, err := compilePredicate(p.Left, lookup)
		if err != nil {
			return nil, err
		}
		right, err := compilePredicate(p.Right, lookup)
		if err != nil {
			return nil, err
		}
		return compiledOrPredicate{left: left, right: right}, nil

	case NotPredicate:
		inner, err := compilePredicate(p.Inner, lookup)
		if err != nil {
			return nil, err
		}
		return compiledNotPredicate{inner: inner}, nil

	case TruePredicate:
		return compiledConstPredicate(true), nil

	case FalsePredicate:
		return compiledConstPredicate(false), nil

	case EqualPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return compiledEqualPredicate{columnIndex: idx, value: p.Value}, nil

	case InPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return compiledInPredicate{
			columnIndex: idx,
			physical:    p.Column.ColumnDesc().Type.Physical,
			values:      p.Values,
		}, nil

	case GreaterThanPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return compiledGreaterThanPredicate{columnIndex: idx, value: p.Value}, nil

	case LessThanPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return compiledLessThanPredicate{columnIndex: idx, value: p.Value}, nil

	case FuncPredicate:
		idx, err := lookupColumnIndex(lookup, p.Column)
		if err != nil {
			return nil, err
		}
		return compiledFuncPredicate{columnIndex: idx, column: p.Column, keep: p.Keep}, nil

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

type compiledAndPredicate struct {
	left, right compiledPredicate
}

func (p compiledAndPredicate) eval(row Row) bool {
	return p.left.eval(row) && p.right.eval(row)
}

type compiledOrPredicate struct {
	left, right compiledPredicate
}

func (p compiledOrPredicate) eval(row Row) bool {
	return p.left.eval(row) || p.right.eval(row)
}

type compiledNotPredicate struct {
	inner compiledPredicate
}

func (p compiledNotPredicate) eval(row Row) bool {
	return !p.inner.eval(row)
}

// compiledConstPredicate is a predicate with a fixed result. It represents
// TruePredicate, FalsePredicate, and a nil predicate.
type compiledConstPredicate bool

func (p compiledConstPredicate) eval(Row) bool {
	return bool(p)
}

type compiledEqualPredicate struct {
	columnIndex int
	value       Value
}

func (p compiledEqualPredicate) eval(row Row) bool {
	return CompareValues(&row.Values[p.columnIndex], &p.value) == 0
}

type compiledInPredicate struct {
	columnIndex int
	physical    datasetmd.PhysicalType
	values      ValueSet
}

func (p compiledInPredicate) eval(row Row) bool {
	value := row.Values[p.columnIndex]
	if value.IsNil() || value.Type() != p.physical {
		return false
	}
	return p.values.Contains(value)
}

type compiledGreaterThanPredicate struct {
	columnIndex int
	value       Value
}

func (p compiledGreaterThanPredicate) eval(row Row) bool {
	return CompareValues(&row.Values[p.columnIndex], &p.value) > 0
}

type compiledLessThanPredicate struct {
	columnIndex int
	value       Value
}

func (p compiledLessThanPredicate) eval(row Row) bool {
	return CompareValues(&row.Values[p.columnIndex], &p.value) < 0
}

type compiledFuncPredicate struct {
	columnIndex int
	column      Column
	keep        func(column Column, value Value) bool
}

func (p compiledFuncPredicate) eval(row Row) bool {
	return p.keep(p.column, row.Values[p.columnIndex])
}
